package tool

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestPatchRecoveryIncludesDistantCandidateContextWithoutApplying(t *testing.T) {
	root := t.TempDir()
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("line-%d", i+1)
	}
	lines[19], lines[49] = "old", "old"
	original := strings.Join(lines, "\n") + "\n"
	path := filepath.Join(root, "notes.txt")
	writePatchTestFile(t, path, original)
	runner := NewBuiltinRunner()
	defer runner.Close()
	result := patchTestCall(runner, "feedback", root, FilePatch, map[string]any{
		"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{{"start_line": 1, "old_lines": []string{"old"}, "new_lines": []string{"new"}}}}},
	})
	recovery := decodePatchRecovery(t, result).Recovery
	if !slices.Equal(patchCandidateStarts(recovery.Candidates), []int{20, 50}) {
		t.Fatalf("lost exact candidates: %s", result.Content)
	}
	for _, candidate := range recovery.Candidates {
		if len(candidate.Context) != 5 || candidate.Context[2].Line != candidate.StartLine || candidate.Context[2].Text != "old" {
			t.Fatalf("candidate lacks its own surrounding context: %+v", candidate)
		}
		for _, line := range candidate.Context {
			if line.Text != lines[line.Line-1] || line.Truncated {
				t.Fatalf("candidate context is not fresh source: %+v", line)
			}
		}
	}
	if readPatchTestFile(t, path) != original {
		t.Fatal("diagnostic candidates applied the patch")
	}
}

func TestPatchCandidateContextKeepsUTF8TruncationMarkers(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("界", 1000)
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "head\n"+long+"\nold\n"+long+"\ntail\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	result := patchTestCall(runner, "feedback", root, FilePatch, map[string]any{
		"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{{"start_line": 1, "old_lines": []string{"old"}, "new_lines": []string{"new"}}}}},
	})
	recovery := decodePatchRecovery(t, result).Recovery
	context := recovery.Candidates[0].Context
	if !context[1].Truncated || !context[3].Truncated || !strings.HasSuffix(context[1].Text, "...(truncated)") || len(context[1].Text) > patchRecoveryLineBytes+len("...(truncated)") {
		t.Fatalf("unmarked candidate truncation: %+v", context)
	}
}

func TestPatchSuccessRangesUsePostPatchCoordinates(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "notes.txt"), "head\nsame\ntail\nsame\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	result := patchTestCall(runner, "feedback", root, FilePatch, map[string]any{
		"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{
			{"start_line": 4, "old_lines": []string{"same"}, "new_lines": []string{"changed"}},
			{"start_line": 1, "old_lines": []string{}, "new_lines": []string{"new"}},
		}}},
	})
	payload := decodePatchPayload(t, result)
	if !result.Ok || !slices.Equal(payload.Files[0].LineRanges, []patchLineRange{{StartLine: 1, LineCount: 1}, {StartLine: 5, LineCount: 1}}) {
		t.Fatalf("ranges reused pre-patch coordinates: %s", result.Content)
	}
	if readPatchTestFile(t, filepath.Join(root, "notes.txt")) != "new\nhead\nsame\ntail\nchanged\n" {
		t.Fatal("changed original coordinate semantics")
	}
}

func TestPatchDiffRangesCoverInsertionDeletionAndLineEndings(t *testing.T) {
	for _, tc := range []struct {
		name, old, next string
		deleted         bool
		want            []patchLineRange
	}{
		{"insert", "head\ntail\n", "head\nnew\ntail\n", false, []patchLineRange{{2, 1}}},
		{"delete middle", "head\nold\ntail\n", "head\ntail\n", false, []patchLineRange{{2, 0}}},
		{"delete EOF", "head\nold", "head\n", false, []patchLineRange{{2, 0}}},
		{"empty result", "old\n", "", false, []patchLineRange{{1, 0}}},
		{"CRLF", "head\r\nold\r\ntail", "head\r\nnew\r\ntail", false, []patchLineRange{{2, 1}}},
		{"create", "", "one\ntwo", false, []patchLineRange{{1, 2}}},
		{"delete file", "old\n", "", true, []patchLineRange{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, ranges, err := buildUnifiedFileDiff(preparedPatchFile{Path: "notes.txt", OldText: tc.old, NewText: tc.next, Existed: tc.old != "", Delete: tc.deleted})
			if err != nil || !slices.Equal(ranges, tc.want) {
				t.Fatalf("ranges=%+v want=%+v err=%v", ranges, tc.want, err)
			}
		})
	}
}

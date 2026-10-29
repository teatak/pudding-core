package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFilePatchRelocation(t *testing.T) {
	for _, tc := range []struct {
		name, original string
		start          int
		old, next      []string
		relocate       bool
		want, reason   string
		actual         int
	}{
		{name: "stale line requires opt in", original: "head\nold\ntail\n", start: 3, old: []string{"old"}, next: []string{"new"}, reason: "hunk_lines_mismatch"},
		{name: "unique exact block", original: "head\nold\ntail\n", start: 3, old: []string{"old"}, next: []string{"new"}, relocate: true, want: "head\nnew\ntail\n", actual: 2},
		{name: "out of range CRLF and EOF", original: "head\r\nold\r\ntail", start: 20, old: []string{"old", "tail"}, next: []string{"new"}, relocate: true, want: "head\r\nnew", actual: 2},
		{name: "ambiguous block", original: "old\nhead\nold\n", start: 2, old: []string{"old"}, next: []string{"new"}, relocate: true, reason: "hunk_lines_mismatch"},
		{name: "no fuzzy whitespace", original: "head\n old\n", start: 1, old: []string{"old"}, next: []string{"new"}, relocate: true, reason: "hunk_lines_mismatch"},
		{name: "empty insertion cannot relocate", original: "head\n", start: 20, old: []string{}, next: []string{"new"}, relocate: true, reason: "hunk_line_out_of_range"},
		{name: "positioned duplicate still valid", original: "old\nold\n", start: 2, old: []string{"old"}, next: []string{"new"}, relocate: true, want: "old\nnew\n"},
		{name: "delete unique block", original: "head\nold\ntail\n", start: 1, old: []string{"old"}, next: []string{}, relocate: true, want: "head\ntail\n", actual: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "note.txt")
			writePatchTestFile(t, path, tc.original)
			runner := NewBuiltinRunner()
			defer runner.Close()
			result := patchTestCall(runner, "relocation", root, FilePatch, map[string]any{"scope": "project", "files": []map[string]any{{"path": "note.txt", "action": "edit", "hunks": []map[string]any{{"start_line": tc.start, "old_lines": tc.old, "new_lines": tc.next, "relocate": tc.relocate}}}}})
			if tc.reason != "" {
				if result.Ok || decodeToolResult(t, result)["reason"] != tc.reason {
					t.Fatalf("unexpected failure: %s", result.Content)
				}
				if got := readPatchTestFile(t, path); got != tc.original {
					t.Fatalf("failed patch changed file: %q", got)
				}
				return
			}
			if !result.Ok || readPatchTestFile(t, path) != tc.want {
				t.Fatalf("patch failed or wrong content: %s", result.Content)
			}
			moves := decodePatchPayload(t, result).Files[0].Relocations
			var want []patchHunkRelocation
			if tc.actual != 0 {
				want = []patchHunkRelocation{{Hunk: 1, RequestedStartLine: tc.start, StartLine: tc.actual}}
			}
			if !slices.Equal(moves, want) {
				t.Fatalf("relocations=%+v want=%+v", moves, want)
			}
		})
	}
}

func TestFilePatchRelocationAfterEarlierDeletion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "page.html")
	writePatchTestFile(t, path, "style\nbody\n<g>\nold\n</g>\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	patch := func(hunks ...map[string]any) Result {
		return patchTestCall(runner, "relocation", root, FilePatch, map[string]any{"scope": "project", "files": []map[string]any{{"path": "page.html", "action": "edit", "hunks": hunks}}})
	}
	first := patch(map[string]any{"start_line": 1, "old_lines": []string{"style"}, "new_lines": []string{}})
	if !first.Ok {
		t.Fatal(first.Content)
	}
	// Reuse the old position, as in the reported HTML task. A second hunk inserts
	// ahead of the target; both hunks must still resolve against the pre-patch file.
	result := patch(map[string]any{"start_line": 3, "old_lines": []string{"<g>", "old", "</g>"}, "new_lines": []string{"<g>", "new", "</g>"}, "relocate": true}, map[string]any{"start_line": 1, "old_lines": []string{}, "new_lines": []string{"inserted"}})
	if !result.Ok || readPatchTestFile(t, path) != "inserted\nbody\n<g>\nnew\n</g>\n" {
		t.Fatalf("stale coordinates not recovered: %s", result.Content)
	}
	payload := decodePatchPayload(t, result)
	if !slices.Equal(payload.Files[0].Relocations, []patchHunkRelocation{{Hunk: 1, RequestedStartLine: 3, StartLine: 2}}) {
		t.Fatalf("wrong pre-patch relocation: %+v", payload)
	}
	if !slices.Equal(payload.Files[0].LineRanges, []patchLineRange{{StartLine: 1, LineCount: 1}, {StartLine: 4, LineCount: 1}}) {
		t.Fatalf("wrong post-patch ranges: %+v", payload)
	}
}

func TestFilePatchRelocationOverlapRejectsWholeBatch(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	other := filepath.Join(root, "other.txt")
	writePatchTestFile(t, path, "head\nold\ntail\n")
	writePatchTestFile(t, other, "untouched\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	result := patchTestCall(runner, "relocation", root, FilePatch, map[string]any{"scope": "project", "files": []map[string]any{
		{"path": "other.txt", "action": "replace", "content": "changed\n"},
		{"path": "note.txt", "action": "edit", "hunks": []map[string]any{
			{"start_line": 1, "old_lines": []string{"old", "tail"}, "new_lines": []string{"new"}, "relocate": true},
			{"start_line": 3, "old_lines": []string{"tail"}, "new_lines": []string{"end"}},
		}},
	}})
	if result.Ok || decodeToolResult(t, result)["reason"] != "hunk_overlap" {
		t.Fatalf("overlap accepted: %s", result.Content)
	}
	if readPatchTestFile(t, path) != "head\nold\ntail\n" || readPatchTestFile(t, other) != "untouched\n" {
		t.Fatal("failed batch wrote files")
	}
}

func TestFilePatchRelocationKeepsApprovalSnapshot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "note.txt")
	writePatchTestFile(t, path, "head\nold\n")
	runner := NewBuiltinRunner()
	defer runner.Close()
	args := json.RawMessage(`{"scope":"project","files":[{"path":"note.txt","action":"edit","hunks":[{"start_line":1,"old_lines":["old"],"new_lines":["new"],"relocate":true}]}]}`)
	call := Call{SessionID: "relocation", TurnID: "turn", CallID: "call", Name: FilePatch, Args: args, ProjectDirs: []string{root}}
	details, err := runner.ApprovalDetails(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	files := details["files"].([]patchFileView)
	if !slices.Equal(files[0].Relocations, []patchHunkRelocation{{Hunk: 1, RequestedStartLine: 1, StartLine: 2}}) || !strings.Contains(details["diff"].(string), "+new") {
		t.Fatalf("review missing resolved change: %+v", details)
	}
	writePatchTestFile(t, path, "inserted\nhead\nold\n")
	result := runner.Call(context.Background(), call)
	if result.Ok || decodeToolResult(t, result)["reason"] != "patch_stale" {
		t.Fatalf("approved relocation was recomputed: %s", result.Content)
	}
	if readPatchTestFile(t, path) != "inserted\nhead\nold\n" {
		t.Fatal("external change overwritten")
	}
}

func TestFilePatchRecoveryReadGuidance(t *testing.T) {
	for _, tc := range []struct {
		name, content, old string
		direct             bool
	}{
		{"unique", "head\nold\ntail\n", "old", true},
		{"ambiguous", "head\nold\nold\n", "old", false},
		{"missing", "head\nother\n", "old", false},
		{"truncated context", "head\nold\n" + strings.Repeat("x", 2000) + "\n", "old", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writePatchTestFile(t, filepath.Join(root, "note.txt"), tc.content)
			runner := NewBuiltinRunner()
			defer runner.Close()
			result := patchTestCall(runner, "recovery", root, FilePatch, map[string]any{"scope": "project", "files": []map[string]any{{"path": "note.txt", "action": "edit", "hunks": []map[string]any{{"start_line": 1, "old_lines": []string{tc.old}, "new_lines": []string{"new"}}}}}})
			payload := decodePatchRecovery(t, result)
			if strings.Contains(payload.Hint, "no extra read is needed") != tc.direct {
				t.Fatalf("wrong read guidance: %s", payload.Hint)
			}
			if !tc.direct && !strings.Contains(payload.Hint, "Read a fresh builtin_file_slice") {
				t.Fatalf("missing read guidance: %s", payload.Hint)
			}
		})
	}
}

func TestPatchRelocationPreviewBounds(t *testing.T) {
	moves := make([]patchHunkRelocation, patchMaxHunksPerFile)
	for i := range moves {
		moves[i] = patchHunkRelocation{Hunk: i + 1, RequestedStartLine: 1000 + i, StartLine: 2000 + i}
	}
	previews := modelPatchFilePreviews([]patchFileView{{Path: "note.txt", Relocations: moves, LineRanges: []patchLineRange{{StartLine: 2000, LineCount: 1}}}})
	if len(previews) != 1 || !previews[0].RelocationsTruncated || len(previews[0].Relocations) == 0 || len(previews[0].Relocations) >= len(moves) {
		t.Fatalf("missing bounded relocations: %+v", previews)
	}
	if !slices.Equal(previews[0].Relocations, moves[:len(previews[0].Relocations)]) || len(previews[0].LineRanges) != 1 {
		t.Fatalf("preview lost coordinates: %+v", previews)
	}
	if len(jsonString(previews)) > modelResultHeadChars {
		t.Fatal("relocation preview exceeded budget")
	}
}

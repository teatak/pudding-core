package tool

import (
	"encoding/json"
	"math/rand"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"
)

type patchRecoveryPayload struct {
	OK       bool
	Reason   string
	Hint     string
	Recovery *struct {
		Path                string
		Hunk                int
		StartLine           int
		FileLineCount       int
		ExactMatchCount     int
		CandidateStartLines []int
		CandidatesTruncated bool
		Mismatch            *struct {
			Line              int
			Expected          string
			Actual            string
			ExpectedTruncated bool
			ActualTruncated   bool
		}
		Context []struct {
			Line      int
			Text      string
			Truncated bool
		}
	}
}

func decodePatchRecovery(t *testing.T, result Result) patchRecoveryPayload {
	t.Helper()
	var payload patchRecoveryPayload
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if result.Ok || payload.OK || payload.Recovery == nil || payload.Hint == "" {
		t.Fatalf("missing hunk recovery details: %s", result.Content)
	}
	if result.SummaryKind != SummaryReturnedFields || result.SummaryCount != 5 {
		t.Fatalf("error result lost summary metadata: %+v", result)
	}
	return payload
}

func TestFilePatchHunkRecoveryRequiresExplicitCorrection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	const original = "head\nsame\ntail\nsame\n"
	writePatchTestFile(t, path, original)
	runner := NewBuiltinRunner()
	hunks := []map[string]any{
		{"start_line": 1, "old_lines": []string{"head"}, "new_lines": []string{"new", "head"}},
		{"start_line": 3, "old_lines": []string{"same"}, "new_lines": []string{"changed"}},
	}
	args := map[string]any{"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": hunks}}}
	failed := patchTestCall(runner, "session_recovery", root, FilePatch, args)
	payload := decodePatchRecovery(t, failed)
	if payload.Recovery.Hunk != 2 || !slices.Equal(payload.Recovery.CandidateStartLines, []int{2, 4}) || !strings.Contains(payload.Hint, "disambiguate") {
		t.Fatalf("second hunk must report both ambiguous targets: %s", failed.Content)
	}
	if got := readPatchTestFile(t, path); got != original {
		t.Fatalf("diagnostics applied an uncorrected patch: %q", got)
	}
	// The caller explicitly chooses the second occurrence in the original file.
	// The first hunk grows the file but must not shift this pre-patch coordinate.
	hunks[1]["start_line"] = 4
	applied := patchTestCall(runner, "session_recovery", root, FilePatch, args)
	if !applied.Ok {
		t.Fatalf("corrected patch failed: %s", applied.Content)
	}
	if got := readPatchTestFile(t, path); got != "new\nhead\nsame\ntail\nchanged\n" {
		t.Fatalf("corrected patch changed the wrong occurrence: %q", got)
	}
}

func TestFilePatchHunkRecoveryBounds(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "notes.txt")
	const original = "head\nsame\nsame\nsame\nsame\nsame\nsame\nsame\nsame\n"
	writePatchTestFile(t, path, original)
	result := patchTestCall(NewBuiltinRunner(), "session_recovery", root, FilePatch, map[string]any{
		"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{
			{"start_line": 1, "old_lines": []string{"same", "same"}, "new_lines": []string{"changed"}},
		}}},
	})
	r := decodePatchRecovery(t, result).Recovery
	if r.ExactMatchCount != 7 || !slices.Equal(r.CandidateStartLines, []int{2, 3, 4, 5, 6}) || !r.CandidatesTruncated || len(r.Context) != 5 {
		t.Fatalf("overlapping exact matches must be counted but bounded: %s", result.Content)
	}
	longLine := strings.Repeat("界", 1000)
	writePatchTestFile(t, path, strings.Repeat(longLine+"\n", 7))
	result = patchTestCall(NewBuiltinRunner(), "session_recovery", root, FilePatch, map[string]any{
		"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{
			{"start_line": 4, "old_lines": []string{longLine + "different"}, "new_lines": []string{"changed"}},
		}}},
	})
	r = decodePatchRecovery(t, result).Recovery
	if r.ExactMatchCount != 0 || r.Mismatch == nil || !r.Mismatch.ExpectedTruncated || !r.Mismatch.ActualTruncated || len(r.Context) != 5 || len(result.Content) > 50<<10 {
		t.Fatalf("unbounded or unmarked diagnostic preview: %s", result.Content)
	}
	previews := []string{r.Mismatch.Expected, r.Mismatch.Actual}
	for _, line := range r.Context {
		if !line.Truncated {
			t.Fatal("truncated context must be marked")
		}
		previews = append(previews, line.Text)
	}
	for _, text := range previews {
		if !utf8.ValidString(text) || strings.ContainsRune(text, utf8.RuneError) || len(text) > 1024+len("...(truncated)") || !strings.HasSuffix(text, "...(truncated)") {
			t.Fatalf("invalid or unbounded UTF-8 preview: %q", text)
		}
	}
}

func TestPatchExactCandidateLines(t *testing.T) {
	// Compare the bounded linear scan with exhaustive matching on small inputs,
	// including overlapping patterns, blank lines, whitespace and Unicode.
	random := rand.New(rand.NewSource(1))
	alphabet := []string{"", "a", " a", "中文"}
	for trial := 0; trial < 500; trial++ {
		lines := make([]patchTextLine, random.Intn(30))
		for i := range lines {
			lines[i].text = alphabet[random.Intn(len(alphabet))]
		}
		old := make([]string, random.Intn(7))
		for i := range old {
			old[i] = alphabet[random.Intn(len(alphabet))]
		}
		var want []int
		if len(old) > 0 {
			for i := 0; i+len(old) <= len(lines); i++ {
				match := true
				for j, text := range old {
					if lines[i+j].text != text {
						match = false
						break
					}
				}
				if match {
					want = append(want, i+1)
				}
			}
		}
		got, count := patchExactCandidateLines(lines, old)
		if count != len(want) || !slices.Equal(got, want[:min(len(want), 5)]) {
			t.Fatalf("trial %d: candidates=%v count=%d want=%v", trial, got, count, want)
		}
	}
	// A large repetitive input must not trigger quadratic repeated-block scans.
	got, count := patchExactCandidateLines(make([]patchTextLine, 100000), make([]string, 50000))
	if count != 50001 || !slices.Equal(got, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("large repetitive scan: candidates=%v count=%d", got, count)
	}
}

func TestFilePatchHunkRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, content, reason string
		start, lineCount      int
		old                   []string
		candidates            []int
		mismatchLine          int
		expected, actual      string
	}{
		{name: "one line offset", content: "header\nold\ntail\n", start: 1, lineCount: 3, old: []string{"old"}, candidates: []int{2}, mismatchLine: 1, expected: "old", actual: "header"},
		{name: "moved block", content: "head\nother\nold\nblock\ntail\n", start: 1, lineCount: 5, old: []string{"old", "block"}, candidates: []int{3}, mismatchLine: 1, expected: "old", actual: "head"},
		{name: "duplicate blocks", content: "head\nold\nblock\nold\nblock\n", start: 1, lineCount: 5, old: []string{"old", "block"}, candidates: []int{2, 4}, mismatchLine: 1, expected: "old", actual: "head"},
		{name: "whitespace is not a match", content: "  old\n", start: 1, lineCount: 1, old: []string{"old"}, mismatchLine: 1, expected: "old", actual: "  old"},
		{name: "deleted source line", content: "head\ntail\n", start: 2, lineCount: 2, old: []string{"deleted"}, mismatchLine: 2, expected: "deleted", actual: "tail"},
		{name: "first differing line", content: "head\nchanged\n", start: 1, lineCount: 2, old: []string{"head", "old"}, mismatchLine: 2, expected: "old", actual: "changed"},
		{name: "crlf and no final newline", content: "head\r\nold\r\nblock", start: 1, lineCount: 3, old: []string{"old", "block"}, candidates: []int{2}, mismatchLine: 1, expected: "old", actual: "head"},
		{name: "past eof", content: "old\n", start: 99, lineCount: 1, old: []string{"old"}, candidates: []int{1}, reason: "hunk_line_out_of_range"},
		{name: "range crosses eof", content: "old\nblock\n", start: 2, lineCount: 2, old: []string{"old", "block"}, candidates: []int{1}, reason: "hunk_line_out_of_range"},
		{name: "empty file", start: 1, old: []string{"old"}, reason: "hunk_line_out_of_range"},
		{name: "insertion past eof has no anchor", content: "old\n", start: 3, lineCount: 1, old: []string{}, reason: "hunk_line_out_of_range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "notes.txt")
			otherPath := filepath.Join(root, "other.txt")
			writePatchTestFile(t, path, tc.content)
			writePatchTestFile(t, otherPath, "unchanged\n")
			result := patchTestCall(NewBuiltinRunner(), "session_recovery", root, FilePatch, map[string]any{
				"scope": "project",
				"files": []map[string]any{
					{"path": "other.txt", "action": "replace", "content": "must not apply\n"},
					{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{
						{"start_line": tc.start, "old_lines": tc.old, "new_lines": []string{"replacement"}},
					}},
				},
			})
			payload := decodePatchRecovery(t, result)
			wantReason := tc.reason
			if wantReason == "" {
				wantReason = "hunk_lines_mismatch"
			}
			r := payload.Recovery
			if payload.Reason != wantReason || r.Path != "notes.txt" || r.Hunk != 1 || r.StartLine != tc.start || r.FileLineCount != tc.lineCount {
				t.Fatalf("incorrect recovery location: %s", result.Content)
			}
			if r.ExactMatchCount != len(tc.candidates) || !slices.Equal(r.CandidateStartLines, tc.candidates) || r.CandidatesTruncated {
				t.Fatalf("incorrect exact candidates: %s", result.Content)
			}
			if tc.mismatchLine == 0 {
				if r.Mismatch != nil {
					t.Fatalf("out-of-range hunk should not invent actual text: %s", result.Content)
				}
			} else if r.Mismatch == nil || r.Mismatch.Line != tc.mismatchLine || r.Mismatch.Expected != tc.expected || r.Mismatch.Actual != tc.actual || r.Mismatch.ExpectedTruncated || r.Mismatch.ActualTruncated {
				t.Fatalf("incorrect first mismatch: %s", result.Content)
			}
			if tc.lineCount > 0 && len(r.Context) == 0 {
				t.Fatal("nonempty file should include numbered context")
			}
			originalLines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(tc.content, "\r\n", "\n"), "\n"), "\n")
			for i, line := range r.Context {
				if line.Line < 1 || line.Line > tc.lineCount || line.Text != originalLines[line.Line-1] || line.Truncated || (i > 0 && line.Line != r.Context[i-1].Line+1) {
					t.Fatalf("incorrect numbered context: %+v", r.Context)
				}
			}
			if got := readPatchTestFile(t, path); got != tc.content {
				t.Fatalf("failed patch relocated or changed source: %q", got)
			}
			if got := readPatchTestFile(t, otherPath); got != "unchanged\n" {
				t.Fatalf("failed batch partially applied: %q", got)
			}
			assertNoPatchTempFiles(t, root)
		})
	}
}

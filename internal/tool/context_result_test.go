package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestModelSearchNumbersCanonicalContextWithoutDuplicatingText(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "context.txt")
	if err := os.WriteFile(path, []byte("\r\n before\r\nneedle\r\n\r\n末尾\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, contextLines := range []int{0, 2} {
		result := NewBuiltinRunner().Call(context.Background(), Call{
			SessionID: "search_projection", Name: FileSearch, ProjectDirs: []string{root},
			Args: resultJSON(map[string]any{"scope": "project", "path": path, "query": "needle", "context_lines": contextLines}),
		})
		if !result.Ok {
			t.Fatalf("search failed: %s", result.Content)
		}
		for _, canRead := range []bool{true, false} {
			got := ModelResultContent(FileSearch, true, result.Content, "t1", "c1", canRead)
			fields := decodeToolResult(t, Result{Content: got})
			match := fields["matches"].([]any)[0].(map[string]any)
			want := "3: needle"
			if contextLines == 2 {
				want = "1: \n2:  before\n3: needle\n4: \n5: 末尾"
			}
			if match["numberedExcerpt"] != want || match["path"] != path || match["line"] != float64(3) || match["truncated"] != false || fields["matchCount"] != float64(1) {
				t.Fatalf("numbered context lost source coordinates: %s", got)
			}
			if _, exists := match["text"]; exists {
				t.Fatalf("numbered context duplicated match text: %s", got)
			}
			if _, exists := match["excerpt"]; exists {
				t.Fatalf("numbered context duplicated excerpt: %s", got)
			}
			canonical := decodeToolResult(t, result)["matches"].([]any)[0].(map[string]any)
			if canonical["text"] != "needle" || canonical["excerpt"] == nil || canonical["numberedExcerpt"] != nil {
				t.Fatal("model projection changed canonical search content")
			}
		}
	}
	longLine := "needle" + strings.Repeat("界", 600)
	if err := os.WriteFile(path, []byte(longLine+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := NewBuiltinRunner().Call(context.Background(), Call{
		SessionID: "search_projection", Name: FileSearch, ProjectDirs: []string{root},
		Args: resultJSON(map[string]any{"scope": "project", "path": path, "query": "needle"}),
	})
	if !result.Ok {
		t.Fatalf("long line search failed: %s", result.Content)
	}
	fields := decodeToolResult(t, Result{Content: ModelResultContent(FileSearch, true, result.Content, "t1", "c1", true)})
	match := fields["matches"].([]any)[0].(map[string]any)
	excerpt, _ := truncateSearchLine(longLine, maxFileSearchExcerptLineChars)
	if match["numberedExcerpt"] != "1: "+excerpt || match["truncated"] != true || match["text"] != nil {
		t.Fatalf("truncated search line lost evidence or marker: %+v", match)
	}
}

func TestModelSearchRequiresValidCoordinatesBeforeNumbering(t *testing.T) {
	for _, source := range []string{
		`{"line":2,"lineStart":0,"lineEnd":2,"text":"hit","excerpt":"before\nhit"}`,
		`{"line":2,"lineStart":1,"lineEnd":3,"text":"hit","excerpt":"before\nhit"}`,
		`{"line":3,"lineStart":1,"lineEnd":2,"text":"hit","excerpt":"before\nhit"}`,
		`{"line":2,"lineStart":1,"lineEnd":2,"text":"hit","excerpt":null}`,
	} {
		original := `{"matches":[` + source + `]}`
		got := ModelResultContent(FileSearch, true, original, "t1", "c1", true)
		match := decodeToolResult(t, Result{Content: got})["matches"].([]any)[0].(map[string]any)
		if match["numberedExcerpt"] != nil || match["text"] != "hit" || !strings.Contains(got, `"excerpt"`) {
			t.Fatalf("invalid coordinates invented or discarded source: %s", got)
		}
	}
}

func TestModelResultFileSliceHasOneExactBody(t *testing.T) {
	for _, numbered := range []string{"2: 中文\n3:  縮进", "3:  縮进\n2: 中文", ""} {
		original := jsonString(map[string]any{"content": "duplicate", "numberedContent": numbered, "start": 2, "end": 3, "truncated": true})
		got := ModelResultContent(FileSlice, true, original, "t1", "c1", true)
		var fields map[string]any
		if err := json.Unmarshal([]byte(got), &fields); err != nil {
			t.Fatal(err)
		}
		if _, exists := fields["content"]; exists || fields["numberedContent"] != numbered || fields["truncated"] != true {
			t.Fatalf("deduplicated slice lost source/metadata: %s", got)
		}
		if got != ModelResultContent(FileSlice, true, original, "t1", "c1", true) {
			t.Fatal("model projection is not deterministic")
		}
	}
	for _, original := range []string{`{"content":"keep","numberedContent":null}`, `{"content":"keep"}`, "not JSON"} {
		if got := ModelResultContent(FileSlice, false, original, "t1", "c1", true); !strings.Contains(got, "keep") && original != "not JSON" {
			t.Fatalf("invalid numbered body removed original: %s", got)
		}
	}
}

func TestLargeModelFileSliceKeepsSourceCoordinates(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat(strings.Repeat("x", 1024)+"\n", 100)), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, order := range []string{"natural", "reverse"} {
		result := NewBuiltinRunner().Call(context.Background(), Call{
			SessionID: "slice_projection", Name: FileSlice, ProjectDirs: []string{root},
			Args: resultJSON(map[string]any{"scope": "project", "path": path, "start": 10, "end": 99, "order": order}),
		})
		if !result.Ok {
			t.Fatalf("large slice failed: %s", result.Content)
		}
		canonical := decodeToolResult(t, result)
		got := ModelResultContent(FileSlice, true, result.Content, "slice-turn", "slice-call", true)
		fields := decodeToolResult(t, Result{Content: got})
		if fields["preview_only"] != true || fields["truncated"] != true {
			t.Fatalf("large producer-truncated slice did not retain both preview markers: %s", got)
		}
		for _, key := range []string{"scope", "path", "root", "relativePath", "start", "end", "lines", "origin", "order"} {
			if fields[key] != canonical[key] {
				t.Fatalf("%s preview lost source coordinate %s: got=%v want=%v", order, key, fields[key], canonical[key])
			}
		}
		ref := fields["result_ref"].(map[string]any)
		if ref["turn_id"] != "slice-turn" || ref["call_id"] != "slice-call" {
			t.Fatalf("slice preview cannot read its canonical source: %+v", ref)
		}
		if fields["content"] != nil || fields["numberedContent"] != nil || utf8.RuneCountInString(fields["head"].(string)) != modelResultHeadChars || utf8.RuneCountInString(fields["tail"].(string)) != modelResultTailChars {
			t.Fatal("slice preview duplicated the source body or lost bounded excerpts")
		}
	}
}

func TestModelSearchPreviewKeepsSavedCountsAndReference(t *testing.T) {
	var matches []map[string]any
	for i := 0; i < 35; i++ {
		matches = append(matches, map[string]any{"path": "demo.go", "line": i + 1, "text": fmt.Sprintf("line %d", i), "excerpt": fmt.Sprintf("line %d", i)})
	}
	original := jsonString(map[string]any{"matches": matches, "matchCount": 35, "resultsCapped": true})
	for _, canRead := range []bool{true, false} {
		got := ModelResultContent(FileSearch, true, original, "t1", "c1", canRead)
		fields := decodeToolResult(t, Result{Content: got})
		items := fields["matches"].([]any)
		wantCount := 35
		if canRead {
			wantCount = modelSearchMaxMatches
			if fields["preview_only"] != true || fields["result_ref"].(map[string]any)["call_id"] != "c1" {
				t.Fatalf("missing readback reference: %s", got)
			}
		}
		if len(items) != wantCount || fields["matchCount"] != float64(35) || fields["resultsCapped"] != true {
			t.Fatalf("search changed original counts or incorrectly limited: %s", got)
		}
		if _, duplicate := items[0].(map[string]any)["excerpt"]; duplicate {
			t.Fatal("duplicate search snippet retained")
		}
	}
	// Different excerpts contain additional evidence; do not remove them.
	source := `{"matches":[{"text":"hit","excerpt":"before\nhit\nafter"}]}`
	if got := ModelResultContent(FileSearch, true, source, "t1", "c1", true); !strings.Contains(got, "before") {
		t.Fatalf("explicit search context lost: %s", got)
	}
}

func TestLargeModelResultIsBoundedAndPreservesFailure(t *testing.T) {
	original := jsonString(map[string]any{
		"ok": false, "exitCode": 2, "reason": "non_zero_exit", "verificationStatus": "failed",
		"stdoutTruncated": true, "stdout": strings.Repeat("开头日志🙂\n", 9000), "stderr": "last-error",
	})
	got := ModelResultContent(CommandRun, false, original, "t1", "c1", true)
	fields := decodeToolResult(t, Result{Content: got})
	if fields["ok"] != false || fields["exitCode"] != float64(2) || fields["verificationStatus"] != "failed" || fields["stdoutTruncated"] != true || fields["preview_only"] != true {
		t.Fatalf("failure/producer truncation metadata lost: %s", got)
	}
	if !utf8.ValidString(got) || utf8.RuneCountInString(fields["head"].(string)) != modelResultHeadChars || utf8.RuneCountInString(fields["tail"].(string)) != modelResultTailChars {
		t.Fatal("invalid preview boundaries")
	}
	if len(got) >= len(original)/2 {
		t.Fatalf("large result did not shrink: %d -> %d", len(original), len(got))
	}
	if ModelResultContent(CommandRun, false, original, "t1", "c1", false) != original || ModelResultContent(CommandRun, false, original, "", "c1", true) != original {
		t.Fatal("shortened result without an available canonical reader")
	}
	if ModelResultContent(HistoryGetMessage, true, original, "t1", "c1", true) != original {
		t.Fatal("explicit history read was recursively shortened")
	}
	if got := ModelResultContent(SkillRead, true, original, "t1", "c1", true); strings.Contains(got, "preview_only") {
		t.Fatal("skill instructions were shortened")
	}
}

func TestLargeModelResultKeepsArgumentAndRecoveryDiagnostics(t *testing.T) {
	diagnostics := map[string]any{
		"ok": false, "reason": "invalid_arguments", "errorKind": "path_required",
		"field": "files[0].path", "expected": "non-empty string", "offset": 18, "receivedBytes": 40000,
		"detail": "patch file path is required", "hint": "Set a project file path and retry.",
		"metric": "destination_file_bytes", "actual": 600000, "limit": 524288, "unit": "bytes",
		"allowedScopes": []string{"project"}, "projectRoots": []string{"/project"},
		"truncated": true, "padding": strings.Repeat("x", 20000),
		"recovery": map[string]any{
			"path": "notes.txt", "hunk": 1, "startLine": 37, "fileLineCount": 81,
			"exactMatchCount": 1, "candidateStartLines": []int{36}, "context": strings.Repeat("y", 20000),
		},
	}
	got := ModelResultContent(FilePatch, false, jsonString(diagnostics), "t1", "c1", true)
	fields := decodeToolResult(t, Result{Content: got})
	for _, key := range []string{"reason", "errorKind", "field", "expected", "offset", "receivedBytes", "detail", "hint", "metric", "actual", "limit", "unit", "allowedScopes", "projectRoots", "truncated"} {
		if string(resultJSON(fields[key])) != string(resultJSON(diagnostics[key])) {
			t.Fatalf("preview discarded %s: %s", key, got)
		}
	}
	recovery := fields["recovery"].(map[string]any)
	if recovery["path"] != "notes.txt" || recovery["exactMatchCount"] != float64(1) || string(resultJSON(recovery["candidateStartLines"])) != "[36]" || recovery["context"] != nil {
		t.Fatalf("preview discarded candidate coordinates or kept oversized context: %s", got)
	}
	if fields["preview_only"] != true || fields["result_ref"].(map[string]any)["call_id"] != "c1" {
		t.Fatalf("preview lost canonical readback: %s", got)
	}
}

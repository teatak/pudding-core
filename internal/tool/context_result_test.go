package tool

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

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

package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileWriteRejectsInvalidArgumentsWithoutChangingFiles(t *testing.T) {
	for _, input := range []string{
		`{"scope":"project","path":"sample.txt"}`,
		`{"scope":"project","path":"sample.txt","content":null}`,
		`{"scope":"project","path":"sample.txt","contents":"replacement"}`,
		`{"scope":"project","path":"sample.txt","content":"replacement","extra":true}`,
		`{"scope":"project","path":"sample.txt","content":12}`,
		`{"scope":"project","path":"sample.txt","content":[]}`,
		`{"scope":"project","path":"sample.txt","content":"replacement"} {}`,
		`{"scope":"project","path":"sample.txt","content":"replacement"} invalid`,
		`{"scope":"project","path":"sample.txt","content":"unfinished`,
		`{"path":"sample.txt","content":"replacement"}`,
		`{"scope":"project","content":"replacement"}`,
		`{"scope":"project","path":" ","content":"replacement"}`,
		`null`,
		``,
	} {
		t.Run(input, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "sample.txt")
			const original = "original text\n"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			for _, args := range []string{input, strings.ReplaceAll(input, "sample.txt", "new/dir/sample.txt")} {
				result := runner.Call(context.Background(), Call{Name: FileWrite, Args: json.RawMessage(args), ProjectDirs: []string{root}})
				if result.Ok || decodeToolResult(t, result)["reason"] != "invalid_arguments" {
					t.Fatalf("invalid input must fail before writing: %+v", result)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != original {
				t.Fatalf("invalid input changed original file: %q, %v", after, err)
			}
			if _, err := os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
				t.Fatalf("invalid input created a parent directory: %v", err)
			}
		})
	}
}

func TestFileWriteAllowsExplicitEmptyContent(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	for _, path := range []string{"existing.txt", "new/empty.txt"} {
		if path == "existing.txt" {
			if err := os.WriteFile(filepath.Join(root, path), []byte("original"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		args, err := json.Marshal(map[string]any{"scope": "project", "path": path, "content": ""})
		if err != nil {
			t.Fatal(err)
		}
		result := runner.Call(context.Background(), Call{Name: FileWrite, Args: args, ProjectDirs: []string{root}})
		if !result.Ok || decodeToolResult(t, result)["bytes"] != float64(0) {
			t.Fatalf("explicit empty content must succeed: %+v", result)
		}
		content, err := os.ReadFile(filepath.Join(root, path))
		if err != nil || len(content) != 0 {
			t.Fatalf("expected empty file: %q, %v", content, err)
		}
	}
}

func TestFileSliceMetadataMatchesReturnedLines(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		lineChars, totalLines int
		options               string
		start, end, count     int
		reverse, truncated    bool
	}{
		{name: "normal", lineChars: 4, totalLines: 100, options: `"start":1,"end":100`, start: 1, end: 100, count: 100},
		{name: "truncated", lineChars: 1024, totalLines: 100, options: `"start":1,"end":100`, start: 1, end: 63, count: 63, truncated: true},
		{name: "reverse", lineChars: 1024, totalLines: 100, options: `"start":1,"end":100,"order":"reverse"`, start: 38, end: 100, count: 63, reverse: true, truncated: true},
		{name: "offset", lineChars: 1024, totalLines: 120, options: `"start":20,"end":100`, start: 20, end: 82, count: 63, truncated: true},
		{name: "tail", lineChars: 1024, totalLines: 120, options: `"origin":"end","skip":5,"lines":100`, start: 16, end: 78, count: 63, truncated: true},
		{name: "reverse tail", lineChars: 1024, totalLines: 120, options: `"origin":"end","skip":5,"lines":100,"order":"reverse"`, start: 53, end: 115, count: 63, reverse: true, truncated: true},
		{name: "line cannot fit", lineChars: maxFileSlicePayload, totalLines: 1, options: `"start":1,"end":1`, truncated: true},
		{name: "empty file", options: `"start":1,"end":100`},
		{name: "past end", lineChars: 4, totalLines: 1, options: `"start":2,"end":100`},
		{name: "blank line", totalLines: 1, options: `"start":1,"end":1`, start: 1, end: 1, count: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			lineText := strings.Repeat("x", tc.lineChars)
			if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte(strings.Repeat(lineText+"\n", tc.totalLines)), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			result := runner.Call(context.Background(), Call{
				Name: FileSlice, Args: json.RawMessage(`{"scope":"project","path":"sample.txt",` + tc.options + `}`), ProjectDirs: []string{root},
			})
			var payload struct {
				Start, End, Lines        int
				Content, NumberedContent string
				Truncated                bool
			}
			if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
				t.Fatal(err)
			}
			if !result.Ok || payload.Start != tc.start || payload.End != tc.end || payload.Lines != tc.count || payload.Truncated != tc.truncated {
				t.Fatalf("range=(%d,%d) lines=%d truncated=%v ok=%v", payload.Start, payload.End, payload.Lines, payload.Truncated, result.Ok)
			}
			var wantText, wantNumbered []string
			for i := 0; i < tc.count; i++ {
				number := tc.start + i
				if tc.reverse {
					number = tc.end - i
				}
				wantText = append(wantText, lineText)
				wantNumbered = append(wantNumbered, fmt.Sprintf("%d: %s", number, lineText))
			}
			if payload.Content != strings.Join(wantText, "\n") || payload.NumberedContent != strings.Join(wantNumbered, "\n") {
				t.Fatal("content and numberedContent do not match the reported source range")
			}
		})
	}
}

func TestFileSliceContinuesAfterTruncatedResultWithoutSkippingLines(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d %s", i, strings.Repeat("x", 1024)))
	}
	if err := os.WriteFile(filepath.Join(root, "sample.txt"), []byte(strings.Join(lines, "\n")), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	var received []string
	for start := 1; start <= len(lines); {
		result := runner.Call(context.Background(), Call{
			Name: FileSlice, Args: json.RawMessage(fmt.Sprintf(`{"scope":"project","path":"sample.txt","start":%d,"end":100}`, start)), ProjectDirs: []string{root},
		})
		var payload struct {
			Start, End, Lines int
			Content           string
		}
		if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
			t.Fatal(err)
		}
		if !result.Ok || payload.Start != start || payload.End < start || payload.Lines == 0 {
			t.Fatalf("invalid continuation: %+v", payload)
		}
		received = append(received, strings.Split(payload.Content, "\n")...)
		start = payload.End + 1
	}
	if strings.Join(received, "\n") != strings.Join(lines, "\n") {
		t.Fatalf("expected all 100 source lines, received %d", len(received))
	}
}

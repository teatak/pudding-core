package tool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

func TestFileWriteSchemaMatchesAcceptedFields(t *testing.T) {
	def := definitionByName(t, BuiltinDefinitions(), FileWrite)
	var schema struct{ Properties map[string]json.RawMessage }
	if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	var fields []string
	for field := range schema.Properties {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	if !reflect.DeepEqual(fields, []string{"content", "expected_hash", "path", "scope"}) {
		t.Fatalf("write schema advertises fields outside the write contract: %v", fields)
	}
}

func TestFileWriteRejectsInvalidArgumentsWithoutChangingFiles(t *testing.T) {
	for _, tc := range []struct{ input, kind, field string }{
		{`{"scope":"project","path":"sample.txt"}`, "missing_field", "content"},
		{`{"scope":"project","path":"sample.txt","content":null}`, "invalid_type", "content"},
		{`{"scope":"project","path":"sample.txt","contents":"replacement"}`, "missing_field", "content"},
		{`{"scope":"project","path":"sample.txt","content":"replacement","extra":true}`, "unknown_field", "extra"},
		{`{"scope":"project","path":"sample.txt","content":"replacement","include_ignored":true}`, "unknown_field", "include_ignored"},
		{`{"scope":"project","path":"sample.txt","content":"replacement","output_mode":"files"}`, "unknown_field", "output_mode"},
		{`{"scope":"project","path":"sample.txt","content":12}`, "invalid_type", "content"},
		{`{"scope":"project","path":"sample.txt","content":[]}`, "invalid_type", "content"},
		{`{"scope":"project","path":"sample.txt","content":"replacement"} {}`, "invalid_json", ""},
		{`{"scope":"project","path":"sample.txt","content":"replacement"} invalid`, "invalid_json", ""},
		{`{"scope":"project","path":"sample.txt","content":"unfinished`, "truncated_json", ""},
		{`{"path":"sample.txt","content":"replacement"}`, "missing_field", "scope"},
		{`{"scope":"project","content":"replacement"}`, "missing_field", "path"},
		{`{"scope":"project","path":" ","content":"replacement"}`, "invalid_value", "path"},
		{`null`, "expected_object", ""},
		{``, "missing_arguments", ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "sample.txt")
			const original = "original text\n"
			if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			for _, args := range []string{tc.input, strings.ReplaceAll(tc.input, "sample.txt", "new/dir/sample.txt")} {
				result := runner.Call(context.Background(), Call{Name: FileWrite, Args: json.RawMessage(args), ProjectDirs: []string{root}})
				payload := decodeToolResult(t, result)
				if result.Ok || payload["reason"] != "invalid_arguments" {
					t.Fatalf("invalid input must fail before writing: %+v", result)
				}
				field, _ := payload["field"].(string)
				if payload["errorKind"] != tc.kind || field != tc.field || payload["receivedBytes"] != float64(len(args)) {
					t.Fatalf("inaccurate argument diagnostic: %+v", payload)
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
		input := map[string]any{"scope": "project", "path": path, "content": ""}
		if path == "existing.txt" {
			input["expected_hash"] = patchContentHash([]byte("original"))
		}
		args, err := json.Marshal(input)
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

func TestFileSliceStopsAtRequestedEndBeforeOversizedLine(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "generated.txt"), []byte("wanted\n"+strings.Repeat("x", 1<<20)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	call := Call{Name: FileSlice, ProjectDirs: []string{root}, Args: json.RawMessage(`{"scope":"project","path":"generated.txt","start":1,"end":1}`)}
	result := runner.Call(context.Background(), call)
	payload := decodeToolResult(t, result)
	if !result.Ok || payload["content"] != "wanted" || payload["start"] != float64(1) || payload["end"] != float64(1) || payload["truncated"] != false {
		t.Fatalf("a line outside the requested range broke the slice: %+v", result)
	}
	call.Args = json.RawMessage(`{"scope":"project","path":"generated.txt","start":1,"end":2}`)
	if result = runner.Call(context.Background(), call); result.Ok {
		t.Fatal("an oversized requested line was silently omitted")
	}
}

func TestFileSliceTailWindowKeepsOrderAndLineNumbers(t *testing.T) {
	for _, tc := range []struct {
		total, lines, skip int
		finalNewline       bool
	}{
		{0, 3, 0, false}, {1, 1, 0, false}, {5, 1, 0, true},
		{5, 3, 2, true}, {5, 3, 6, true}, {14, 4, 2, false},
		{20000, maxFileSliceLines, maxFileSliceSkip, true},
	} {
		t.Run(fmt.Sprintf("total=%d/lines=%d/skip=%d", tc.total, tc.lines, tc.skip), func(t *testing.T) {
			var source []string
			for i := range tc.total {
				text := fmt.Sprintf("line-%d", i+1)
				if i%7 == 3 {
					text = ""
				}
				source = append(source, text)
			}
			data := strings.Join(source, "\n")
			if tc.finalNewline {
				data += "\n"
			}
			path := filepath.Join(t.TempDir(), "log.txt")
			if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := readLineRangeFromEnd(path, tc.lines, tc.skip)
			if err != nil {
				t.Fatal(err)
			}
			end := max(0, tc.total-tc.skip)
			start := max(0, end-tc.lines)
			if len(got) != end-start {
				t.Fatalf("tail length=%d, want %d", len(got), end-start)
			}
			for i, record := range got {
				if record.number != start+i+1 || record.text != source[start+i] {
					t.Fatalf("tail record %d lost source order: %+v", i, record)
				}
			}
		})
	}
}

func TestFileMoveRequiresExplicitOverwrite(t *testing.T) {
	for _, scope := range []string{"project", "skill", "temp"} {
		t.Run(scope, func(t *testing.T) {
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			defer runner.Close()
			root := t.TempDir()
			if scope != "project" {
				var err error
				root, _, err = runner.managedRoot(scope)
				if err != nil {
					t.Fatal(err)
				}
			}
			for name, content := range map[string]string{"source.txt": "source", "target.txt": "target"} {
				writePatchTestFile(t, filepath.Join(root, name), content)
			}
			args := map[string]any{"scope": scope, "from_path": "source.txt", "to_path": "target.txt"}
			call := Call{Name: FileMove, ProjectDirs: []string{root}, Args: resultJSON(args)}
			result := runner.Call(context.Background(), call)
			if result.Ok || decodeToolResult(t, result)["reason"] != "destination_exists" {
				t.Fatalf("conflicting move was not rejected: %+v", result)
			}
			for name, want := range map[string]string{"source.txt": "source", "target.txt": "target"} {
				if data, err := os.ReadFile(filepath.Join(root, name)); err != nil || string(data) != want {
					t.Fatalf("conflict changed %s: %q %v", name, data, err)
				}
			}
			args["overwrite"] = true
			call.Args = resultJSON(args)
			if result = runner.Call(context.Background(), call); !result.Ok {
				t.Fatalf("explicit replacement failed: %+v", result)
			}
			if data, err := os.ReadFile(filepath.Join(root, "target.txt")); err != nil || string(data) != "source" {
				t.Fatalf("wrong replacement: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(root, "source.txt")); !os.IsNotExist(err) {
				t.Fatalf("source survived completed move: %v", err)
			}
			args["from_path"], args["overwrite"] = "target.txt", false
			call.Args = resultJSON(args)
			if result = runner.Call(context.Background(), call); !result.Ok {
				t.Fatalf("moving to the same path should be a no-op: %+v", result)
			}
		})
	}
}

func TestFileMoveDirectoryPreservesConflictsAndCreatesParents(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "source", "nested", "file.txt"), "source")
	if err := os.Mkdir(filepath.Join(root, "target"), 0o700); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	call := Call{Name: FileMove, ProjectDirs: []string{root}, Args: json.RawMessage(`{"scope":"project","from_path":"source","to_path":"target"}`)}
	result := runner.Call(context.Background(), call)
	if result.Ok || decodeToolResult(t, result)["reason"] != "destination_exists" {
		t.Fatalf("existing directory was replaced: %+v", result)
	}
	call.Args = json.RawMessage(`{"scope":"project","from_path":"source","to_path":"new/parents/moved"}`)
	if result = runner.Call(context.Background(), call); !result.Ok {
		t.Fatal(result.Content)
	}
	if data, err := os.ReadFile(filepath.Join(root, "new/parents/moved/nested/file.txt")); err != nil || string(data) != "source" {
		t.Fatalf("directory contents lost: %q %v", data, err)
	}
}

func TestFileMoveConcurrentDestinationCreationDoesNotOverwrite(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner()
	defer runner.Close()
	const count = 8
	for i := range count {
		name := fmt.Sprintf("source-%d", i)
		writePatchTestFile(t, filepath.Join(root, name), name)
	}
	start := make(chan struct{})
	results := make(chan Result, count)
	for i := range count {
		go func() {
			<-start
			args := map[string]any{"scope": "project", "from_path": fmt.Sprintf("source-%d", i), "to_path": "winner"}
			results <- runner.Call(context.Background(), Call{Name: FileMove, ProjectDirs: []string{root}, Args: resultJSON(args)})
		}()
	}
	close(start)
	winners := 0
	for range count {
		result := <-results
		if result.Ok {
			winners++
		} else if decodeToolResult(t, result)["reason"] != "destination_exists" {
			t.Fatalf("unexpected failure: %+v", result)
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent moves replaced one another: %d succeeded", winners)
	}
	data, err := os.ReadFile(filepath.Join(root, "winner"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range count {
		name := fmt.Sprintf("source-%d", i)
		remaining, err := os.ReadFile(filepath.Join(root, name))
		if name == string(data) {
			if !os.IsNotExist(err) {
				t.Fatalf("winning source not moved: %v", err)
			}
		} else if err != nil || string(remaining) != name {
			t.Fatalf("losing source lost: %s %q %v", name, remaining, err)
		}
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

func TestFileWriteVersionConflictAndConcurrentWriters(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	target := filepath.Join(root, "file.txt")
	if err := os.WriteFile(target, []byte("original content"), 0600); err != nil {
		t.Fatal(err)
	}
	read := runner.Call(context.Background(), Call{Name: FileRead, ProjectDirs: []string{root}, Args: json.RawMessage(`{"scope":"project","path":"file.txt","max_chars":1}`)})
	version := decodeToolResult(t, read)["contentHash"]
	if version != patchContentHash([]byte("original content")) {
		t.Fatalf("hash was not for full content: %s", read.Content)
	}
	write := func(session, content string, hash any) Result {
		args := map[string]any{"scope": "project", "path": "file.txt", "content": content}
		if hash != nil {
			args["expected_hash"] = hash
		}
		return runner.Call(context.Background(), Call{SessionID: session, Name: FileWrite, ProjectDirs: []string{root}, Args: resultJSON(args)})
	}
	missing := write("a", "must not write", nil)
	if missing.Ok || decodeToolResult(t, missing)["reason"] != "file_version_required" {
		t.Fatalf("unguarded overwrite: %s", missing.Content)
	}
	var wg sync.WaitGroup
	results := make(chan Result, 2)
	for _, session := range []string{"a", "b"} {
		wg.Add(1)
		go func(session string) { defer wg.Done(); results <- write(session, session, version) }(session)
	}
	wg.Wait()
	close(results)
	success := 0
	var winner string
	for result := range results {
		if result.Ok {
			success++
			winner = decodeToolResult(t, result)["contentHash"].(string)
		} else if decodeToolResult(t, result)["reason"] != "file_conflict" {
			t.Fatalf("unexpected failure: %s", result.Content)
		}
	}
	if success != 1 {
		t.Fatalf("%d concurrent writes succeeded", success)
	}
	current, err := os.ReadFile(target)
	if err != nil || patchContentHash(current) != winner {
		t.Fatalf("winner overwritten: %q %v", current, err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	deleted := write("a", "do not recreate", winner)
	if deleted.Ok || decodeToolResult(t, deleted)["reason"] != "file_conflict" {
		t.Fatalf("recreated removed file: %s", deleted.Content)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("deleted file reappeared: %v", err)
	}
	created := write("a", "new", nil)
	if !created.Ok {
		t.Fatalf("create failed: %s", created.Content)
	}
	overwritten := write("b", "overwrite", nil)
	if overwritten.Ok {
		t.Fatal("create replaced existing file")
	}
}

type fileWriteTestReader func([]byte) (int, error)

func (read fileWriteTestReader) Read(p []byte) (int, error) { return read(p) }

func TestFileWriteStagingFailurePreservesDestination(t *testing.T) {
	for _, existed := range []bool{false, true} {
		t.Run(fmt.Sprint(existed), func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "file.txt")
			expectedHash := ""
			if existed {
				writePatchTestFile(t, target, "original content")
				expectedHash = patchContentHash([]byte("original content"))
			}
			failure := errors.New("interrupted content transfer")
			content := io.MultiReader(strings.NewReader("partial replacement"), fileWriteTestReader(func([]byte) (int, error) {
				return 0, failure
			}))
			if err := writeFileAtomically(target, content, expectedHash, !existed); !errors.Is(err, failure) {
				t.Fatalf("staging failure hidden: %v", err)
			}
			data, err := os.ReadFile(target)
			if existed && (err != nil || string(data) != "original content") {
				t.Fatalf("failed write damaged original: %q, %v", data, err)
			}
			if !existed && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("failed create left partial content: %q, %v", data, err)
			}
			entries, err := os.ReadDir(root)
			want := 0
			if existed {
				want = 1
			}
			if err != nil || len(entries) != want {
				t.Fatalf("staging files leaked: %v, %v", entries, err)
			}
		})
	}
}

func TestFileWriteRechecksVersionAfterStaging(t *testing.T) {
	for _, change := range []string{"edit", "delete", "create"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "file.txt")
			expectedHash := ""
			if change != "create" {
				writePatchTestFile(t, target, "original")
				expectedHash = patchContentHash([]byte("original"))
			}
			content := io.MultiReader(strings.NewReader("replacement"), fileWriteTestReader(func([]byte) (int, error) {
				var err error
				if change == "delete" {
					err = os.Remove(target)
				} else {
					err = os.WriteFile(target, []byte("concurrent edit"), 0o600)
				}
				if err != nil {
					t.Fatal(err)
				}
				return 0, io.EOF
			}))
			if err := writeFileAtomically(target, content, expectedHash, change == "create"); !errors.Is(err, errFileWriteConflict) {
				t.Fatalf("concurrent change was not rejected: %v", err)
			}
			data, err := os.ReadFile(target)
			if change == "delete" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("deleted file recreated: %q, %v", data, err)
				}
			} else if err != nil || string(data) != "concurrent edit" {
				t.Fatalf("concurrent change lost: %q, %v", data, err)
			}
			staged, err := filepath.Glob(filepath.Join(root, ".pudding-write-*"))
			if err != nil || len(staged) != 0 {
				t.Fatalf("conflict left staging files: %v, %v", staged, err)
			}
		})
	}
}

func TestFileWriteAtomicReplacementPreservesModesAndScope(t *testing.T) {
	for _, scope := range []string{"project", "skill", "temp"} {
		t.Run(scope, func(t *testing.T) {
			root := t.TempDir()
			runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
			defer runner.Close()
			call := Call{Name: FileWrite, ProjectDirs: []string{root}}
			path := "demo/script.sh"
			resolved, err := runner.resolveFilePath(call, scope, path, true, false, true)
			if err != nil {
				t.Fatal(err)
			}
			original := "#!/bin/sh\necho original\n"
			writePatchTestFile(t, resolved.target, original)
			if err := os.Chmod(resolved.target, 0o755); err != nil {
				t.Fatal(err)
			}
			content := "#!/bin/sh\necho replacement\n"
			args := map[string]any{"scope": scope, "path": path, "content": content}
			if scope == "project" {
				args["expected_hash"] = patchContentHash([]byte(original))
			}
			call.Args = resultJSON(args)
			result := runner.Call(context.Background(), call)
			if !result.Ok || decodeToolResult(t, result)["contentHash"] != patchContentHash([]byte(content)) {
				t.Fatalf("write failed: %s", result.Content)
			}
			if data, err := os.ReadFile(resolved.target); err != nil || string(data) != content {
				t.Fatalf("wrong replacement: %q, %v", data, err)
			}
			info, err := os.Stat(resolved.target)
			if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o755) {
				t.Fatalf("file permissions lost: %v, %v", info, err)
			}
			entries, err := os.ReadDir(filepath.Dir(resolved.target))
			if err != nil || len(entries) != 1 || entries[0].Name() != "script.sh" {
				t.Fatalf("successful write left staging files: %v, %v", entries, err)
			}
		})
	}
}

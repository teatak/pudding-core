package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestFileDiscoveryPagination(t *testing.T) {
	root := t.TempDir()
	for name, content := range map[string]string{"a/one.txt": "hit\nhit\nhit\n", "a/z.txt": "hit\n", "a-.txt": "hit\n", "b.txt": "hit\n", "ignored.txt": "hit\n", ".gitignore": "ignored.txt\n"} {
		writePatchTestFile(t, filepath.Join(root, name), content)
	}
	for _, mode := range []string{"content", "files"} {
		options := TextFileSearchOptions{Query: "hit", OutputMode: mode, MaxResults: 2, CaseSensitive: true}
		var got []string
		for n := 0; ; n++ {
			if n > 10 {
				t.Fatal("cursor did not advance")
			}
			result, err := SearchTextFiles(context.Background(), root, options)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range result.Matches {
				rel, _ := filepath.Rel(root, m.Path)
				got = append(got, fmt.Sprintf("%s:%d", filepath.ToSlash(rel), m.Line))
			}
			if result.NextCursor == "" {
				if !result.Complete {
					t.Fatal("final page incomplete")
				}
				break
			}
			if result.Complete || !result.ResultsCapped {
				t.Fatal("paged result claims completion")
			}
			options.Cursor = result.NextCursor
			wrong := options
			wrong.Query = "other"
			if _, err := SearchTextFiles(context.Background(), root, wrong); err == nil {
				t.Fatal("cursor reused with a different query")
			}
		}
		want := []string{"a/one.txt:1", "a/one.txt:2", "a/one.txt:3", "a/z.txt:1", "a-.txt:1", "b.txt:1"}
		if mode == "files" {
			want = []string{"a/one.txt:0", "a/z.txt:0", "a-.txt:0", "b.txt:0"}
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s pages: %v", mode, got)
		}
	}
	options := TextFileSearchOptions{Query: "hit", MaxResults: 1}
	for line := 1; line <= 3; line++ {
		page, err := SearchTextFiles(context.Background(), filepath.Join(root, "a/one.txt"), options)
		if err != nil || len(page.Matches) != 1 || page.Matches[0].Line != line {
			t.Fatalf("direct page %d: %+v %v", line, page, err)
		}
		options.Cursor = page.NextCursor
		if (line == 3) != (options.Cursor == "") {
			t.Fatalf("wrong EOF: %+v", page)
		}
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	var paths []string
	cursor := ""
	for n := 0; ; n++ {
		if n > 10 {
			t.Fatal("list cursor did not advance")
		}
		result := runner.Call(context.Background(), Call{Name: FileList, ProjectDirs: []string{root}, Args: resultJSON(map[string]any{"scope": "project", "path": ".", "recursive": true, "include_globs": []string{"**/*.txt"}, "max_entries": 1, "cursor": cursor})})
		if !result.Ok {
			t.Fatal(result.Content)
		}
		var page struct {
			Entries    []struct{ Path string }
			NextCursor string
			Complete   bool
		}
		if err := json.Unmarshal([]byte(result.Content), &page); err != nil {
			t.Fatal(err)
		}
		for _, e := range page.Entries {
			paths = append(paths, filepath.Base(e.Path))
		}
		cursor = page.NextCursor
		if cursor == "" {
			if !page.Complete {
				t.Fatal(result.Content)
			}
			break
		}
	}
	if !reflect.DeepEqual(paths, []string{"one.txt", "z.txt", "a-.txt", "b.txt"}) {
		t.Fatalf("list pages: %v", paths)
	}
}

func TestFileSearchCursorAdvancesAcrossTraversalLimit(t *testing.T) {
	root := t.TempDir()
	// This lower-level traversal receives canonical roots from SearchTextFiles.
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a/a.txt", "a/b.txt", "b.txt", "c.txt"} {
		writePatchTestFile(t, filepath.Join(root, name), "nothing\n")
	}
	matcher, _ := newFileSearchMatcher("literal", "hit", true)
	cursor := ""
	for n := 0; ; n++ {
		if n > 5 {
			t.Fatal("empty pages loop")
		}
		page, err := newFilePage(root, cursor, "test")
		if err != nil {
			t.Fatal(err)
		}
		page.limit = 1
		_, _, capped, err := searchTextFiles(context.Background(), root, fileSearchOptions{baseRoot: root, matcher: matcher, skipped: map[string]int{}, page: page}, 10)
		if err != nil {
			t.Fatal(err)
		}
		if !capped {
			if n == 0 {
				t.Fatal("test did not exercise cap")
			}
			break
		}
		next := page.next()
		if next == "" || next == cursor {
			t.Fatal("cursor failed to advance")
		}
		cursor = next
	}
}

func TestFileDiscoverySchemasExposePagingAndSearchModes(t *testing.T) {
	for _, def := range BuiltinDefinitions() {
		if def.Name != FileList && def.Name != FileSearch {
			continue
		}
		var schema struct{ Properties map[string]json.RawMessage }
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		keys := []string{"cursor", "include_ignored"}
		if def.Name == FileSearch {
			keys = append(keys, "output_mode")
		}
		for _, key := range keys {
			if len(schema.Properties[key]) == 0 {
				t.Fatalf("%s missing %s", def.Name, key)
			}
		}
	}
}

func TestFileDiscoverySharesIgnoreRulesAndSupportsExplicitOverride(t *testing.T) {
	root := t.TempDir()
	for path, content := range map[string]string{
		".gitignore":     "*.gen.go\n!keep.gen.go\nignored/\n",
		"src/.gitignore": "local.go\n",
		"src/main.go":    "needle\nneedle\n", "src/local.go": "needle\n",
		"src/nested/other.go": "needle\n", "src/generated.gen.go": "needle\n",
		"keep.gen.go": "needle\n", "sibling/local.go": "needle\n",
		"ignored/hidden.go": "needle\n", "dist/built.go": "needle\n",
		".git/internal.go": "needle\n",
	} {
		writePatchTestFile(t, filepath.Join(root, path), content)
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	for _, includeIgnored := range []bool{false, true} {
		want := []string{"keep.gen.go", "sibling/local.go", "src/main.go", "src/nested/other.go"}
		if includeIgnored {
			want = append(want, "dist/built.go", "ignored/hidden.go", "src/generated.gen.go", "src/local.go")
		}
		sort.Strings(want)
		for _, name := range []string{FileList, FileSearch} {
			args := map[string]any{"scope": "project", "path": ".", "include_globs": []string{"**/*.go"}, "include_ignored": includeIgnored}
			if name == FileList {
				args["recursive"] = true
			} else {
				args["query"], args["output_mode"] = "needle", "files"
			}
			result := runner.Call(context.Background(), Call{Name: name, ProjectDirs: []string{root}, Args: resultJSON(args)})
			payload := decodeToolResult(t, result)
			if !result.Ok || payload["complete"] != true {
				t.Fatalf("%s: %s", name, result.Content)
			}
			paths := make([]string, 0)
			if name == FileSearch {
				for _, path := range payload["files"].([]any) {
					paths = append(paths, path.(string))
				}
				if payload["matches"] != nil || payload["matchCount"] != nil || payload["fileCount"] != float64(len(want)) {
					t.Fatalf("files mode returned snippets or line counts: %s", result.Content)
				}
				if model := ModelResultContent(name, true, result.Content, "turn", "call", true); strings.Contains(model, "numberedExcerpt") || !strings.Contains(model, "src/main.go") {
					t.Fatalf("model lost path result: %s", model)
				}
			} else {
				for _, entry := range payload["entries"].([]any) {
					paths = append(paths, entry.(map[string]any)["path"].(string))
				}
			}
			canonicalRoot, err := filepath.EvalSymlinks(payload["root"].(string))
			if err != nil {
				t.Fatal(err)
			}
			for i, path := range paths {
				paths[i], _ = filepath.Rel(canonicalRoot, path)
				paths[i] = filepath.ToSlash(paths[i])
			}
			sort.Strings(paths)
			if !reflect.DeepEqual(paths, want) {
				t.Fatalf("%s include=%t: %v, want %v", name, includeIgnored, paths, want)
			}
		}
	}
	// A focused subtree still inherits authorized-root rules; explicitly named
	// files remain readable/searchable even when discovery ignores them.
	subtree, err := SearchTextFiles(context.Background(), filepath.Join(root, "src"), TextFileSearchOptions{BaseRoot: root, Query: "needle", OutputMode: "files"})
	if err != nil || len(subtree.Matches) != 2 {
		t.Fatalf("subtree rules: %+v %v", subtree, err)
	}
	for _, match := range subtree.Matches {
		rel, err := filepath.Rel(root, match.Path)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			t.Fatalf("search changed caller path representation: %s", match.Path)
		}
	}
	direct, err := SearchTextFiles(context.Background(), filepath.Join(root, "src/local.go"), TextFileSearchOptions{BaseRoot: root, Query: "needle"})
	if err != nil || len(direct.Matches) != 1 {
		t.Fatalf("explicit file was hidden: %+v %v", direct, err)
	}
}

func TestFileDiscoveryBoundariesAndCaps(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "a.bin"), "\x00binary")
	writePatchTestFile(t, filepath.Join(root, "nested/b.txt"), "needle\n")
	writePatchTestFile(t, filepath.Join(outside, "secret.txt"), "needle\n")
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if _, err := SearchTextFiles(context.Background(), filepath.Join(root, "escape"), TextFileSearchOptions{Query: "needle"}); err == nil {
		t.Fatal("normalization followed a direct symlink target")
	}
	runner := NewBuiltinRunner()
	defer runner.Close()
	call := Call{Name: FileList, ProjectDirs: []string{root}, Args: resultJSON(map[string]any{"scope": "project", "path": ".", "recursive": true})}
	result := runner.Call(context.Background(), call)
	payload := decodeToolResult(t, result)
	if !result.Ok || payload["returnedCount"] != float64(2) || payload["complete"] != false || strings.Contains(result.Content, "secret.txt") {
		t.Fatalf("recursive path discovery: %s", result.Content)
	}
	call.Args = resultJSON(map[string]any{"scope": "project", "path": ".", "recursive": true, "max_entries": 1})
	result = runner.Call(context.Background(), call)
	if payload = decodeToolResult(t, result); !result.Ok || payload["returnedCount"] != float64(1) || payload["truncated"] != true || payload["complete"] != false {
		t.Fatalf("cap: %s", result.Content)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if result := runner.Call(ctx, call); result.Ok {
		t.Fatal("cancelled traversal succeeded")
	}
	call.ProjectDirs = []string{root, outside}
	if result := runner.Call(context.Background(), call); result.Ok {
		t.Fatal("ambiguous recursive root accepted")
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	call.ProjectDirs = []string{root}
	if result := runner.Call(context.Background(), call); result.Ok || !strings.Contains(result.Content, "ignore_rules_unavailable") || strings.Contains(result.Content, "needle") {
		t.Fatalf("external ignore rules read or hidden: %s", result.Content)
	}
}

func TestFileSearchFilesModeStopsAfterFirstMatch(t *testing.T) {
	root := t.TempDir()
	// The later line exceeds the scanner limit. A paths-only search should
	// finish after finding the first match, without reading/retaining it.
	writePatchTestFile(t, filepath.Join(root, "log.txt"), "needle\n"+strings.Repeat("x", (1<<20)+1))
	result, err := SearchTextFiles(context.Background(), filepath.Join(root, "log.txt"), TextFileSearchOptions{Query: "needle", OutputMode: "files"})
	if err != nil || len(result.Matches) != 1 || result.Matches[0].Text != "" || result.Matches[0].Line != 0 {
		t.Fatalf("files mode: %+v %v", result, err)
	}
	if _, err := SearchTextFiles(context.Background(), root, TextFileSearchOptions{Query: "needle", OutputMode: "unknown"}); err == nil {
		t.Fatal("unsupported output mode accepted")
	}
}

func TestBuiltinFileSearchReportsIncompleteCoverage(t *testing.T) {
	root := t.TempDir()
	large := filepath.Join(root, "large.go")
	content := strings.Repeat("// padding\n", maxFileSearchBytes/10+1) + "// needle\n"
	if err := os.WriteFile(large, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	options := TextFileSearchOptions{Query: "needle", CaseSensitive: true}
	result, err := SearchTextFiles(context.Background(), root, options)
	if err != nil || result.Complete || result.ResultsCapped || len(result.Matches) != 0 || result.Skipped["file_too_large"] != 1 {
		t.Fatalf("missing omission metadata: %+v %v", result, err)
	}
	result, err = SearchTextFiles(context.Background(), large, options)
	if err != nil || !result.Complete || len(result.Matches) != 1 {
		t.Fatalf("explicit search failed: %+v %v", result, err)
	}
	if err := os.Symlink(large, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}
	result, err = SearchTextFiles(context.Background(), root, options)
	if err != nil || result.Skipped["symlink"] != 1 {
		t.Fatalf("symlink omission missing: %+v %v", result, err)
	}
	file, err := os.OpenFile(large, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxFileSearchDirectBytes + 1); err != nil {
		t.Fatal(err)
	}
	file.Close()
	_, err = SearchTextFiles(context.Background(), large, options)
	if searchErr, ok := err.(*TextFileSearchError); !ok || searchErr.Code != "file_too_large" {
		t.Fatalf("unbounded explicit search: %v", err)
	}
}

func TestBuiltinFileSearchReportsReadErrors(t *testing.T) {
	root := t.TempDir()
	// One oversized line exercises a read failure, without permission-dependent fixtures.
	path := filepath.Join(root, "broken.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", maxFileSearchBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := SearchTextFiles(context.Background(), root, TextFileSearchOptions{Query: "needle"})
	if err != nil || result.Complete || result.Skipped["read_error"] != 1 {
		t.Fatalf("read error hidden: %+v %v", result, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	result, err = SearchTextFiles(context.Background(), root, TextFileSearchOptions{Query: "needle"})
	if err != nil || !result.Complete || len(result.Skipped) != 0 {
		t.Fatalf("empty search not complete: %+v %v", result, err)
	}
}

func TestBuiltinFileSkillWriteRead(t *testing.T) {
	home := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(home))

	write := runner.Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill","path":"demo/SKILL.md","content":"hello\nworld\n"}`),
	})
	if !write.Ok {
		t.Fatalf("write should succeed: %+v", write)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "demo", "SKILL.md")); err != nil {
		t.Fatalf("file missing: %v", err)
	}

	read := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"skill","path":"demo/SKILL.md"}`),
	})
	if !read.Ok {
		t.Fatalf("read should succeed: %+v", read)
	}
	payload := decodeToolResult(t, read)
	if payload["content"] != "hello\nworld\n" {
		t.Fatalf("unexpected content: %+v", payload)
	}
}

func TestBuiltinFileListAllowsSkillRoot(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "skills", "demo"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", ".reserved"), []byte("hidden"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, args := range []string{
		`{"scope":"skill","path":"."}`,
		`{"scope":"skill"}`,
	} {
		res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
			Name: FileList,
			Args: json.RawMessage(args),
		})
		if !res.Ok {
			t.Fatalf("list root should succeed with args %s: %+v", args, res)
		}
		payload := decodeToolResult(t, res)
		entries, ok := payload["entries"].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("unexpected entries with args %s: %+v", args, payload)
		}
		entry, ok := entries[0].(map[string]any)
		if !ok || entry["name"] != "demo" {
			t.Fatalf("unexpected entry with args %s: %+v", args, entries[0])
		}
	}
}

func TestBuiltinFilePluginScopeIsReadOnlyAndHidesRuntimeFiles(t *testing.T) {
	home := t.TempDir()
	pluginDir := filepath.Join(home, "plugins", "example")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.yaml"), []byte("id: example\nname: Example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, ".pudding-mcp-overrides.yaml"), []byte("secret: hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))

	read := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"plugin","path":"example/plugin.yaml"}`),
	})
	if !read.Ok || !strings.Contains(read.Content, "Example") {
		t.Fatalf("read App manifest: %+v", read)
	}
	list := runner.Call(context.Background(), Call{
		Name: FileList,
		Args: json.RawMessage(`{"scope":"plugin","path":"example"}`),
	})
	if !list.Ok || strings.Contains(list.Content, ".pudding-mcp-overrides.yaml") {
		t.Fatalf("hidden App file leaked from list: %+v", list)
	}
	hidden := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"plugin","path":"example/.pudding-mcp-overrides.yaml"}`),
	})
	if hidden.Ok {
		t.Fatalf("hidden App file should not be readable: %+v", hidden)
	}
	search := runner.Call(context.Background(), Call{
		Name: FileSearch,
		Args: json.RawMessage(`{"scope":"plugin","path":"example","query":"hidden"}`),
	})
	if !search.Ok || strings.Contains(search.Content, ".pudding-mcp-overrides.yaml") || strings.Contains(search.Content, "secret: hidden") {
		t.Fatalf("hidden App file leaked from search: %+v", search)
	}
	write := runner.Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"plugin","path":"example/plugin.yaml","content":"changed"}`),
	})
	if write.Ok {
		t.Fatalf("App scope should be read-only: %+v", write)
	}
}

func TestBuiltinFileRejectsUnknownManagedScope(t *testing.T) {
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"unknown","path":"demo/SKILL.md","content":"x"}`),
	})
	if res.Ok {
		t.Fatalf("unknown scope should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "invalid_scope" {
		t.Fatalf("unexpected reason: %+v", payload)
	}
}

func TestBuiltinFileRejectsSymlinkedManagedRoot(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, "skills")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill","path":"demo/SKILL.md","content":"outside"}`),
	})
	if res.Ok {
		t.Fatalf("symlinked skill root should fail: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(outside, "demo", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("write escaped managed root: %v", err)
	}
}

func TestBuiltinFileAllowsPluginSkillIDInGlobalScope(t *testing.T) {
	homeDir := t.TempDir()
	result := NewBuiltinRunner(WithHomeDir(homeDir)).Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill","path":"skill-creator/SKILL.md","content":"shadow"}`),
	})
	if !result.Ok {
		t.Fatalf("App Skill id should not reserve global Skill writes: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(homeDir, "skills", "skill-creator", "SKILL.md")); err != nil {
		t.Fatalf("global Skill file was not created: %v", err)
	}
}

func TestBuiltinFileTempScopeHidesCodeScratch(t *testing.T) {
	homeDir := t.TempDir()
	scratch := filepath.Join(homeDir, "temp", ".code", "sess_other")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "secret.txt"), []byte("private session data"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(homeDir))

	list := runner.Call(context.Background(), Call{
		Name: FileList,
		Args: json.RawMessage(`{"scope":"temp","path":"."}`),
	})
	if !list.Ok || strings.Contains(list.Content, ".code") {
		t.Fatalf("temp list exposed code scratch: %+v", list)
	}
	read := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":".code/sess_other/secret.txt"}`),
	})
	if read.Ok || strings.Contains(read.Content, "private session data") {
		t.Fatalf("temp read exposed code scratch: %+v", read)
	}
}

func TestBuiltinFileSkillWritePreservesOtherFiles(t *testing.T) {
	home := t.TempDir()
	publishedDir := filepath.Join(home, "skills", "demo", "assets")
	if err := os.MkdirAll(publishedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "demo", "SKILL.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publishedDir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill","path":"demo/SKILL.md","content":"new"}`),
	})
	if !res.Ok {
		t.Fatalf("write should succeed: %+v", res)
	}
	data, err := os.ReadFile(filepath.Join(home, "skills", "demo", "SKILL.md"))
	if err != nil || string(data) != "new" {
		t.Fatalf("skill should be patched directly: %q %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "demo", "assets", "icon.svg")); err != nil {
		t.Fatalf("existing icon should be preserved: %v", err)
	}
}

func TestBuiltinFileSkillDeleteIsDirect(t *testing.T) {
	home := t.TempDir()
	publishedDir := filepath.Join(home, "skills", "demo", "assets")
	if err := os.MkdirAll(publishedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "skills", "demo", "SKILL.md"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publishedDir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileDelete,
		Args: json.RawMessage(`{"scope":"skill","path":"demo/assets/icon.svg"}`),
	})
	if !res.Ok {
		t.Fatalf("delete should succeed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "demo", "assets", "icon.svg")); !os.IsNotExist(err) {
		t.Fatalf("icon should be deleted directly, stat err=%v", err)
	}
}

func TestBuiltinFileSkillWriteRejectsMissingPathThroughEscapingSymlink(t *testing.T) {
	homeDir := t.TempDir()
	skillsRoot := filepath.Join(homeDir, "skills")
	outside := t.TempDir()
	if err := os.MkdirAll(skillsRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(skillsRoot, "outside-link")); err != nil {
		t.Fatal(err)
	}

	result := NewBuiltinRunner(WithHomeDir(homeDir)).Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill","path":"outside-link/new/demo.txt","content":"escaped"}`),
	})
	if result.Ok {
		t.Fatalf("write through escaping symlink should fail: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(outside, "new", "demo.txt")); !os.IsNotExist(err) {
		t.Fatalf("write escaped the Skill root: %v", err)
	}
}

func TestBuiltinFileProjectScopeReadWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(root, "dir", "note.txt")
	if err := os.WriteFile(notePath, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	resolvedNotePath, err := filepath.EvalSymlinks(notePath)
	if err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	list := runner.Call(context.Background(), Call{
		Name:        FileList,
		Args:        json.RawMessage(`{"scope":"project","path":"dir"}`),
		ProjectDirs: []string{root},
	})
	if !list.Ok {
		t.Fatalf("project list should succeed: %+v", list)
	}
	listPayload := decodeToolResult(t, list)
	if listPayload["root"] != root || listPayload["relativePath"] != "dir" {
		t.Fatalf("project metadata missing: %+v", listPayload)
	}
	entries := listPayload["entries"].([]any)
	if len(entries) != 1 || entries[0].(map[string]any)["path"] != resolvedNotePath {
		t.Fatalf("project entries should use absolute paths: %+v", entries)
	}

	read := runner.Call(context.Background(), Call{
		Name:        FileRead,
		Args:        json.RawMessage(`{"scope":"project","path":"` + filepath.ToSlash(notePath) + `"}`),
		ProjectDirs: []string{root},
	})
	if !read.Ok {
		t.Fatalf("project read should succeed: %+v", read)
	}
	readPayload := decodeToolResult(t, read)
	if readPayload["content"] != "hello" || readPayload["path"] != resolvedNotePath {
		t.Fatalf("unexpected project read payload: %+v", readPayload)
	}

	writePath := filepath.Join(root, "nested", "created.txt")
	write := runner.Call(context.Background(), Call{
		Name:        FileWrite,
		Args:        json.RawMessage(`{"scope":"project","path":"` + filepath.ToSlash(writePath) + `","content":"created"}`),
		ProjectDirs: []string{root},
	})
	if !write.Ok {
		t.Fatalf("project write should succeed: %+v", write)
	}
	data, err := os.ReadFile(writePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "created" {
		t.Fatalf("project write content = %q", data)
	}
}

func TestBuiltinFileListReturnsEveryProjectRoot(t *testing.T) {
	firstRoot := t.TempDir()
	secondRoot := t.TempDir()
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	list := runner.Call(context.Background(), Call{
		Name:        FileList,
		Args:        json.RawMessage(`{"scope":"project","path":"."}`),
		ProjectDirs: []string{firstRoot, secondRoot},
	})
	if !list.Ok {
		t.Fatalf("multi-root project list should succeed: %+v", list)
	}
	payload := decodeToolResult(t, list)
	if payload["rootCount"] != float64(2) || payload["path"] != "." {
		t.Fatalf("unexpected multi-root metadata: %+v", payload)
	}
	entries := payload["entries"].([]any)
	if len(entries) != 2 || entries[0].(map[string]any)["path"] != firstRoot || entries[1].(map[string]any)["path"] != secondRoot {
		t.Fatalf("multi-root project entries missing: %+v", entries)
	}
	roots := payload["projectRoots"].([]any)
	if len(roots) != 2 || roots[0] != firstRoot || roots[1] != secondRoot {
		t.Fatalf("multi-root project roots missing: %+v", roots)
	}
}

func TestBuiltinFileCopyFileAndDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src", "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "note.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "nested", "deep.txt"), []byte("deep"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	fileCopy := runner.Call(context.Background(), Call{
		Name:        FileCopy,
		Args:        json.RawMessage(`{"from":{"scope":"project","path":"src/note.txt"},"to":{"scope":"project","path":"copy/note.txt"}}`),
		ProjectDirs: []string{root},
	})
	if !fileCopy.Ok {
		t.Fatalf("file copy should succeed: %+v", fileCopy)
	}
	data, err := os.ReadFile(filepath.Join(root, "copy", "note.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("copied file content = %q", data)
	}

	dirWithoutRecursive := runner.Call(context.Background(), Call{
		Name:        FileCopy,
		Args:        json.RawMessage(`{"from":{"scope":"project","path":"src"},"to":{"scope":"project","path":"copy/src"}}`),
		ProjectDirs: []string{root},
	})
	if dirWithoutRecursive.Ok {
		t.Fatalf("directory copy without recursive should fail")
	}
	payload := decodeToolResult(t, dirWithoutRecursive)
	if payload["reason"] != "recursive_required" {
		t.Fatalf("unexpected recursive reason: %+v", payload)
	}

	dirCopy := runner.Call(context.Background(), Call{
		Name:        FileCopy,
		Args:        json.RawMessage(`{"from":{"scope":"project","path":"src"},"to":{"scope":"project","path":"copy/src"},"recursive":true}`),
		ProjectDirs: []string{root},
	})
	if !dirCopy.Ok {
		t.Fatalf("directory copy should succeed: %+v", dirCopy)
	}
	deep, err := os.ReadFile(filepath.Join(root, "copy", "src", "nested", "deep.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(deep) != "deep" {
		t.Fatalf("copied nested content = %q", deep)
	}
}

func TestBuiltinFileProjectRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "note.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		Name:        FileRead,
		Args:        json.RawMessage(`{"scope":"project","path":"` + filepath.ToSlash(outside) + `"}`),
		ProjectDirs: []string{root},
	})
	if res.Ok {
		t.Fatalf("outside project read should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "path_not_authorized" {
		t.Fatalf("unexpected reason: %+v", payload)
	}
}

func TestBuiltinFileProjectRequiresDirs(t *testing.T) {
	res := NewBuiltinRunner(WithHomeDir(t.TempDir())).Call(context.Background(), Call{
		Name: FileList,
		Args: json.RawMessage(`{"scope":"project","path":"."}`),
	})
	if res.Ok {
		t.Fatalf("project list without dirs should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "project_dirs_required" {
		t.Fatalf("unexpected reason: %+v", payload)
	}
}

func TestBuiltinFileStatSearchAndSlice(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(root, "docs", "note.txt")
	content := strings.Join([]string{
		"first line",
		"alpha marker",
		"middle line",
		"omega marker",
		"last line",
	}, "\n")
	if err := os.WriteFile(notePath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "pkg", "junk.txt"), []byte("marker should be skipped"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	stat := runner.Call(context.Background(), Call{
		Name:        FileStat,
		Args:        json.RawMessage(`{"scope":"project","path":"` + filepath.ToSlash(notePath) + `"}`),
		ProjectDirs: []string{root},
	})
	if !stat.Ok {
		t.Fatalf("stat should succeed: %+v", stat)
	}
	statPayload := decodeToolResult(t, stat)
	if statPayload["exists"] != true || statPayload["type"] != "file" {
		t.Fatalf("unexpected stat payload: %+v", statPayload)
	}

	search := runner.Call(context.Background(), Call{
		Name:        FileSearch,
		Args:        json.RawMessage(`{"scope":"project","path":".","query":"marker"}`),
		ProjectDirs: []string{root},
	})
	if !search.Ok {
		t.Fatalf("search should succeed: %+v", search)
	}
	searchPayload := decodeToolResult(t, search)
	matches := searchPayload["matches"].([]any)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches outside skipped dirs, got %+v", matches)
	}

	slice := runner.Call(context.Background(), Call{
		Name:        FileSlice,
		Args:        json.RawMessage(`{"scope":"project","path":"docs/note.txt","start":2,"end":3}`),
		ProjectDirs: []string{root},
	})
	if !slice.Ok {
		t.Fatalf("slice should succeed: %+v", slice)
	}
	slicePayload := decodeToolResult(t, slice)
	if slicePayload["content"] != "alpha marker\nmiddle line" || slicePayload["numberedContent"] != "2: alpha marker\n3: middle line" {
		t.Fatalf("unexpected slice payload: %+v", slicePayload)
	}

	reverseTail := runner.Call(context.Background(), Call{
		Name:        FileSlice,
		Args:        json.RawMessage(`{"scope":"project","path":"docs/note.txt","origin":"end","lines":2,"order":"reverse"}`),
		ProjectDirs: []string{root},
	})
	if !reverseTail.Ok {
		t.Fatalf("reverse tail slice should succeed: %+v", reverseTail)
	}
	tailPayload := decodeToolResult(t, reverseTail)
	if tailPayload["content"] != "last line\nomega marker" || tailPayload["numberedContent"] != "5: last line\n4: omega marker" {
		t.Fatalf("unexpected tail payload: %+v", tailPayload)
	}
}

func TestBuiltinFileReadRejectsLargeText(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "large.txt"), []byte(strings.Repeat("x", maxFileReadWholeBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"large.txt"}`),
	})
	if res.Ok {
		t.Fatalf("large read should fail with guidance: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "file_too_large" || payload["hint"] == "" {
		t.Fatalf("unexpected large read payload: %+v", payload)
	}
}

func TestBuiltinFileSearchSupportsRegexGlobsAndContext(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"src/main.go":      "package main\n\nfunc RunServer() {}\n\nfunc stop() {}\n",
		"src/main_test.go": "package main\n\nfunc TestRunServer() {}\n",
		"docs/readme.md":   "RunServer is documented here\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	result := runner.Call(context.Background(), Call{
		Name: FileSearch,
		Args: json.RawMessage(`{
			"scope":"project",
			"path":".",
			"query":"func\\s+runserver",
			"mode":"regex",
			"case_sensitive":false,
			"include_globs":["**/*.go"],
			"exclude_globs":["**/*_test.go"],
			"context_lines":1
		}`),
		ProjectDirs: []string{root},
	})
	if !result.Ok {
		t.Fatalf("search should succeed: %+v", result)
	}
	payload := decodeToolResult(t, result)
	matches := payload["matches"].([]any)
	if len(matches) != 1 {
		t.Fatalf("expected one filtered match, got %+v", matches)
	}
	match := matches[0].(map[string]any)
	if match["line"] != float64(3) || match["lineStart"] != float64(2) || match["lineEnd"] != float64(4) {
		t.Fatalf("unexpected match range: %+v", match)
	}
	if match["excerpt"] != "\nfunc RunServer() {}\n" {
		t.Fatalf("unexpected search excerpt: %q", match["excerpt"])
	}
	if payload["searchType"] != "regex" || payload["caseSensitive"] != false {
		t.Fatalf("unexpected search metadata: %+v", payload)
	}
}

func TestBuiltinFileSearchRejectsInvalidPatterns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	for _, tt := range []struct {
		name   string
		args   string
		reason string
	}{
		{name: "regex", args: `{"scope":"project","path":".","query":"[","mode":"regex"}`, reason: "invalid_regex"},
		{name: "glob", args: `{"scope":"project","path":".","query":"main","include_globs":["["]}`, reason: "invalid_glob"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			result := runner.Call(context.Background(), Call{Name: FileSearch, Args: json.RawMessage(tt.args), ProjectDirs: []string{root}})
			if result.Ok {
				t.Fatalf("invalid pattern should fail: %+v", result)
			}
			payload := decodeToolResult(t, result)
			if payload["reason"] != tt.reason {
				t.Fatalf("reason=%v want %s", payload["reason"], tt.reason)
			}
		})
	}
}

func TestBuiltinFileSearchContextLinesRangeAndRecovery(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))
	for _, lines := range []int{-1, 0, 5, 6, 12} {
		args, _ := json.Marshal(map[string]any{"scope": "project", "path": "main.go", "query": "main", "context_lines": lines})
		result := runner.Call(context.Background(), Call{Name: FileSearch, Args: args, ProjectDirs: []string{root}})
		if lines == 0 || lines == 5 {
			if !result.Ok {
				t.Fatalf("valid context_lines=%d rejected: %s", lines, result.Content)
			}
			continue
		}
		payload := decodeToolResult(t, result)
		detail, _ := payload["detail"].(string)
		if result.Ok || payload["reason"] != "invalid_context_lines" || !strings.Contains(detail, "between 0 and 5") || !strings.Contains(detail, "context_lines=0") || !strings.Contains(detail, "builtin_file_slice") {
			t.Fatalf("context_lines=%d must explain range and recovery: %s", lines, result.Content)
		}
	}
}

func TestBuiltinFileReadAllowsUTF8RuneAcrossProbeBoundary(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := strings.Repeat("a", 511) + "中\n"
	if err := os.WriteFile(filepath.Join(tempDir, "boundary.md"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"boundary.md"}`),
	})
	if !res.Ok {
		t.Fatalf("utf-8 boundary read should succeed: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["content"] != content {
		t.Fatalf("unexpected boundary content: %+v", payload)
	}
}

func TestBuiltinFileReadTreatsTypeScriptAsTextDespiteSystemMIME(t *testing.T) {
	if err := mime.AddExtensionType(".ts", "video/mp2t"); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	content := "export const value: string = \"pudding\"\n"
	if err := os.WriteFile(filepath.Join(tempDir, "example.ts"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tempDir, "binary.ts"), []byte{'T', 'S', 0, 1}, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))

	textResult := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"example.ts"}`),
	})
	if !textResult.Ok {
		t.Fatalf("TypeScript text should be readable: %+v", textResult)
	}
	if payload := decodeToolResult(t, textResult); payload["content"] != content {
		t.Fatalf("unexpected TypeScript content: %+v", payload)
	}

	binaryResult := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"binary.ts"}`),
	})
	if binaryResult.Ok {
		t.Fatalf("binary data with a TypeScript extension should fail: %+v", binaryResult)
	}
	if payload := decodeToolResult(t, binaryResult); payload["reason"] != "binary_file" {
		t.Fatalf("unexpected binary TypeScript result: %+v", payload)
	}
}

func TestBuiltinFileReadTreatsUTF8SVGAsText(t *testing.T) {
	root := t.TempDir()
	content := "<svg xmlns=\"http://www.w3.org/2000/svg\">\n  <path d=\"M0 0h1v1H0z\"/>\n</svg>\n"
	if err := os.WriteFile(filepath.Join(root, "icon.svg"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "binary.svg"), []byte{'<', 's', 'v', 'g', 0, '>'}, 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(t.TempDir()))

	read := runner.Call(context.Background(), Call{
		Name:        FileRead,
		Args:        json.RawMessage(`{"scope":"project","path":"icon.svg"}`),
		ProjectDirs: []string{root},
	})
	if !read.Ok {
		t.Fatalf("UTF-8 SVG read should succeed: %+v", read)
	}
	if payload := decodeToolResult(t, read); payload["content"] != content {
		t.Fatalf("unexpected SVG content: %+v", payload)
	}

	slice := runner.Call(context.Background(), Call{
		Name:        FileSlice,
		Args:        json.RawMessage(`{"scope":"project","path":"icon.svg","start":2,"end":2}`),
		ProjectDirs: []string{root},
	})
	if !slice.Ok {
		t.Fatalf("UTF-8 SVG slice should succeed: %+v", slice)
	}
	if payload := decodeToolResult(t, slice); payload["content"] != `  <path d="M0 0h1v1H0z"/>` {
		t.Fatalf("unexpected SVG slice: %+v", payload)
	}

	binary := runner.Call(context.Background(), Call{
		Name:        FileRead,
		Args:        json.RawMessage(`{"scope":"project","path":"binary.svg"}`),
		ProjectDirs: []string{root},
	})
	if binary.Ok {
		t.Fatalf("SVG containing NUL should fail: %+v", binary)
	}
	if payload := decodeToolResult(t, binary); payload["reason"] != "binary_file" {
		t.Fatalf("unexpected binary SVG result: %+v", payload)
	}
}

func TestBuiltinFileReadRejectsBinaryBeforeLargeHint(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data := append([]byte{0, 1, 2, 3}, []byte(strings.Repeat("x", maxFileReadWholeBytes+1))...)
	if err := os.WriteFile(filepath.Join(tempDir, "large.wav"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	res := NewBuiltinRunner(WithHomeDir(home)).Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"temp","path":"large.wav"}`),
	})
	if res.Ok {
		t.Fatalf("large binary read should fail: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["reason"] != "unsupported_media" || payload["recommendedTool"] != MediaRead {
		t.Fatalf("large audio should recommend media_read, got %+v", payload)
	}
}

func TestBuiltinTextReadersRejectImageWithMediaHint(t *testing.T) {
	home := t.TempDir()
	tempDir := filepath.Join(home, "temp")
	if err := os.MkdirAll(tempDir, 0o700); err != nil {
		t.Fatal(err)
	}
	imagePath := filepath.Join(tempDir, "image.png")
	imageBytes := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 0, 'I', 'E', 'N', 'D'}
	if err := os.WriteFile(imagePath, imageBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	runner := NewBuiltinRunner(WithHomeDir(home))
	for _, name := range []string{FileRead, FileSlice} {
		res := runner.Call(context.Background(), Call{
			SessionID: "sess_img",
			Name:      name,
			Args:      json.RawMessage(`{"scope":"temp","path":"image.png"}`),
		})
		if res.Ok || len(res.Attachments) != 0 || len(res.ContextAttachments) != 0 {
			t.Fatalf("%s should reject image without routing it: %+v", name, res)
		}
		payload := decodeToolResult(t, res)
		if payload["reason"] != "unsupported_media" || payload["recommendedTool"] != MediaRead {
			t.Fatalf("%s should recommend media_read: %+v", name, payload)
		}
	}
}

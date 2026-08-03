package tool

import (
	"context"
	"encoding/json"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestBuiltinFileAppScopeIsReadOnlyAndHidesRuntimeFiles(t *testing.T) {
	home := t.TempDir()
	appDir := filepath.Join(home, "apps", "example")
	if err := os.MkdirAll(appDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "app.yaml"), []byte("id: example\nname: Example\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, ".pudding-mcp-overrides.yaml"), []byte("secret: hidden\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(home))

	read := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"app","path":"example/app.yaml"}`),
	})
	if !read.Ok || !strings.Contains(read.Content, "Example") {
		t.Fatalf("read App manifest: %+v", read)
	}
	list := runner.Call(context.Background(), Call{
		Name: FileList,
		Args: json.RawMessage(`{"scope":"app","path":"example"}`),
	})
	if !list.Ok || strings.Contains(list.Content, ".pudding-mcp-overrides.yaml") {
		t.Fatalf("hidden App file leaked from list: %+v", list)
	}
	hidden := runner.Call(context.Background(), Call{
		Name: FileRead,
		Args: json.RawMessage(`{"scope":"app","path":"example/.pudding-mcp-overrides.yaml"}`),
	})
	if hidden.Ok {
		t.Fatalf("hidden App file should not be readable: %+v", hidden)
	}
	search := runner.Call(context.Background(), Call{
		Name: FileSearch,
		Args: json.RawMessage(`{"scope":"app","path":"example","query":"hidden"}`),
	})
	if !search.Ok || strings.Contains(search.Content, ".pudding-mcp-overrides.yaml") || strings.Contains(search.Content, "secret: hidden") {
		t.Fatalf("hidden App file leaked from search: %+v", search)
	}
	write := runner.Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"app","path":"example/app.yaml","content":"changed"}`),
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
	if payload["reason"] != "path_not_allowed" {
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

func TestBuiltinFileAllowsAppSkillIDInGlobalScope(t *testing.T) {
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
		Args:        json.RawMessage(`{"scope":"project","from_path":"src/note.txt","to_path":"copy/note.txt"}`),
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
		Args:        json.RawMessage(`{"scope":"project","from_path":"src","to_path":"copy/src"}`),
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
		Args:        json.RawMessage(`{"scope":"project","from_path":"src","to_path":"copy/src","recursive":true}`),
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

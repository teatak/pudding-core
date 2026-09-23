package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/turnfiles"
)

func TestMutationTrackingForCall(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		call        Call
		wantOK      bool
		wantTargets []string
		wantOrigin  store.FileChangeOrigin
	}{
		{
			name:        "foreground command tracks explicit target",
			call:        Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"touch changed.txt"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "changed.txt")},
			wantOrigin:  store.FileChangeOriginCommandObserved,
		},
		{
			name:        "command target is relative to cwd",
			call:        Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","cwd":"sub","command":"printf hi > generated.txt"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "sub", "generated.txt")},
			wantOrigin:  store.FileChangeOriginCommandObserved,
		},
		{
			name:        "package manager formatter tracks explicit target",
			call:        Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","cwd":"sub","command":"pnpm exec prettier --write view.tsx"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "sub", "view.tsx")},
			wantOrigin:  store.FileChangeOriginCommandObserved,
		},
		{
			name:       "package manager script only observes already tracked files",
			call:       Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"pnpm run format"}`), ProjectDirs: []string{root}},
			wantOK:     true,
			wantOrigin: store.FileChangeOriginCommandObserved,
		},
		{
			name:   "background command is not finalized with the call",
			call:   Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"sleep 10","background":true}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
		{
			name:        "write owns one project path",
			call:        Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"project","path":"dir/file.txt","content":"new"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "dir", "file.txt")},
			wantOrigin:  store.FileChangeOriginStructured,
		},
		{
			name:        "copy owns only destination",
			call:        Call{Name: FileCopy, Args: json.RawMessage(`{"from":{"scope":"project","path":"source.txt"},"to":{"scope":"project","path":"copy.txt"}}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "copy.txt")},
			wantOrigin:  store.FileChangeOriginStructured,
		},
		{
			name:       "project root target does not expand command tracking",
			call:       Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"prettier --write ."}`), ProjectDirs: []string{root}},
			wantOK:     true,
			wantOrigin: store.FileChangeOriginCommandObserved,
		},
		{
			name:       "dynamic command target is excluded",
			call:       Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"touch \"$TARGET\""}`), ProjectDirs: []string{root}},
			wantOK:     true,
			wantOrigin: store.FileChangeOriginCommandObserved,
		},
		{
			name:       "glob command target is excluded",
			call:       Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"gofmt -w *.go"}`), ProjectDirs: []string{root}},
			wantOK:     true,
			wantOrigin: store.FileChangeOriginCommandObserved,
		},
		{
			name:   "non-project write is excluded",
			call:   Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"temp","path":"file.txt","content":"new"}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := MutationTrackingForCall(test.call)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v, tracking = %+v", ok, test.wantOK, got)
			}
			if !ok {
				return
			}
			if len(got.Targets) != len(test.wantTargets) {
				t.Fatalf("targets = %v, want %v", got.Targets, test.wantTargets)
			}
			for i := range test.wantTargets {
				if got.Targets[i] != test.wantTargets[i] {
					t.Fatalf("targets = %v, want %v", got.Targets, test.wantTargets)
				}
			}
			if got.Origin != test.wantOrigin {
				t.Fatalf("origin = %q, want %q", got.Origin, test.wantOrigin)
			}
		})
	}
}

func TestCommandMutationTrackingFollowsShellDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mutation analysis uses POSIX shell syntax")
	}
	root, second := projectContractDir(t), projectContractDir(t)
	for _, name := range []string{"sub", "sub/nested"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(second, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	abs := filepath.Join(second, "absolute.txt")
	tests := []struct {
		name    string
		command string
		want    []string
	}{
		{"redirect after cd", "cd sub && printf after > note.txt", []string{filepath.Join(root, "sub/note.txt")}},
		{"operand after cd", "cd sub && touch note.txt", []string{filepath.Join(root, "sub/note.txt")}},
		{"absolute cd across roots", "cd " + quoteShellArg(second) + " && touch note.txt", []string{filepath.Join(second, "note.txt")}},
		{"compound block keeps cwd", "cd sub && { touch one; touch two; }", []string{filepath.Join(root, "sub/one"), filepath.Join(root, "sub/two")}},
		{"semicolon joins cd success and failure", "cd sub; touch ambiguous.txt", nil},
		{"failure branch retains cwd", "cd sub || touch fallback.txt", []string{filepath.Join(root, "fallback.txt")}},
		{"and followed by semicolon", "cd sub && touch one; touch ambiguous.txt", []string{filepath.Join(root, "sub/one")}},
		{"nested cd", "cd sub && cd nested && touch note.txt", []string{filepath.Join(root, "sub/nested/note.txt")}},
		{"subshell restores cwd", "(cd sub && touch inner.txt); touch outer.txt", []string{filepath.Join(root, "sub/inner.txt"), filepath.Join(root, "outer.txt")}},
		{"subshell redirect uses parent cwd", "(cd sub && touch inner.txt) > output.txt", []string{filepath.Join(root, "sub/inner.txt"), filepath.Join(root, "output.txt")}},
		{"logical symlink parent", "cd link && cd .. && touch note.txt", []string{filepath.Join(root, "note.txt")}},
		{"dynamic cd keeps absolute redirect", "cd \"$DEST\" && printf after > ambiguous.txt; printf after > " + quoteShellArg(abs), []string{abs}},
		{"dynamic cd keeps absolute operand", "cd \"$DEST\" && touch " + quoteShellArg(abs), []string{abs}},
		{"dynamic cd failure retains cwd", "cd \"$DEST\" || touch fallback.txt", []string{filepath.Join(root, "fallback.txt")}},
		{"absolute cd restores known cwd", "cd \"$DEST\"; cd " + quoteShellArg(second) + " && touch note.txt", []string{filepath.Join(second, "note.txt")}},
		{"opaque builtin invalidates cwd", "eval 'cd sub'; touch ambiguous.txt; touch " + quoteShellArg(abs), []string{abs}},
		{"unsupported branch keeps absolute redirect", "if true; then cd sub; printf after > ambiguous.txt; printf after > " + quoteShellArg(abs) + "; fi", []string{abs}},
		{"pipeline does not change parent cwd", "cd sub | printf after > output.txt; touch outer.txt", []string{filepath.Join(root, "output.txt"), filepath.Join(root, "outer.txt")}},
		{"function definition does not execute cd", "move() { cd sub; printf hidden > never.txt; }; printf outer > outer.txt", []string{filepath.Join(root, "outer.txt")}},
		{"function call invalidates cwd", "move() { cd sub; }; move; printf after > ambiguous.txt; touch " + quoteShellArg(abs), []string{abs}},
		{"cd function is not a builtin", "cd() { printf ignored; }; cd sub && touch ambiguous.txt", nil},
		{"redirect-only statement preserves cwd", "> first.txt; touch second.txt", []string{filepath.Join(root, "first.txt"), filepath.Join(root, "second.txt")}},
		{"inline CDPATH prevents relative cd inference", "CDPATH=" + quoteShellArg(second) + " cd sub && touch ambiguous.txt", nil},
		{"negated cd uses failure cwd", "! cd sub && touch fallback.txt", []string{filepath.Join(root, "fallback.txt")}},
		{"function body does not write absolute target", "unused() { printf hidden > " + quoteShellArg(abs) + "; }; printf outer > outer.txt", []string{filepath.Join(root, "outer.txt")}},
		{"argument substitution keeps absolute redirect", "printf '%s' \"$(printf after > " + quoteShellArg(abs) + ")\"", []string{abs}},
		{"assignment substitution keeps absolute redirect", "VALUE=$(printf after > " + quoteShellArg(abs) + ")", []string{abs}},
		{"redirect substitution keeps absolute redirect", "printf after > \"$(printf inner > " + quoteShellArg(abs) + "; printf output.txt)\"", []string{abs}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			call := projectContractCall(CommandRun, []string{root, second}, map[string]any{"scope": "project", "cwd": root, "command": test.command})
			got, ok := MutationTrackingForCall(call)
			sort.Strings(test.want)
			if !ok || got.Origin != store.FileChangeOriginCommandObserved || len(got.Targets) != len(test.want) {
				t.Fatalf("tracking = %+v, ok=%v, want targets=%v", got, ok, test.want)
			}
			for i, target := range test.want {
				if got.Targets[i] != target {
					t.Fatalf("targets=%v, want %v", got.Targets, test.want)
				}
			}
		})
	}
	call := projectContractCall(CommandRun, []string{root, second}, map[string]any{
		"scope": "project", "cwd": root, "command": "cd sub && touch ambiguous.txt; touch " + quoteShellArg(abs), "env": map[string]string{"CDPATH": second},
	})
	if got, ok := MutationTrackingForCall(call); !ok || len(got.Targets) != 1 || got.Targets[0] != abs {
		t.Fatalf("CDPATH must not resolve a relative cd against initial cwd: %+v, ok=%v", got, ok)
	}
}

func TestCommandCDMutationProducesTurnFileChange(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("command fixture uses POSIX shell syntax")
	}
	root, second := projectContractDir(t), projectContractDir(t)
	writeCommandTestFile(t, root, "sub/note.txt", "before\n")
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	call := projectContractCall(CommandRun, []string{root, second}, map[string]any{
		"scope": "project", "cwd": root, "command": "cd sub && printf after > note.txt",
	})
	tracking, ok := MutationTrackingForCall(call)
	if !ok {
		t.Fatal("command did not enable mutation observation")
	}
	tracker := turnfiles.New()
	if err := tracker.BeginCallWithOrigin("turn", "call", call.ProjectDirs, tracking.Targets, tracking.Origin); err != nil {
		t.Fatal(err)
	}
	result := runner.Call(context.Background(), call)
	if !result.Ok || decodeToolResult(t, result)["exitCode"] != float64(0) {
		t.Fatalf("command failed: %+v", result)
	}
	if err := tracker.EndCall("turn", "call"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "sub/note.txt" || changes[0].OldContent != "before\n" || changes[0].NewContent != "after" || changes[0].Origin != store.FileChangeOriginCommandObserved {
		t.Fatalf("cd mutation omitted or attributed to wrong file: targets=%v changes=%+v", tracking.Targets, changes)
	}
}

func TestCommandUnknownDirectoryStillObservesTrackedFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("command fixture uses POSIX shell syntax")
	}
	root := projectContractDir(t)
	writeCommandTestFile(t, root, "sub/note.txt", "before\n")
	tracker := turnfiles.New()
	if err := tracker.BeginCall("turn", "structured", []string{root}, []string{filepath.Join(root, "sub/note.txt")}); err != nil {
		t.Fatal(err)
	}
	writeCommandTestFile(t, root, "sub/note.txt", "intermediate\n")
	if err := tracker.EndCall("turn", "structured"); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	call := projectContractCall(CommandRun, []string{root}, map[string]any{
		"scope": "project", "cwd": root,
		"command": `DEST=sub; cd "$DEST" && printf after > note.txt && printf untracked > unrelated.txt`,
	})
	tracking, ok := MutationTrackingForCall(call)
	if !ok || len(tracking.Targets) != 0 {
		t.Fatalf("dynamic cd must only observe known files: %+v, ok=%v", tracking, ok)
	}
	if err := tracker.BeginCallWithOrigin("turn", "command", call.ProjectDirs, tracking.Targets, tracking.Origin); err != nil {
		t.Fatal(err)
	}
	result := runner.Call(context.Background(), call)
	if !result.Ok || decodeToolResult(t, result)["exitCode"] != float64(0) {
		t.Fatalf("command failed: %+v", result)
	}
	if err := tracker.EndCall("turn", "command"); err != nil {
		t.Fatal(err)
	}
	changes, err := tracker.Finish("turn")
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].Path != "sub/note.txt" || changes[0].OldContent != "before\n" || changes[0].NewContent != "after" || changes[0].Origin != store.FileChangeOriginCommandObserved {
		t.Fatalf("known file observation changed or scanned unrelated file: %+v", changes)
	}
	if content, err := os.ReadFile(filepath.Join(root, "sub/unrelated.txt")); err != nil || string(content) != "untracked" {
		t.Fatalf("untracked fixture was not written: content=%q err=%v", content, err)
	}
}

func TestCommandCDSymlinkTargetsUseShellPathSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("command fixture uses POSIX shell syntax")
	}
	for _, test := range []struct {
		name        string
		command     string
		physicalCWD bool
	}{
		{"file parent uses physical cwd", "cd link && printf after > ../note.txt", true},
		{"cd parent uses logical cwd", "cd link && cd .. && printf after > note.txt", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, second := projectContractDir(t), projectContractDir(t)
			writeCommandTestFile(t, root, "note.txt", "before\n")
			writeCommandTestFile(t, second, "note.txt", "before\n")
			if err := os.Mkdir(filepath.Join(second, "nested"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(filepath.Join(second, "nested"), filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			wantRoot, untouchedRoot := root, second
			if test.physicalCWD {
				wantRoot, untouchedRoot = second, root
			}
			call := projectContractCall(CommandRun, []string{root, second}, map[string]any{
				"scope": "project", "cwd": root, "command": test.command,
			})
			tracking, ok := MutationTrackingForCall(call)
			if !ok {
				t.Fatal("command did not enable mutation observation")
			}
			tracker := turnfiles.New()
			if err := tracker.BeginCallWithOrigin("turn", "call", call.ProjectDirs, tracking.Targets, tracking.Origin); err != nil {
				t.Fatal(err)
			}
			runner := NewBuiltinRunner()
			t.Cleanup(func() { _ = runner.Close() })
			result := runner.Call(context.Background(), call)
			if !result.Ok || decodeToolResult(t, result)["exitCode"] != float64(0) {
				t.Fatalf("command failed: %+v", result)
			}
			if err := tracker.EndCall("turn", "call"); err != nil {
				t.Fatal(err)
			}
			changes, err := tracker.Finish("turn")
			if err != nil {
				t.Fatal(err)
			}
			if len(tracking.Targets) != 1 || tracking.Targets[0] != filepath.Join(wantRoot, "note.txt") ||
				len(changes) != 1 || changes[0].RootPath != wantRoot || changes[0].Path != "note.txt" || changes[0].OldContent != "before\n" || changes[0].NewContent != "after" {
				t.Fatalf("shell cwd semantics changed: targets=%v changes=%+v wantRoot=%s", tracking.Targets, changes, wantRoot)
			}
			if content, err := os.ReadFile(filepath.Join(untouchedRoot, "note.txt")); err != nil || string(content) != "before\n" {
				t.Fatalf("other root unexpectedly changed: content=%q err=%v", content, err)
			}
		})
	}
}

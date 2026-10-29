//go:build !windows

package tool

import (
	"encoding/json"
	"github.com/teatak/pudding-core/internal/home"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChromeScreenshotTemplateSessionGrant(t *testing.T) {
	root, tempRoot := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", tempRoot)
	chrome := filepath.Join(t.TempDir(), "Google Chrome.app", "Contents", "MacOS", "Google Chrome")
	if err := os.MkdirAll(filepath.Dir(chrome), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chrome, []byte("#!/bin/sh\necho chrome-out\necho chrome-error >&2\nexit 7\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "page.html"), []byte("<html></html>"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := quoteShellArg(chrome) + ` --headless=new --disable-gpu --hide-scrollbars --window-size=1040,660 --user-data-dir="$TMPDIR/chrome-pelican" --screenshot="` + filepath.Join(root, "one.png") + `" "file://` + filepath.Join(root, "page.html") + `" 2>/dev/null; echo done`
	makeCall := func(command string) Call {
		raw, _ := json.Marshal(map[string]any{"scope": "project", "execution": "host", "host_access_reason": "render local HTML", "command": command})
		return Call{Name: CommandRun, Args: raw, ProjectDirs: []string{root}}
	}
	grant := CommandSessionGrantForCall(makeCall(base))
	if grant == nil {
		t.Fatal("reported Chrome template cannot be remembered")
	}
	canonicalTemp, err := filepath.EvalSymlinks(tempRoot)
	if err != nil {
		t.Fatal(err)
	}
	if grant.ProfilePath != filepath.Join(canonicalTemp, "chrome-pelican") {
		t.Fatalf("profile not bound to resolved temp directory: %q", grant.ProfilePath)
	}
	repeat := strings.ReplaceAll(strings.ReplaceAll(base, "one.png", "two.png"), "1040,660", "1200,900")
	if next := CommandSessionGrantForCall(makeCall(repeat)); next == nil || next.Key != grant.Key {
		t.Fatal("filename/viewport change discarded session grant")
	}
	for _, command := range []string{
		strings.Replace(base, `"$TMPDIR/chrome-pelican"`, `$TMPDIR/chrome-pelican`, 1),
		strings.Replace(base, `"$TMPDIR/chrome-pelican"`, `"$OTHER/chrome-pelican"`, 1),
		strings.Replace(base, `"$TMPDIR/chrome-pelican"`, `"${TMPDIR:-/tmp}/chrome-pelican"`, 1),
		strings.Replace(base, `"$TMPDIR/chrome-pelican"`, `"$TMPDIR/"`, 1),
		strings.Replace(base, `"$TMPDIR/chrome-pelican"`, `"$TMPDIR/../outside"`, 1),
		strings.Replace(base, "2>/dev/null", "2>private.log", 1),
		strings.Replace(base, "2>/dev/null", ">/dev/null", 1),
		strings.Replace(base, "echo done", "echo $(touch side-effect)", 1),
		strings.Replace(base, "echo done", "rm page.html", 1),
		strings.Replace(base, "; echo done", " && echo done", 1),
		base + "; echo more",
		strings.Replace(base, "file://"+filepath.Join(root, "page.html"), "https://example.com", 1),
		strings.Replace(base, "--headless=new", "--headless=new --remote-debugging-port=9222", 1),
	} {
		if got := CommandSessionGrantForCall(makeCall(command)); got != nil {
			t.Fatalf("unsupported template received grant: %s", command)
		}
	}
	scratchRoot, scratch, err := home.OpenSessionScratch(t.TempDir(), "session")
	if err != nil {
		t.Fatal(err)
	}
	scratchRoot.Close()
	scratchCommand := strings.Replace(base, filepath.Join(root, "one.png"), filepath.Join(scratch, "preview.png"), 1)
	if CommandSessionGrantForCall(makeCall(scratchCommand)) != nil {
		t.Fatal("unowned scratch received a grant")
	}
	owned := makeCall(scratchCommand)
	owned.CommandArtifactDir = filepath.Dir(scratch)
	if CommandSessionGrantForCall(owned) == nil {
		t.Fatal("session-owned scratch screenshot cannot reuse approval")
	}
	foreign := makeCall(strings.Replace(scratchCommand, filepath.Join(scratch, "preview.png"), filepath.Join(t.TempDir(), "preview.png"), 1))
	foreign.CommandArtifactDir = owned.CommandArtifactDir
	if CommandSessionGrantForCall(foreign) != nil {
		t.Fatal("foreign output received a grant")
	}
	// The approved invocation must preserve the original redirection and trailing
	// echo semantics, while passing a literal profile path to the executable.
	executable, args, env, err := grant.invocation(makeCall(base))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(args[1], "$TMPDIR") {
		t.Fatal("execution retained a dynamic profile")
	}
	cmd := exec.Command(executable, args...)
	cmd.Dir = root
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil || string(output) != "chrome-out\ndone\n" {
		t.Fatalf("shell template semantics changed: %q err=%v", output, err)
	}
	// A later symlink replacement must revoke this capability before execution.
	if err := os.Symlink(t.TempDir(), filepath.Join(tempRoot, "chrome-pelican")); err != nil {
		t.Fatal(err)
	}
	if got := CommandSessionGrantForCall(makeCall(base)); got != nil {
		t.Fatal("profile escaped temp scope")
	}
	if _, _, _, err := grant.invocation(makeCall(base)); err == nil {
		t.Fatal("stale profile grant executed")
	}
}

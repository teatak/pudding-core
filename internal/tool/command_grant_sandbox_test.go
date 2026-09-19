package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestSandboxCommandGrantBoundaries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX grants")
	}
	root := t.TempDir()
	makeCall := func(command string, env map[string]string) Call {
		raw, _ := json.Marshal(map[string]any{"scope": "project", "command": command, "env": env})
		return Call{Name: CommandRun, Args: raw, ProjectDirs: []string{root}, CommandStateKey: "session:test"}
	}
	base := makeCall("cat file.txt", map[string]string{"PYTHONPATH": root})
	grant := CommandSessionGrantForCall(base)
	if grant == nil || grant.Kind != "sandbox_command" || grant.Execution != CommandExecutionSandbox {
		t.Fatalf("grant=%+v", grant)
	}
	risk, _ := ClassifyToolCallForProject(base.Name, base.Args, base.ProjectDirs)
	if risk.LowRisk || !slices.Contains(risk.ApprovalReasons, "custom_environment") {
		t.Fatalf("risk=%+v", risk)
	}
	for _, tc := range []struct {
		name, command string
		env           map[string]string
		eligible      bool
	}{
		{"same", "cat file.txt", map[string]string{"PYTHONPATH": root}, true},
		{"venv", "cat file.txt", map[string]string{"VIRTUAL_ENV": root}, true},
		{"unsafe env", "cat file.txt", map[string]string{"NODE_OPTIONS": "--require=x"}, false},
		{"path override", "cat file.txt", map[string]string{"PATH": "/usr/bin"}, false},
		{"external libs", "cat file.txt", map[string]string{"PYTHONPATH": t.TempDir()}, false},
		{"empty libs", "cat file.txt", map[string]string{"PYTHONPATH": ""}, false},
		{"implicit current dir", "cat file.txt", map[string]string{"PYTHONPATH": root + ":"}, false},
		{"inline env", "LANG=C cat file.txt", nil, false},
		{"negation", "! cat file.txt", nil, false},
		{"glob", "cat *.txt", nil, false},
		{"tilde", "cat ~/file.txt", nil, false},
		{"escape", "cat file\\ name.txt", nil, false},
		{"compound", "cat file.txt; cat other.txt", nil, false},
		{"redirect", "cat file.txt > out.txt", nil, false},
		{"shell wrapper", "sh -c 'cat file.txt'", nil, false},
		{"destructive", "rm file.txt", nil, false},
		{"push", "git push", nil, false},
		{"credentials", "security list-keychains", nil, false},
		{"system", "launchctl list", nil, false},
		{"substitution", "cat $(echo file.txt)", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := CommandSessionGrantForCall(makeCall(tc.command, tc.env))
			if (got != nil) != tc.eligible {
				t.Fatalf("grant=%+v", got)
			}
		})
	}
	for _, changed := range []Call{
		makeCall("cat other.txt", map[string]string{"PYTHONPATH": root}),
		makeCall("cat file.txt", map[string]string{"PYTHONPATH": root, "LANG": "C"}),
	} {
		if got := CommandSessionGrantForCall(changed); got == nil || got.Key == grant.Key {
			t.Fatalf("changed invocation reused: %+v", got)
		}
	}
	changed := base
	changed.CommandStateKey = "other"
	if got := CommandSessionGrantForCall(changed); got == nil || got.Key == grant.Key {
		t.Fatal("state boundary not bound")
	}
	changed = base
	changed.CommandSandbox = CommandSandboxBypass
	if CommandSessionGrantForCall(changed) != nil {
		t.Fatal("sandbox grant offered without sandbox")
	}
	changed = base
	changed.ProjectDirs = append(changed.ProjectDirs, filepath.Dir(grant.Executable))
	if CommandSessionGrantForCall(changed) != nil {
		t.Fatal("project-writable executable leased")
	}
	for _, key := range []string{"background", "tty"} {
		var args map[string]any
		_ = json.Unmarshal(base.Args, &args)
		args[key] = true
		changed = base
		changed.Args, _ = json.Marshal(args)
		if CommandSessionGrantForCall(changed) != nil {
			t.Fatalf("%s leased", key)
		}
	}
	if _, _, _, err := grant.invocation(makeCall("cat other.txt", nil)); err == nil {
		t.Fatal("changed args executed")
	}
}

func TestCommandGrantUsesBoundInvocation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX grants")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "file.txt"), []byte("bound"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"scope": "project", "command": "cat file.txt", "env": map[string]string{"PYTHONPATH": root}})
	call := Call{Name: CommandRun, CallID: "bound", ProjectDirs: []string{root}, Args: raw}
	call.CommandGrant = CommandSessionGrantForCall(call)
	if call.CommandGrant == nil {
		t.Fatal("missing candidate")
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	recording := &recordingCommandRunner{sandboxed: true, sandboxKind: "test"}
	runner.setCommandRunner(recording)
	result := runner.Call(context.Background(), call)
	if !result.Ok {
		t.Fatal(result.Content)
	}
	specs := recording.snapshot()
	if len(specs) != 1 || specs[0].Executable != "/bin/sh" || !slices.Equal(specs[0].Args, []string{"-c", "exec " + joinShellCommand(call.CommandGrant.argv)}) || specs[0].SandboxMode != CommandSandboxEnforce {
		t.Fatalf("specs=%+v", specs)
	}
	// A fresh environment is checked but the approved snapshot is executed.
	t.Setenv("LANG", "pudding-changed-locale")
	result = runner.Call(context.Background(), call)
	if result.Ok || !strings.Contains(result.Content, "approval_context_changed") || len(recording.snapshot()) != 1 {
		t.Fatalf("stale execution: %+v", result)
	}
}

func TestChromeGrantRejectsUnboundShellSemantics(t *testing.T) {
	for _, command := range []string{"LANG=C '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome' --headless", "! cat file", "(cat file)", "cat file | cat"} {
		if literalGrantCommand(command) != nil {
			t.Fatalf("accepted shell semantics: %s", command)
		}
	}
}

func TestCommandGrantPreservesExecutableAlias(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX grants")
	}
	bin, root := t.TempDir(), t.TempDir()
	real := filepath.Join(bin, "real-python")
	alias := filepath.Join(bin, "python3")
	if err := os.WriteFile(real, []byte("#!/bin/sh\nprintf '%s' \"$0\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, alias); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":/usr/bin:/bin")
	raw, _ := json.Marshal(map[string]any{"scope": "project", "command": "python3", "env": map[string]string{"PYTHONPATH": root}})
	call := Call{Name: CommandRun, CallID: "alias", ProjectDirs: []string{root}, Args: raw}
	call.CommandGrant = CommandSessionGrantForCall(call)
	if call.CommandGrant == nil {
		t.Fatal("missing alias grant")
	}
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	runner.setCommandRunner(&recordingCommandRunner{sandboxed: true, sandboxKind: "test"})
	result := runner.Call(context.Background(), call)
	payload := decodeCommandPayload(t, result)
	if !result.Ok || payload.Stdout != alias {
		t.Fatalf("launch path changed (breaks interpreter environments): %s", result.Content)
	}
}

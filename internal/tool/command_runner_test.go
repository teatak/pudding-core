package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

type recordingCommandRunner struct {
	mu          sync.Mutex
	specs       []commandSpec
	sandboxed   bool
	sandboxKind string
}

func TestCommandEnvironmentIncludesDesktopToolchainPaths(t *testing.T) {
	t.Setenv("PATH", filepath.Join(t.TempDir(), "custom-bin"))
	env, err := commandEnvironment(nil)
	if err != nil {
		t.Fatal(err)
	}
	pathValue := executableEnvValue(env, "PATH")
	parts := filepath.SplitList(pathValue)
	if len(parts) == 0 || parts[0] != os.Getenv("PATH") {
		t.Fatalf("original PATH must remain first: %q", pathValue)
	}
	for _, want := range commonExecutableDirs() {
		if !slices.Contains(parts, want) {
			t.Fatalf("PATH %q does not contain %q", pathValue, want)
		}
	}
	if strings.Count(pathValue, "/usr/bin") != 1 {
		t.Fatalf("PATH contains duplicate standard directories: %q", pathValue)
	}
}

func TestDirectCommandRunnerResolvesExecutableFromCommandEnvironment(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX shell script")
	}
	binDir := t.TempDir()
	executable := filepath.Join(binDir, "pudding-path-fixture")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nprintf resolved"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin:/bin")
	env, err := commandEnvironment(map[string]string{"PATH": binDir})
	if err != nil {
		t.Fatal(err)
	}
	execution, err := newDirectCommandRunner().Prepare(commandSpec{
		Executable: "pudding-path-fixture",
		CWD:        t.TempDir(),
		Env:        env,
	})
	if err != nil {
		t.Fatal(err)
	}
	output, err := execution.Cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(output) != "resolved" {
		t.Fatalf("output = %q, want resolved", output)
	}
}

func TestResolveExecutableFromEnvDoesNotFallBackToProcessPATH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fixture uses a POSIX executable")
	}
	processBin := t.TempDir()
	executable := filepath.Join(processBin, "pudding-process-path-fixture")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", processBin)

	if _, err := resolveExecutableFromEnv("pudding-process-path-fixture", t.TempDir(), []string{"PATH=" + t.TempDir()}); err == nil {
		t.Fatal("expected supplied command environment to exclude the process PATH")
	}
}

func TestCompareVersionNamesUsesNumericOrder(t *testing.T) {
	if compareVersionNames("v20.18.0", "v9.22.1") <= 0 {
		t.Fatal("expected Node 20 to sort before Node 9")
	}
	if compareVersionNames("v20.10.0", "v20.9.0") <= 0 {
		t.Fatal("expected Node 20.10 to sort before Node 20.9")
	}
}

func (r *recordingCommandRunner) Prepare(spec commandSpec) (*commandExecution, error) {
	r.mu.Lock()
	r.specs = append(r.specs, commandSpec{
		Executable:  spec.Executable,
		Args:        append([]string(nil), spec.Args...),
		CWD:         spec.CWD,
		Env:         append([]string(nil), spec.Env...),
		ProjectDirs: append([]string(nil), spec.ProjectDirs...),
		SandboxMode: spec.SandboxMode,
	})
	r.mu.Unlock()
	execution, err := newDirectCommandRunner().Prepare(spec)
	if execution != nil {
		execution.Sandboxed = r.sandboxed
		execution.SandboxKind = r.sandboxKind
	}
	return execution, err
}

func TestCommandResultsExposeSandboxMetadata(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	runner.setCommandRunner(&recordingCommandRunner{sandboxed: true, sandboxKind: "test-sandbox"})
	root := t.TempDir()

	raw, _ := json.Marshal(map[string]any{
		"scope":   "project",
		"command": commandHelperCommand("sandbox-denied"),
	})
	foreground := runner.Call(context.Background(), Call{
		CallID:      "call_sandbox_metadata",
		Name:        CommandRun,
		Args:        raw,
		ProjectDirs: []string{root},
	})
	payload := decodeCommandPayload(t, foreground)
	if !payload.Sandboxed || payload.SandboxKind != "test-sandbox" || !payload.SandboxDenied {
		t.Fatalf("foreground sandbox metadata missing: %+v", payload)
	}

	background := backgroundToolCall(runner, "sess_sandbox_metadata", root, CommandRun, map[string]any{
		"scope":      "project",
		"command":    commandHelperCommand("sleep", "100"),
		"background": true,
	})
	started := decodeBackgroundProcessPayload(t, background)
	if !started.Sandboxed || started.SandboxKind != "test-sandbox" {
		t.Fatalf("background sandbox metadata missing: %+v", started)
	}

	deniedBackground := backgroundToolCall(runner, "sess_sandbox_metadata", root, CommandRun, map[string]any{
		"scope":      "project",
		"command":    commandHelperCommand("sandbox-denied"),
		"background": true,
	})
	deniedStarted := decodeBackgroundProcessPayload(t, deniedBackground)
	deniedPoll := backgroundToolCall(runner, "sess_sandbox_metadata", root, CommandSession, map[string]any{
		"action":     "poll",
		"process_id": deniedStarted.ProcessID,
		"wait_ms":    1000,
	})
	deniedFinished := decodeBackgroundProcessPayload(t, deniedPoll)
	if deniedFinished.Running || !deniedFinished.SandboxDenied {
		t.Fatalf("background sandbox denial metadata missing: %+v", deniedFinished)
	}
}

func (r *recordingCommandRunner) snapshot() []commandSpec {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]commandSpec(nil), r.specs...)
}

func TestForegroundAndBackgroundCommandsShareRunner(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	recording := &recordingCommandRunner{}
	runner.setCommandRunner(recording)
	root := t.TempDir()

	raw, _ := json.Marshal(map[string]any{
		"scope":   "project",
		"command": commandHelperCommand("report"),
	})
	foreground := runner.Call(context.Background(), Call{
		CallID:      "call_foreground_runner",
		Name:        CommandRun,
		Args:        raw,
		ProjectDirs: []string{root},
	})
	if !foreground.Ok {
		t.Fatalf("foreground command failed: %+v", foreground)
	}

	background := backgroundToolCall(runner, "sess_runner", root, CommandRun, map[string]any{
		"scope":      "project",
		"command":    commandHelperCommand("report"),
		"background": true,
	})
	if !background.Ok {
		t.Fatalf("background command failed: %+v", background)
	}

	specs := recording.snapshot()
	if len(specs) != 2 {
		t.Fatalf("shared runner prepared %d commands, want 2", len(specs))
	}
	for _, spec := range specs {
		if len(spec.ProjectDirs) != 1 || spec.ProjectDirs[0] != root {
			t.Fatalf("runner lost project authorization snapshot: %+v", spec.ProjectDirs)
		}
		if spec.SandboxMode != CommandSandboxEnforce {
			t.Fatalf("runner lost sandbox policy snapshot: %q", spec.SandboxMode)
		}
	}
}

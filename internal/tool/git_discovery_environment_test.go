package tool

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGitToolDiscoveryDistinguishesExecutionFailures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("executable fixture uses a POSIX shell")
	}
	for _, tc := range []struct {
		name   string
		stderr string
		exit   int
		reason string
	}{
		{"not_repository", "fatal: not a git repository (or any of the parent directories): .git", 128, "not_git_repository"},
		{"config_denied", "fatal: unable to access '/isolated/.gitconfig': Operation not permitted", 128, "git_discovery_failed"},
		{"bad_config", "fatal: bad config line 1 in file /isolated/.gitconfig", 128, "git_discovery_failed"},
		{"misleading_config_path", "fatal: bad config line 1 in file /isolated/not a git repository/config", 128, "git_discovery_failed"},
		{"dubious_ownership", "fatal: detected dubious ownership in repository at '/isolated/project'", 128, "git_discovery_failed"},
		{"unsupported_option", "error: unknown option show-toplevel", 129, "git_discovery_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binDir := t.TempDir()
			script := "#!/bin/sh\nprintf '%s\\n' '" + strings.ReplaceAll(tc.stderr, "'", "'\"'\"'") + "' >&2\nexit " + strconv.Itoa(tc.exit) + "\n"
			if err := os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			setCommandEnvironmentSnapshotForTest(t, []string{"PATH=" + binDir})
			root := t.TempDir()
			runner := NewBuiltinRunner()
			t.Cleanup(func() { _ = runner.Close() })
			for _, name := range []string{GitStatus, GitStage} {
				args := map[string]any{"scope": "project"}
				if name == GitStage {
					args["paths"] = []string{"file.txt"}
				}
				call := newGitToolCall("session_git_discovery", "call_git_discovery", root, name, args)
				result := runner.Call(context.Background(), call)
				payload := decodeToolResult(t, result)
				if result.Ok || payload["reason"] != tc.reason || payload["detail"] != tc.stderr {
					t.Fatalf("%s discovery = %+v, want reason %q and detail %q", name, payload, tc.reason, tc.stderr)
				}
			}
		})
	}
}

func TestGitToolDiscoveryPreservesContextFailure(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	for _, tc := range []struct {
		name     string
		deadline bool
		reason   string
	}{
		{"cancelled", false, "cancelled"},
		{"timed_out", true, "timed_out"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if tc.deadline {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			cancel()
			_, failed := resolveGitRepository(ctx, Call{ProjectDirs: []string{t.TempDir()}}, "project", ".")
			if failed == nil || failed.reason != tc.reason {
				t.Fatalf("discovery failure = %+v, want %s", failed, tc.reason)
			}
		})
	}
}

func TestRunGitPreservesConfigIsolationAndHostDefaults(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	for _, name := range []string{"global", "system", "system_disabled", "xdg", "host_default", "isolated"} {
		t.Run(name, func(t *testing.T) {
			setCommandEnvironmentSnapshotForTest(t, nil)
			userHome := t.TempDir()
			t.Setenv("HOME", userHome)
			for _, key := range []string{"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_NOSYSTEM", "XDG_CONFIG_HOME"} {
				t.Setenv(key, "")
				if err := os.Unsetenv(key); err != nil {
					t.Fatal(err)
				}
			}
			// Narrow Git configuration inheritance must not enable arbitrary
			// caller-supplied configuration injection.
			t.Setenv("GIT_CONFIG_COUNT", "1")
			t.Setenv("GIT_CONFIG_KEY_0", "puddingtest.source")
			t.Setenv("GIT_CONFIG_VALUE_0", "injected")
			t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
			writeConfig := func(path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("[puddingtest]\n\tsource = "+name+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			want := name + "\n"
			wantExit := 0
			switch name {
			case "global":
				configPath := filepath.Join(t.TempDir(), "explicit global config")
				writeConfig(configPath)
				t.Setenv("GIT_CONFIG_GLOBAL", configPath)
			case "system", "system_disabled":
				configPath := filepath.Join(t.TempDir(), "system.config")
				writeConfig(configPath)
				t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
				t.Setenv("GIT_CONFIG_SYSTEM", configPath)
				if name == "system" {
					t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
				} else {
					want, wantExit = "", 1
				}
			case "xdg":
				configRoot := t.TempDir()
				writeConfig(filepath.Join(configRoot, "git", "config"))
				t.Setenv("XDG_CONFIG_HOME", configRoot)
			case "host_default":
				writeConfig(filepath.Join(userHome, ".gitconfig"))
			case "isolated":
				if err := os.WriteFile(filepath.Join(userHome, ".gitconfig"), []byte("[invalid\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
				want, wantExit = "", 1
			}
			result := runGit(context.Background(), t.TempDir(), 1024, "config", "--get", "puddingtest.source")
			exit := 0
			if result.err != nil {
				exit = gitExitCode(result.err)
			}
			if exit != wantExit || result.stdout.String() != want || result.stderr.String() != "" {
				t.Fatalf("Git config = stdout %q, stderr %q, exit %d, err %v; want stdout %q and exit %d", result.stdout.String(), result.stderr.String(), exit, result.err, want, wantExit)
			}
		})
	}
}

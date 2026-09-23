package projectgit

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestDiscoverClassifiesGitFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		stderr string
		status string
		code   string
	}{
		{"non_repository", "fatal: not a git repository (or any of the parent directories): .git", "128", CodeNotRepository},
		{"invalid_git_directory", "fatal: not a git repository: '/missing/.git'", "128", CodeNotRepository},
		{"non_repository_without_suffix", "fatal: not a git repository", "128", CodeNotRepository},
		{"configuration_denied", "fatal: unable to access '/Users/test/.gitconfig': Operation not permitted", "128", CodeDiscoveryFailed},
		{"phrase_in_config_path", "fatal: bad config line 1 in file /tmp/not a git repository/config", "128", CodeDiscoveryFailed},
		{"similar_message", "fatal: not a git repository-related configuration", "128", CodeDiscoveryFailed},
		{"other_exit_status", "fatal: repository access failed", "1", CodeDiscoveryFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeDiscoveryGit(t, "cat >&2 <<'GIT_ERROR'\n"+test.stderr+"\nGIT_ERROR\nexit "+test.status+"\n")
			_, err := Discover(context.Background(), t.TempDir())
			if ErrorCode(err) != test.code {
				t.Fatalf("Discover error = %v, code = %q, want %q", err, ErrorCode(err), test.code)
			}
			if test.code != CodeNotRepository && !strings.Contains(err.Error(), test.stderr) {
				t.Fatalf("Discover discarded Git diagnostic: %v", err)
			}
		})
	}
}

func TestDiscoverPreservesExecutionFailures(t *testing.T) {
	t.Run("missing_git", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := Discover(context.Background(), t.TempDir())
		if ErrorCode(err) != CodeGitUnavailable || !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("Discover error = %v, code = %q", err, ErrorCode(err))
		}
	})
	for _, test := range []struct {
		name string
		code string
		err  error
	}{
		{"cancelled", CodeCancelled, context.Canceled},
		{"deadline", CodeTimedOut, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			fakeDiscoveryGit(t, "exit 0\n")
			ctx, cancel := context.WithCancel(context.Background())
			if test.err == context.DeadlineExceeded {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			}
			cancel()
			_, err := Discover(ctx, t.TempDir())
			if ErrorCode(err) != test.code || !errors.Is(err, test.err) {
				t.Fatalf("Discover error = %v, code = %q, want %q wrapping %v", err, ErrorCode(err), test.code, test.err)
			}
		})
	}
}

func TestDiscoverReportsUnavailablePaths(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(root, "missing"), file} {
		_, err := Discover(context.Background(), path)
		if ErrorCode(err) != CodeDiscoveryFailed {
			t.Errorf("Discover(%q) error = %v, code = %q", path, err, ErrorCode(err))
		}
	}
}

func TestGitUsesExplicitConfiguration(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	globalConfig := filepath.Join(root, "global.gitconfig")
	systemConfig := filepath.Join(root, "system.gitconfig")
	writeFile(t, root, "global.gitconfig", "[pudding]\n\tglobalTestValue = selected-global\n")
	writeFile(t, root, "system.gitconfig", "[pudding]\n\tsystemTestValue = selected-system\n")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_SYSTEM", systemConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	for key, want := range map[string]string{
		"pudding.globalTestValue": "selected-global",
		"pudding.systemTestValue": "selected-system",
	} {
		result := run(context.Background(), root, 1024, "config", "--get", key)
		if result.err != nil || strings.TrimSpace(result.stdout) != want {
			t.Errorf("git config %s: stdout = %q, stderr = %q, error = %v", key, result.stdout, result.stderr, result.err)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	result := run(context.Background(), root, 1024, "config", "--get", "pudding.systemTestValue")
	if exitCode(result.err) != 1 || result.stdout != "" {
		t.Fatalf("disabled system config: stdout = %q, error = %v", result.stdout, result.err)
	}
}

func TestDiscoverReportsInvalidExplicitConfiguration(t *testing.T) {
	requireGit(t)
	root := newRepository(t)
	config := filepath.Join(t.TempDir(), "invalid.gitconfig")
	if err := os.WriteFile(config, []byte("[broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	_, err := Discover(context.Background(), root)
	if ErrorCode(err) != CodeDiscoveryFailed || !strings.Contains(err.Error(), config) {
		t.Fatalf("Discover error = %v, code = %q", err, ErrorCode(err))
	}
}

func TestGitUsesXDGConfigurationWithoutGlobalOverride(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	writeFile(t, root, "git/config", "[pudding]\n\txdgTestValue = selected-xdg\n")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	// Restore the original value at cleanup while exercising the unset state.
	t.Setenv("GIT_CONFIG_GLOBAL", "")
	if err := os.Unsetenv("GIT_CONFIG_GLOBAL"); err != nil {
		t.Fatal(err)
	}
	result := run(context.Background(), t.TempDir(), 1024, "config", "--get", "pudding.xdgTestValue")
	if result.err != nil || strings.TrimSpace(result.stdout) != "selected-xdg" {
		t.Fatalf("XDG Git config: stdout = %q, stderr = %q, error = %v", result.stdout, result.stderr, result.err)
	}
}

func TestGitConfigurationEnvironmentIsNarrow(t *testing.T) {
	controls := map[string]string{
		"GIT_CONFIG_GLOBAL":   "",
		"GIT_CONFIG_SYSTEM":   "/explicit/system.gitconfig",
		"GIT_CONFIG_NOSYSTEM": "1",
		"XDG_CONFIG_HOME":     "/explicit/xdg",
	}
	for key, value := range controls {
		t.Setenv(key, value)
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		t.Setenv(key, "untrusted")
	}
	values := make(map[string]string)
	for _, entry := range gitEnvironment() {
		key, value, _ := strings.Cut(entry, "=")
		values[key] = value
	}
	for key, want := range controls {
		if got, ok := values[key]; !ok || got != want {
			t.Errorf("git environment %s = %q, present = %v; want %q", key, got, ok, want)
		}
	}
	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_0", "GIT_CONFIG_VALUE_0"} {
		if value, ok := values[key]; ok {
			t.Errorf("unexpected inherited %s=%s", key, value)
		}
	}
}

func fakeDiscoveryGit(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Git executable fixture requires a POSIX shell")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

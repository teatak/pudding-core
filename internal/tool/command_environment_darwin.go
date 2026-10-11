//go:build darwin

package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxLoginShellEnvironmentBytes = 1 << 20

// Resolve defaults before approval and executable discovery so a grant and its
// eventual invocation see the same toolchain. Selection never executes a tool
// from a caller-provided DEVELOPER_DIR outside the command sandbox.
func platformCommandEnvironment(env []string) ([]string, error) {
	developer := executableEnvValue(env, "DEVELOPER_DIR")
	if developer == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/usr/bin/xcode-select", "--print-path")
		cmd.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
		output, err := cmd.CombinedOutput()
		if err != nil {
			// Developer tools are optional for ordinary commands. This specific
			// absence is not a reason to install tools or choose another Xcode.
			var exit *exec.ExitError
			if errors.As(err, &exit) && exit.ExitCode() == 2 && strings.Contains(string(output), "unable to get active developer directory") {
				return env, nil
			}
			return nil, fmt.Errorf("resolve Apple developer directory: %w: %s", err, strings.TrimSpace(string(output)))
		}
		developer = strings.TrimSpace(string(output))
	}
	if !filepath.IsAbs(developer) {
		// Relative explicit paths belong to the eventual command's cwd.
		return env, nil
	}
	if strings.HasSuffix(developer, ".app") {
		developer = filepath.Join(developer, "Contents", "Developer")
	}
	info, err := os.Stat(developer)
	if err != nil {
		return nil, fmt.Errorf("Apple developer directory is unavailable: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("Apple developer directory is unavailable: %s", developer)
	}
	out := append([]string(nil), env...)
	if executableEnvValue(out, "SDKROOT") == "" {
		// Xcode and the standalone Command Line Tools ship different layouts.
		sdkDir := filepath.Join(developer, "SDKs")
		platform := filepath.Join(developer, "Platforms", "MacOSX.platform", "Developer", "SDKs")
		if info, err := os.Stat(platform); err == nil && info.IsDir() {
			sdkDir = platform
		} else if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect Apple SDK directory: %w", err)
		}
		sdk := filepath.Join(sdkDir, "MacOSX.sdk")
		if _, err := os.Stat(sdk); err == nil {
			resolved, err := filepath.EvalSymlinks(sdk)
			if err != nil {
				return nil, fmt.Errorf("resolve Apple SDK: %w", err)
			}
			out = sandboxSetEnv(out, "SDKROOT", resolved)
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("inspect Apple SDK: %w", err)
		}
	}
	var toolDirs []string
	if executableEnvValue(out, "TOOLCHAINS") == "" {
		toolDirs = append(toolDirs, filepath.Join(developer, "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin"))
	}
	toolDirs = append(toolDirs, filepath.Join(developer, "usr", "bin"))
	available := toolDirs[:0]
	for _, dir := range toolDirs {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			available = append(available, dir)
		}
	}
	pathDirs := filepath.SplitList(executableEnvValue(out, "PATH"))
	for index, dir := range pathDirs {
		if filepath.Clean(dir) == "/usr/bin" {
			pathDirs = append(pathDirs[:index:index], append(available, pathDirs[index:]...)...)
			out = sandboxSetEnv(out, "PATH", strings.Join(pathDirs, string(os.PathListSeparator)))
			break
		}
	}
	return out, nil
}

func captureLoginShellEnvironment(ctx context.Context) ([]string, string, bool, error) {
	shell := preferredLoginShell()
	info, err := os.Stat(shell)
	if err != nil {
		return nil, shell, true, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, shell, true, errors.New("login shell is not executable")
	}

	cmd := exec.CommandContext(ctx, shell, loginShellEnvironmentArgs()...)
	cmd.Env = append([]string(nil), os.Environ()...)
	stdout := newTruncatingBuffer(maxLoginShellEnvironmentBytes)
	cmd.Stdout = stdout
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, shell, true, ctx.Err()
		}
		return nil, shell, true, err
	}
	if stdout.Truncated() {
		return nil, shell, true, errors.New("login shell environment exceeded the size limit")
	}
	entries, err := parseLoginShellEnvironment([]byte(stdout.String()))
	if err != nil {
		return nil, shell, true, err
	}
	return entries, shell, true, nil
}

func preferredLoginShell() string {
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if allowedLoginShell(shell) {
		return shell
	}
	return "/bin/zsh"
}

func allowedLoginShell(shell string) bool {
	if !filepath.IsAbs(shell) {
		return false
	}
	data, err := os.ReadFile("/etc/shells")
	if err != nil {
		return shell == "/bin/zsh" || shell == "/bin/bash"
	}
	for line := range strings.Lines(string(data)) {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && line == shell {
			return true
		}
	}
	return false
}

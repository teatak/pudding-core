//go:build darwin

package tool

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const maxLoginShellEnvironmentBytes = 1 << 20

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

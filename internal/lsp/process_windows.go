//go:build windows

package lsp

import (
	"context"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func configureProcess(cmd *exec.Cmd) {
	if strings.EqualFold(filepath.Base(cmd.Path), "cmd.exe") && len(cmd.Args) == 5 &&
		cmd.Args[1] == "/d" && cmd.Args[2] == "/s" && cmd.Args[3] == "/c" {
		// cmd.exe parses /c itself: Go's standard argv quoting uses backslash
		// escapes, which CMD treats literally. /s needs an outer quote pair.
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CmdLine: syscall.EscapeArg(cmd.Path) + ` /d /s /c "` + cmd.Args[4] + `"`,
		}
	}
}

func terminateProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}

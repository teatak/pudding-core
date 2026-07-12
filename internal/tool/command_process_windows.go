//go:build windows

package tool

import (
	"os/exec"
	"strconv"
	"syscall"
)

const createNewProcessGroup = 0x00000200

func configureCommandProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func terminateCommandProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func requestCommandProcessStop(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := exec.Command("taskkill", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run(); err == nil {
		return nil
	}
	return cmd.Process.Kill()
}

//go:build windows

package tool

import (
	"os/exec"
	"sync"
	"syscall"

	"golang.org/x/sys/windows"
)

const createNewProcessGroup = 0x00000200

// Each started command owns a job object that its descendants inherit.
// Terminating the job also stops children created while termination runs,
// which a taskkill snapshot of the process tree can miss.
var commandJobs sync.Map // *os.Process -> windows.Handle

func configureCommandProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func configureCommandPTY(_ *exec.Cmd) {}

// attachCommandProcess runs right after Start, before PowerShell can run the
// command and create descendants. Normal exit does not kill remaining
// descendants, matching the POSIX process-group behaviour.
func attachCommandProcess(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
		_ = windows.CloseHandle(process)
	}
	if err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	commandJobs.Store(cmd.Process, job)
	return nil
}

// releaseCommandProcess runs after Wait; no termination can follow it.
func releaseCommandProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if job, ok := commandJobs.LoadAndDelete(cmd.Process); ok {
		_ = windows.CloseHandle(job.(windows.Handle))
	}
}

func terminateCommandProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if job, ok := commandJobs.Load(cmd.Process); ok {
		return windows.TerminateJobObject(job.(windows.Handle), 1)
	}
	return cmd.Process.Kill()
}

func requestCommandProcessStop(cmd *exec.Cmd) error {
	// Console processes have no reliable SIGTERM equivalent; stop the whole job.
	return terminateCommandProcess(cmd)
}

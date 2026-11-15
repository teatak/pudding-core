//go:build windows

package tool

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const createNewProcessGroup = 0x00000200

// Each started command owns a job object that its descendants inherit.
// Terminating the job also stops children created while termination runs,
// which a taskkill snapshot of the process tree can miss.
var commandJobs sync.Map // *os.Process -> *commandJob

type commandJob struct {
	mu      sync.Mutex // guards the job handle against stop and cleanup
	handle  windows.Handle
	process windows.Handle // held until the root-exit watcher has returned
	done    chan struct{}
	err     error // published by closing done
}

func configureCommandProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNewProcessGroup}
}

func configureCommandPTY(_ *exec.Cmd) {}

func commandProcessSignal(*exec.Cmd) string { return "" }

// Own descendants until the root exits, including those keeping stdout/stderr
// open. Watching a retained process handle observes root exit independently of
// exec.Cmd.Wait, which also waits for its output-copy goroutines to reach EOF.
func attachCommandProcess(cmd *exec.Cmd) error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}
	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		_ = windows.CloseHandle(job)
		return err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(cmd.Process.Pid))
	if err == nil {
		err = windows.AssignProcessToJobObject(job, process)
	}
	if err != nil {
		if process != 0 {
			_ = windows.CloseHandle(process)
		}
		_ = windows.CloseHandle(job)
		return err
	}
	owned := &commandJob{handle: job, process: process, done: make(chan struct{})}
	commandJobs.Store(cmd.Process, owned)
	go owned.cleanupAfterRootExit()
	return nil
}

func (job *commandJob) cleanupAfterRootExit() {
	status, waitErr := windows.WaitForSingleObject(job.process, windows.INFINITE)
	if waitErr == nil && status != windows.WAIT_OBJECT_0 {
		waitErr = fmt.Errorf("wait for command root exit: unexpected status %d", status)
	}
	job.mu.Lock()
	cleanupErr := windows.TerminateJobObject(job.handle, 1)
	if cleanupErr != nil {
		// Kill-on-close still releases inherited output handles if explicit
		// termination fails. Preserve the error instead of claiming cleanup.
		cleanupErr = errors.Join(cleanupErr, windows.CloseHandle(job.handle))
		job.handle = 0
	}
	job.err = errors.Join(waitErr, cleanupErr)
	job.mu.Unlock()
	close(job.done)
}

// Wait has joined output copying; also join Job cleanup before releasing the
// process record/quota. No stop or watcher may use a handle after it is closed.
func releaseCommandProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if value, ok := commandJobs.LoadAndDelete(cmd.Process); ok {
		job := value.(*commandJob)
		<-job.done
		job.mu.Lock()
		defer job.mu.Unlock()
		err := job.err
		if job.handle != 0 {
			err = errors.Join(err, windows.CloseHandle(job.handle))
			job.handle = 0
		}
		return errors.Join(err, windows.CloseHandle(job.process))
	}
	return nil
}

func terminateCommandProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if value, ok := commandJobs.Load(cmd.Process); ok {
		job := value.(*commandJob)
		job.mu.Lock()
		defer job.mu.Unlock()
		if job.handle == 0 {
			return job.err
		}
		return windows.TerminateJobObject(job.handle, 1)
	}
	return cmd.Process.Kill()
}

func requestCommandProcessStop(cmd *exec.Cmd) error {
	// Console processes have no reliable SIGTERM equivalent; stop the whole job.
	return terminateCommandProcess(cmd)
}

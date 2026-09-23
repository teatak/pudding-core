package tool

import (
	"errors"
	"os/exec"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBackgroundProcessDarwinCleanupOfExitedLeader(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	configureCommandProcess(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	if err := waitForBackgroundProcessExit(cmd); err != nil {
		t.Fatalf("observe exit before reaping: %v", err)
	}
	if err := terminateBackgroundProcessGroup(cmd, true); err != nil {
		t.Fatalf("cleanup changed a successful exit into a failure: %v", err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("child exit status changed: %v", err)
	}
}

func TestBackgroundProcessDarwinCleanupPreservesRealErrors(t *testing.T) {
	const groupID = 123
	member := func(pid int32, state int8) unix.KinfoProc {
		var process unix.KinfoProc
		process.Proc.P_pid = pid
		process.Proc.P_stat = state
		process.Eproc.Pgid = groupID
		return process
	}
	wrongGroup := member(groupID, 5)
	wrongGroup.Eproc.Pgid++
	for _, test := range []struct {
		name     string
		observed bool
		err      error
		members  []unix.KinfoProc
		queryErr error
		clean    bool
	}{
		{name: "zombie leader", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 5)}, clean: true},
		{name: "zombie group", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 5), member(124, 5)}, clean: true},
		{name: "live descendant", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 5), member(124, 2)}},
		{name: "stopped descendant", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 5), member(124, 4)}},
		{name: "observed leader transitioning", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 2)}, clean: true},
		{name: "unobserved leader transitioning", err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 2)}},
		{name: "unobserved zombie leader", err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 5)}},
		{name: "transitioning leader with live descendant", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 2), member(124, 2)}},
		{name: "missing leader", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(124, 5)}},
		{name: "different group", observed: true, err: unix.EPERM, members: []unix.KinfoProc{wrongGroup}},
		{name: "empty query", observed: true, err: unix.EPERM},
		{name: "query failed", observed: true, err: unix.EPERM, members: []unix.KinfoProc{member(groupID, 5)}, queryErr: unix.EIO},
		{name: "different signal error", observed: true, err: unix.EINVAL, members: []unix.KinfoProc{member(groupID, 5)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := darwinBackgroundGroupCleanupError(groupID, test.observed, test.err, test.members, test.queryErr)
			if test.clean {
				if err != nil {
					t.Fatalf("verified finished group failed cleanup: %v", err)
				}
			} else if !errors.Is(err, test.err) {
				t.Fatalf("cleanup suppressed a real or unverified failure: %v", err)
			}
			if test.queryErr != nil && !errors.Is(err, test.queryErr) {
				t.Fatalf("inspection error was lost: %v", err)
			}
		})
	}
}

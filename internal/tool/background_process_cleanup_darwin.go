package tool

import (
	"errors"
	"os/exec"

	"golang.org/x/sys/unix"
)

func terminateBackgroundProcessGroup(cmd *exec.Cmd, observedExit bool) error {
	err := terminateCommandProcess(cmd)
	if !observedExit || !errors.Is(err, unix.EPERM) {
		return err
	}
	// Darwin's killpg1 excludes zombies and returns EPERM when it finds no
	// signalable members. The leader remains unreaped throughout this call.
	// Query both live and zombie members before accepting that specific case.
	groupID := cmd.Process.Pid
	members, queryErr := unix.SysctlKinfoProcSlice("kern.proc.pgrp", groupID)
	return darwinBackgroundGroupCleanupError(groupID, observedExit, err, members, queryErr)
}

func darwinBackgroundGroupCleanupError(groupID int, observedExit bool, signalErr error, members []unix.KinfoProc, queryErr error) error {
	if !observedExit || !errors.Is(signalErr, unix.EPERM) || queryErr != nil {
		return errors.Join(signalErr, queryErr)
	}
	const zombie = 5 // SZOMB in Darwin's sys/proc.h.
	foundLeader := false
	for _, member := range members {
		if int(member.Eproc.Pgid) != groupID {
			return signalErr
		}
		if int(member.Proc.P_pid) == groupID {
			// NOTE_EXIT (or registration ESRCH for our unreaped child) follows
			// Darwin's irreversible task hold, but precedes P_stat = SZOMB.
			// Only this observed leader may still show its previous state.
			foundLeader = true
		} else if member.Proc.P_stat != zombie {
			return signalErr
		}
	}
	if !foundLeader {
		// An empty/incomplete snapshot cannot prove that our observed leader
		// is the only possible member not yet reported as a zombie.
		return signalErr
	}
	return nil
}

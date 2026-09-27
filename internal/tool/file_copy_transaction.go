package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func (r *BuiltinRunner) applyFileCopy(call Call, args fileCopyArgs, plan *FileCopyPlan) ([]string, error) {
	if err := prepareFileCopyDestination(plan.To.Path, args.Overwrite); err != nil {
		return nil, err
	}
	// Stage on the destination filesystem so committing never needs a copy
	// across volumes. An unreadable source or full disk leaves the target intact.
	staging, err := os.MkdirTemp(filepath.Dir(plan.To.Path), ".pudding-copy-*")
	if err != nil {
		return nil, err
	}
	backup := filepath.Join(staging, "previous")
	defer func() {
		// A failed rollback/cleanup must retain the backup for recovery.
		if _, err := os.Lstat(backup); errors.Is(err, os.ErrNotExist) {
			_ = os.RemoveAll(staging)
		}
	}()
	staged := filepath.Join(staging, "new")
	if plan.sourceInfo.IsDir() {
		err = copyFileDir(plan.From.Path, staged)
	} else {
		err = copyFileBytes(plan.From.Path, staged, plan.sourceInfo)
	}
	if err != nil {
		return nil, err
	}
	// Recheck both resolution and the whole trees after staging, not only
	// after approval: a large copy must not overwrite edits made while it ran.
	current, err := r.prepareFileCopy(call, args, call.FileCopyGrant != nil)
	if err != nil {
		return nil, err
	}
	if !plan.matches(current) {
		return nil, &fileCopyError{reason: "copy_approval_changed", detail: "source or destination changed before committing the copy; submit the copy again"}
	}
	if err := installFileCopy(nil, staged, plan.To.Path, backup, plan.destinationInfo != nil); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(backup); err != nil {
		return []string{fmt.Sprintf("copy completed; backup cleanup failed at %s: %v", backup, err)}, nil
	}
	return nil, nil
}

func installFileCopy(root *os.Root, staged, target, backup string, replace bool) error {
	rename, lstat := os.Rename, os.Lstat
	if root != nil {
		rename, lstat = root.Rename, root.Lstat
	}
	if replace {
		if err := rename(target, backup); err != nil {
			return err
		}
	} else if _, err := lstat(target); err == nil {
		return errFileCopyDestinationExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := rename(staged, target); err != nil {
		if replace {
			if rollbackErr := rename(backup, target); rollbackErr != nil {
				if root != nil {
					backup = filepath.Join(root.Name(), backup)
				}
				return &fileCopyError{reason: "copy_rollback_failed", detail: fmt.Sprintf("copy commit failed: %v; restoring destination failed: %v; backup retained at %s", err, rollbackErr, backup)}
			}
		}
		return err
	}
	return nil
}

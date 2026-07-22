package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type patchApplyItem struct {
	file       preparedPatchFile
	tempPath   string
	backupPath string
	installed  bool
}

func applyPreparedPatch(projectDirs []string, patch *preparedPatch) ([]string, error) {
	if err := validatePreparedPatchState(projectDirs, patch); err != nil {
		return nil, err
	}
	items := make([]patchApplyItem, len(patch.Files))
	createdDirs := make(map[string]bool)
	for i, file := range patch.Files {
		items[i].file = file
		if file.Delete {
			continue
		}
		if err := ensurePatchParent(filepath.Dir(file.Target), patch.ProjectRoot, createdDirs); err != nil {
			cleanupPatchStaging(items, createdDirs)
			return nil, newPatchError("stage_failed", err.Error())
		}
		temp, err := os.CreateTemp(filepath.Dir(file.Target), ".pudding-patch-stage-*")
		if err != nil {
			cleanupPatchStaging(items, createdDirs)
			return nil, newPatchError("stage_failed", err.Error())
		}
		items[i].tempPath = temp.Name()
		mode := file.Mode.Perm()
		if mode == 0 {
			mode = 0o600
		}
		writeErr := error(nil)
		if err := temp.Chmod(mode); err != nil {
			writeErr = err
		} else if _, err := temp.WriteString(file.NewText); err != nil {
			writeErr = err
		} else if err := temp.Sync(); err != nil {
			writeErr = err
		}
		if err := temp.Close(); writeErr == nil {
			writeErr = err
		}
		if writeErr != nil {
			cleanupPatchStaging(items, createdDirs)
			return nil, newPatchError("stage_failed", writeErr.Error())
		}
	}

	for i := range items {
		item := &items[i]
		if err := validatePatchFileState(projectDirs, patch.ProjectRoot, item.file); err != nil {
			rollbackErr := rollbackPatchItems(items, createdDirs)
			return nil, patchApplyError(err, rollbackErr)
		}
		if item.file.Existed {
			backupPath, err := reservePatchBackupPath(filepath.Dir(item.file.Target))
			if err != nil {
				rollbackErr := rollbackPatchItems(items, createdDirs)
				return nil, patchApplyError(err, rollbackErr)
			}
			if err := os.Rename(item.file.Target, backupPath); err != nil {
				rollbackErr := rollbackPatchItems(items, createdDirs)
				return nil, patchApplyError(err, rollbackErr)
			}
			item.backupPath = backupPath
		}
		if item.file.Delete {
			continue
		}
		if !item.file.Existed {
			if _, err := os.Lstat(item.file.Target); err == nil || !errors.Is(err, os.ErrNotExist) {
				rollbackErr := rollbackPatchItems(items, createdDirs)
				return nil, patchApplyError(newPatchError("patch_stale", "new file path appeared after patch validation: "+item.file.Path), rollbackErr)
			}
		}
		if err := os.Rename(item.tempPath, item.file.Target); err != nil {
			rollbackErr := rollbackPatchItems(items, createdDirs)
			return nil, patchApplyError(err, rollbackErr)
		}
		item.tempPath = ""
		item.installed = true
	}

	warnings := make([]string, 0)
	for i := range items {
		if items[i].backupPath == "" {
			continue
		}
		if err := os.Remove(items[i].backupPath); err != nil {
			warnings = append(warnings, "remove patch backup: "+err.Error())
		}
		items[i].backupPath = ""
	}
	cleanupPatchTemps(items)
	return warnings, nil
}

func validatePreparedPatchState(projectDirs []string, patch *preparedPatch) error {
	if patch == nil || len(patch.Files) == 0 {
		return newPatchError("patch_empty", "prepared patch is empty or unavailable")
	}
	for _, file := range patch.Files {
		if err := validatePatchFileState(projectDirs, patch.ProjectRoot, file); err != nil {
			return err
		}
	}
	return nil
}

func validatePatchFileState(projectDirs []string, projectRoot string, file preparedPatchFile) error {
	authorizedRoot, err := patchAuthorizedRoot(projectDirs, projectRoot)
	if err != nil {
		return err
	}
	root, target, _, err := resolveProjectPath([]string{authorizedRoot}, file.Path, false, true)
	if err != nil {
		return &patchError{reason: patchPathReason(err), detail: file.Path + ": " + err.Error()}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return newPatchError("project_root_unavailable", err.Error())
	}
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		target = filepath.Join(resolvedRoot, filepath.FromSlash(file.Path))
	}
	if filepath.Clean(resolvedRoot) != filepath.Clean(projectRoot) || filepath.Clean(target) != filepath.Clean(file.Target) {
		return newPatchError("path_not_authorized", "patch file is no longer inside the authorized project root: "+file.Path)
	}
	info, statErr := os.Lstat(file.Target)
	if !file.Existed {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil
		}
		if statErr != nil {
			return newPatchError("stat_failed", statErr.Error())
		}
		return newPatchError("patch_stale", "file was created after patch validation: "+file.Path)
	}
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return newPatchError("patch_stale", "file was removed after patch validation: "+file.Path)
		}
		return newPatchError("stat_failed", statErr.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return newPatchError("patch_stale", "file type changed after patch validation: "+file.Path)
	}
	if info.Size() > patchMaxFileBytes {
		return newPatchError("patch_stale", "file grew beyond the patch size limit: "+file.Path)
	}
	data, err := os.ReadFile(file.Target)
	if err != nil {
		return newPatchError("read_failed", err.Error())
	}
	if patchContentHash(data) != file.OldHash {
		return newPatchError("patch_stale", "file changed after patch validation: "+file.Path)
	}
	return nil
}

func patchAuthorizedRoot(projectDirs []string, projectRoot string) (string, error) {
	for _, root := range normalizeProjectDirs(projectDirs) {
		resolved, err := filepath.EvalSymlinks(root)
		if err == nil && filepath.Clean(resolved) == filepath.Clean(projectRoot) {
			return root, nil
		}
	}
	return "", newPatchError("path_not_authorized", "patch project root is no longer authorized")
}

func ensurePatchParent(parent, root string, created map[string]bool) error {
	parent = filepath.Clean(parent)
	root = filepath.Clean(root)
	if !pathInsideRoot(parent, root) {
		return errors.New("patch parent is outside the project root")
	}
	missing := make([]string, 0)
	for current := parent; current != root; current = filepath.Dir(current) {
		if _, err := os.Stat(current); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		created[missing[i]] = true
	}
	return nil
}

func reservePatchBackupPath(dir string) (string, error) {
	file, err := os.CreateTemp(dir, ".pudding-patch-backup-*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}

func rollbackPatchItems(items []patchApplyItem, createdDirs map[string]bool) error {
	var rollbackErr error
	for i := len(items) - 1; i >= 0; i-- {
		item := &items[i]
		if item.installed {
			if err := os.Remove(item.file.Target); err != nil && !errors.Is(err, os.ErrNotExist) {
				rollbackErr = errors.Join(rollbackErr, err)
			}
			item.installed = false
		}
		if item.backupPath != "" {
			if err := os.Rename(item.backupPath, item.file.Target); err != nil {
				rollbackErr = errors.Join(rollbackErr, err)
			} else {
				item.backupPath = ""
			}
		}
	}
	cleanupPatchTemps(items)
	removeCreatedPatchDirs(createdDirs)
	return rollbackErr
}

func cleanupPatchStaging(items []patchApplyItem, createdDirs map[string]bool) {
	cleanupPatchTemps(items)
	removeCreatedPatchDirs(createdDirs)
}

func cleanupPatchTemps(items []patchApplyItem) {
	for i := range items {
		if items[i].tempPath != "" {
			_ = os.Remove(items[i].tempPath)
			items[i].tempPath = ""
		}
	}
}

func removeCreatedPatchDirs(createdDirs map[string]bool) {
	dirs := make([]string, 0, len(createdDirs))
	for dir := range createdDirs {
		dirs = append(dirs, dir)
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(filepath.Separator)) > strings.Count(dirs[j], string(filepath.Separator))
	})
	for _, dir := range dirs {
		_ = os.Remove(dir)
	}
}

func patchApplyError(applyErr, rollbackErr error) error {
	if rollbackErr != nil {
		return newPatchError("rollback_failed", fmt.Sprintf("apply failed: %v; rollback failed: %v", applyErr, rollbackErr))
	}
	var patchErr *patchError
	if errors.As(applyErr, &patchErr) {
		return patchErr
	}
	return newPatchError("apply_failed", applyErr.Error())
}

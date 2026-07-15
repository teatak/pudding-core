// Package projectfs provides project-root-scoped filesystem mutations.
package projectfs

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/projectpath"
)

var (
	ErrConflict       = errors.New("project entry already exists")
	ErrInvalidName    = errors.New("invalid project entry name")
	ErrNotDirectory   = errors.New("project path is not a directory")
	ErrNotFile        = errors.New("project path is not a regular file")
	ErrNotFound       = errors.New("project path not found")
	ErrPathNotAllowed = errors.New("project path not allowed")
	ErrSymlink        = errors.New("project symlink mutation is not allowed")
)

type Entry struct {
	Name string
	Path string
	Type string
}

func Create(root, parentPath, name, entryType string) (Entry, error) {
	name, err := validateName(name)
	if err != nil {
		return Entry{}, err
	}
	if entryType != "file" && entryType != "dir" {
		return Entry{}, fmt.Errorf("%w: unsupported entry type", ErrInvalidName)
	}
	parent, parentRel, info, err := existingPath(root, parentPath, true)
	if err != nil {
		return Entry{}, err
	}
	if !info.IsDir() {
		return Entry{}, ErrNotDirectory
	}
	target := filepath.Join(parent, name)
	if err := authorizeParent(root, target); err != nil {
		return Entry{}, err
	}
	if _, err := os.Lstat(target); err == nil {
		return Entry{}, ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return Entry{}, err
	}
	if entryType == "dir" {
		if err := os.Mkdir(target, 0o755); err != nil {
			if errors.Is(err, os.ErrExist) {
				return Entry{}, ErrConflict
			}
			return Entry{}, err
		}
	} else {
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return Entry{}, ErrConflict
			}
			return Entry{}, err
		}
		if err := file.Close(); err != nil {
			return Entry{}, err
		}
	}
	return Entry{Name: name, Path: joinRelative(parentRel, name), Type: entryType}, nil
}

func Rename(root, path, newName string) (Entry, error) {
	newName, err := validateName(newName)
	if err != nil {
		return Entry{}, err
	}
	source, sourceRel, info, err := existingPath(root, path, false)
	if err != nil {
		return Entry{}, err
	}
	entryType := "file"
	if info.IsDir() {
		entryType = "dir"
	} else if !info.Mode().IsRegular() {
		return Entry{}, ErrNotFile
	}
	target := filepath.Join(filepath.Dir(source), newName)
	if err := authorizeParent(root, target); err != nil {
		return Entry{}, err
	}
	if filepath.Clean(source) != filepath.Clean(target) {
		if err := renameNoReplace(source, target); err != nil {
			return Entry{}, err
		}
	}
	return Entry{
		Name: newName,
		Path: joinRelative(filepath.ToSlash(filepath.Dir(sourceRel)), newName),
		Type: entryType,
	}, nil
}

// Copy copies a regular file or directory between authorized project roots.
// Symlinks and special files are intentionally rejected.
func Copy(sourceRoot, sourcePath, targetRoot, targetParentPath, requestedName string, unique bool) (Entry, error) {
	source, _, info, err := existingPath(sourceRoot, sourcePath, false)
	if err != nil {
		return Entry{}, err
	}
	entryType, err := supportedEntryType(info)
	if err != nil {
		return Entry{}, err
	}
	parent, parentRel, parentInfo, err := existingPath(targetRoot, targetParentPath, true)
	if err != nil {
		return Entry{}, err
	}
	if !parentInfo.IsDir() {
		return Entry{}, ErrNotDirectory
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = filepath.Base(source)
	}
	name, err = validateName(name)
	if err != nil {
		return Entry{}, err
	}
	if unique {
		name, err = availableCopyName(parent, name, info.IsDir())
		if err != nil {
			return Entry{}, err
		}
	}
	target := filepath.Join(parent, name)
	if err := authorizeParent(targetRoot, target); err != nil {
		return Entry{}, err
	}
	if samePath(source, target) {
		return Entry{}, ErrConflict
	}
	if info.IsDir() && pathContains(source, target) {
		return Entry{}, ErrPathNotAllowed
	}
	if err := copyEntryNoReplace(source, target, info); err != nil {
		return Entry{}, err
	}
	return Entry{Name: name, Path: joinRelative(parentRel, name), Type: entryType}, nil
}

// Move moves a regular file or directory between authorized project roots.
// Moves within one root use an atomic no-replace rename. Cross-root moves copy
// first and remove the source only after the destination is complete.
func Move(sourceRoot, sourcePath, targetRoot, targetParentPath, requestedName string) (Entry, error) {
	source, sourceRel, info, err := existingPath(sourceRoot, sourcePath, false)
	if err != nil {
		return Entry{}, err
	}
	entryType, err := supportedEntryType(info)
	if err != nil {
		return Entry{}, err
	}
	parent, parentRel, parentInfo, err := existingPath(targetRoot, targetParentPath, true)
	if err != nil {
		return Entry{}, err
	}
	if !parentInfo.IsDir() {
		return Entry{}, ErrNotDirectory
	}
	name := strings.TrimSpace(requestedName)
	if name == "" {
		name = filepath.Base(source)
	}
	name, err = validateName(name)
	if err != nil {
		return Entry{}, err
	}
	target := filepath.Join(parent, name)
	if err := authorizeParent(targetRoot, target); err != nil {
		return Entry{}, err
	}
	if samePath(source, target) {
		return Entry{Name: name, Path: sourceRel, Type: entryType}, nil
	}
	if info.IsDir() && pathContains(source, target) {
		return Entry{}, ErrPathNotAllowed
	}
	if filepath.Clean(sourceRoot) == filepath.Clean(targetRoot) {
		if err := renameNoReplace(source, target); err != nil {
			return Entry{}, err
		}
	} else {
		if err := copyEntryNoReplace(source, target, info); err != nil {
			return Entry{}, err
		}
		if err := os.RemoveAll(source); err != nil {
			_ = os.RemoveAll(target)
			return Entry{}, err
		}
	}
	return Entry{Name: name, Path: joinRelative(parentRel, name), Type: entryType}, nil
}

func Remove(root, path string) error {
	target, _, _, err := existingPath(root, path, false)
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
}

func supportedEntryType(info os.FileInfo) (string, error) {
	if info.IsDir() {
		return "dir", nil
	}
	if info.Mode().IsRegular() {
		return "file", nil
	}
	return "", ErrNotFile
}

func availableCopyName(parent, original string, directory bool) (string, error) {
	ext := ""
	base := original
	if !directory {
		ext = filepath.Ext(original)
		base = strings.TrimSuffix(original, ext)
	}
	for index := 1; ; index++ {
		suffix := " copy"
		if index > 1 {
			suffix = fmt.Sprintf(" copy %d", index)
		}
		candidate := base + suffix + ext
		if _, err := os.Lstat(filepath.Join(parent, candidate)); errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
}

func copyEntryNoReplace(source, target string, info os.FileInfo) error {
	if _, err := os.Lstat(target); err == nil {
		return ErrConflict
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	parent := filepath.Dir(target)
	if info.IsDir() {
		temporary, err := os.MkdirTemp(parent, ".pudding-copy-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temporary)
		if err := copyDirectoryContents(source, temporary); err != nil {
			return err
		}
		if err := os.Chmod(temporary, info.Mode().Perm()); err != nil {
			return err
		}
		return renameNoReplace(temporary, target)
	}
	if !info.Mode().IsRegular() {
		return ErrNotFile
	}
	temporary, err := os.CreateTemp(parent, ".pudding-copy-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := copyRegularFile(source, temporary, info.Mode().Perm()); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return renameNoReplace(temporaryPath, target)
}

func copyDirectoryContents(source, target string) error {
	type directoryMode struct {
		mode os.FileMode
		path string
	}
	directories := make([]directoryMode, 0)
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == source {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return ErrSymlink
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if err := os.Mkdir(destination, info.Mode().Perm()|0o700); err != nil {
				return err
			}
			directories = append(directories, directoryMode{mode: info.Mode().Perm(), path: destination})
			return nil
		}
		if !entry.Type().IsRegular() {
			return ErrNotFile
		}
		file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		if err := copyRegularFile(path, file, info.Mode().Perm()); err != nil {
			file.Close()
			return err
		}
		return file.Close()
	})
	if err != nil {
		return err
	}
	for index := len(directories) - 1; index >= 0; index-- {
		if err := os.Chmod(directories[index].path, directories[index].mode); err != nil {
			return err
		}
	}
	return nil
}

func copyRegularFile(source string, destination *os.File, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if _, err := io.Copy(destination, input); err != nil {
		return err
	}
	return destination.Chmod(mode)
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func pathContains(parent, child string) bool {
	parentAbs, parentErr := filepath.Abs(parent)
	childAbs, childErr := filepath.Abs(child)
	if parentErr != nil || childErr != nil {
		return false
	}
	rel, err := filepath.Rel(filepath.Clean(parentAbs), filepath.Clean(childAbs))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func existingPath(root, rawPath string, allowRoot bool) (string, string, os.FileInfo, error) {
	rel, err := cleanRelative(rawPath, allowRoot)
	if err != nil {
		return "", "", nil, err
	}
	target := filepath.Join(filepath.Clean(root), filepath.FromSlash(rel))
	if filepath.Clean(target) != filepath.Clean(root) {
		if err := authorizeParent(root, target); err != nil {
			return "", "", nil, err
		}
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", nil, ErrNotFound
	}
	if err != nil {
		return "", "", nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if filepath.Clean(target) != filepath.Clean(root) {
			return "", "", nil, ErrSymlink
		}
		info, err = os.Stat(target)
		if err != nil {
			return "", "", nil, err
		}
	}
	return target, rel, info, nil
}

func authorizeParent(root, target string) error {
	resolvedRoot, err := filepath.EvalSymlinks(filepath.Clean(root))
	if err != nil {
		return ErrPathNotAllowed
	}
	resolvedParent, err := filepath.EvalSymlinks(filepath.Dir(filepath.Clean(target)))
	if err != nil || !projectpath.Inside(resolvedParent, resolvedRoot) {
		return ErrPathNotAllowed
	}
	return nil
}

func cleanRelative(raw string, allowRoot bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "."
	}
	local := filepath.FromSlash(raw)
	if filepath.IsAbs(local) || filepath.VolumeName(local) != "" {
		return "", ErrPathNotAllowed
	}
	cleaned := filepath.Clean(local)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrPathNotAllowed
	}
	if cleaned == "." && !allowRoot {
		return "", ErrPathNotAllowed
	}
	return filepath.ToSlash(cleaned), nil
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return "", ErrInvalidName
	}
	return name, nil
}

func joinRelative(parent, name string) string {
	if parent == "." || parent == "" {
		return filepath.ToSlash(name)
	}
	return filepath.ToSlash(filepath.Join(filepath.FromSlash(parent), name))
}

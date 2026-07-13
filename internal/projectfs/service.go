// Package projectfs provides project-root-scoped filesystem mutations.
package projectfs

import (
	"errors"
	"fmt"
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

func Remove(root, path string) error {
	target, _, _, err := existingPath(root, path, false)
	if err != nil {
		return err
	}
	return os.RemoveAll(target)
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

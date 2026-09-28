package canvas

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/teatak/pudding-core/contracts"
)

// WritePackage publishes a complete immutable directory before SQLite references it.
func WritePackage(home, id string, p Package) (string, error) {
	_, hash, err := p.Validate()
	if err != nil {
		return "", err
	}
	if home == "" || !identifier.MatchString(id) {
		return "", errors.New("invalid canvas home or ID")
	}
	parent := filepath.Join(home, "canvases", id, "revisions")
	if err = os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	staging, err := os.MkdirTemp(parent, ".pending-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(staging)
	for name, content := range p.Files {
		file := filepath.Join(staging, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return "", err
		}
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return "", err
		}
		_, writeErr := f.WriteString(content)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
	}
	if err = os.Rename(staging, filepath.Join(parent, hash)); err != nil {
		// Concurrent identical packages may already have published this hash.
		if _, readErr := ReadPackage(home, id, hash); readErr != nil {
			return "", err
		}
	}
	directory, err := os.Open(parent)
	if err != nil {
		return "", err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return "", syncErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	return hash, nil
}

func ReadPackage(home, id, hash string) (Package, error) {
	p := Package{Files: map[string]string{}}
	if home == "" || !identifier.MatchString(id) || !revisionID.MatchString(hash) {
		return p, errors.New("invalid canvas package reference")
	}
	root := filepath.Join(home, "canvases", id, "revisions", hash)
	total := int64(0)
	err := filepath.WalkDir(root, func(file string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed in packages")
		}
		if skipFinderMetadata(entry) {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if !ValidFilePath(name) {
			return errors.New("invalid stored source path")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		policy := contracts.Canvas()
		if !info.Mode().IsRegular() || info.Size() > int64(policy.MaxFileBytes) || total > int64(policy.MaxPackageBytes) || len(p.Files) >= policy.MaxFiles {
			return errors.New("stored package exceeds limits")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		p.Files[name] = string(data)
		return nil
	})
	if err != nil {
		return p, err
	}
	_, actual, err := p.Validate()
	if err != nil {
		return p, err
	}
	if actual != hash {
		return p, fmt.Errorf("source integrity mismatch: %s", hash)
	}
	return p, nil
}

// Finder may add .DS_Store after a package is published. It is not source.
func skipFinderMetadata(entry fs.DirEntry) bool {
	return entry.Name() == ".DS_Store" && entry.Type().IsRegular()
}

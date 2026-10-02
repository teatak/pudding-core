package widget

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/teatak/pudding-core/contracts"
)

// RevisionsDir holds the immutable published versions of one widget.
func RevisionsDir(home, id string) (string, error) {
	if home == "" || !identifier.MatchString(id) {
		return "", errors.New("invalid widget home or ID")
	}
	return filepath.Join(home, "studio", id, "revisions"), nil
}

// PackageHash is the content address of a package's files.
func PackageHash(files map[string]string) string {
	data, _ := json.Marshal(Package{Files: files})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

// WritePackage publishes a complete immutable directory before SQLite references it.
func WritePackage(home, id string, p Package) (string, error) {
	_, hash, err := p.Validate()
	if err != nil {
		return "", err
	}
	parent, err := RevisionsDir(home, id)
	if err != nil {
		return "", err
	}
	return hash, WriteVersion(parent, hash, p.Files)
}

// WriteVersion atomically installs files as parent/hash. A version already
// published under the same content address is reused.
func WriteVersion(parent, hash string, files map[string]string) error {
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".pending-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	for name, content := range files {
		file := filepath.Join(staging, filepath.FromSlash(name))
		if err = os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, writeErr := f.WriteString(content)
		if writeErr == nil {
			writeErr = f.Sync()
		}
		closeErr := f.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err = os.Rename(staging, filepath.Join(parent, hash)); err != nil {
		// Concurrent identical packages may already have published this hash.
		existing, readErr := ReadVersion(filepath.Join(parent, hash), func(string) bool { return true })
		if readErr != nil || PackageHash(existing) != hash {
			return err
		}
	}
	directory, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func ReadPackage(home, id, hash string) (Package, error) {
	p := Package{Files: map[string]string{}}
	parent, err := RevisionsDir(home, id)
	if err != nil || !revisionID.MatchString(hash) {
		return p, errors.New("invalid widget package reference")
	}
	files, err := ReadVersion(filepath.Join(parent, hash), ValidFilePath)
	if err != nil {
		return p, err
	}
	p.Files = files
	_, actual, err := p.Validate()
	if err != nil {
		return p, err
	}
	if actual != hash {
		return p, fmt.Errorf("source integrity mismatch: %s", hash)
	}
	return p, nil
}

// ReadVersion reads one published version directory within the package limits.
func ReadVersion(root string, validPath func(string) bool) (map[string]string, error) {
	files := map[string]string{}
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
		if !validPath(name) {
			return errors.New("invalid stored source path")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		policy := contracts.Widget()
		if !info.Mode().IsRegular() || info.Size() > int64(policy.MaxFileBytes) || total > int64(policy.MaxPackageBytes) || len(files) >= policy.MaxFiles {
			return errors.New("stored package exceeds limits")
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		files[name] = string(data)
		return nil
	})
	return files, err
}

// Finder may add .DS_Store after a package is published. It is not source.
func skipFinderMetadata(entry fs.DirEntry) bool {
	return entry.Name() == ".DS_Store" && entry.Type().IsRegular()
}

package workbench

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

var ErrDraftConflict = errors.New("canvas draft changed")

// Draft is the mutable working copy. Only a committed revision is usable by the runtime.
type Draft struct {
	BaseRevisionHash string            `json:"baseRevisionHash"`
	DraftHash        string            `json:"draftHash"`
	Files            map[string]string `json:"-"`
}

func draftRoot(home, id string) (string, error) {
	if home == "" || !identifier.MatchString(id) {
		return "", errors.New("invalid canvas draft reference")
	}
	return filepath.Join(home, "workbenches", id, "draft"), nil
}

func ReadDraft(home, id string) (Draft, error) {
	d := Draft{Files: map[string]string{}}
	root, err := draftRoot(home, id)
	if err != nil {
		return d, err
	}
	base, err := os.ReadFile(filepath.Join(root, ".base"))
	if err != nil {
		return d, err
	}
	d.BaseRevisionHash = string(base)
	if d.BaseRevisionHash != "" && !revisionID.MatchString(d.BaseRevisionHash) {
		return d, errors.New("invalid draft base revision")
	}
	total := 0
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlinks are not allowed in drafts")
		}
		if entry.IsDir() || file == filepath.Join(root, ".base") {
			return nil
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if !ValidFilePath(name) {
			return fmt.Errorf("invalid draft path %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		policy := contracts.Workbench()
		total += int(info.Size())
		if !info.Mode().IsRegular() || info.Size() > int64(policy.MaxFileBytes) || total > policy.MaxPackageBytes || len(d.Files) >= policy.MaxFiles {
			return errors.New("draft exceeds file limits")
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if !utf8.Valid(content) {
			return fmt.Errorf("invalid UTF-8 in %q", name)
		}
		d.Files[name] = string(content)
		return nil
	})
	if err != nil {
		return d, err
	}
	d.DraftHash = draftHash(d.Files)
	return d, nil
}

func draftHash(files map[string]string) string {
	data, _ := json.Marshal(Package{Files: files})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func StartDraft(home, id, baseHash string) (Draft, error) {
	root, err := draftRoot(home, id)
	if err != nil {
		return Draft{}, err
	}
	if existing, err := ReadDraft(home, id); err == nil {
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Draft{}, err
	}
	files := map[string]string{}
	if baseHash != "" {
		base, err := ReadPackage(home, id, baseHash)
		if err != nil {
			return Draft{}, err
		}
		files = base.Files
	}
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return Draft{}, err
	}
	staging, err := os.MkdirTemp(parent, ".draft-")
	if err != nil {
		return Draft{}, err
	}
	defer os.RemoveAll(staging)
	if err := os.WriteFile(filepath.Join(staging, ".base"), []byte(baseHash), 0600); err != nil {
		return Draft{}, err
	}
	for name, content := range files {
		file := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return Draft{}, err
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			return Draft{}, err
		}
	}
	if err := os.Rename(staging, root); err != nil {
		return Draft{}, err
	}
	return ReadDraft(home, id)
}

func WriteDraftFile(home, id, name string, content *string, expectedHash string) (Draft, error) {
	d, err := ReadDraft(home, id)
	if err != nil {
		return d, err
	}
	if d.DraftHash != expectedHash {
		return d, ErrDraftConflict
	}
	if !ValidFilePath(name) || (content != nil && strings.HasPrefix(name, "fixtures/")) {
		return d, errors.New("invalid changed source path")
	}
	if content == nil {
		delete(d.Files, name)
	} else {
		d.Files[name] = *content
	}
	policy := contracts.Workbench()
	if len(d.Files) > policy.MaxFiles {
		return d, errors.New("draft exceeds file count")
	}
	total := 0
	for path, value := range d.Files {
		total += len(value)
		if !ValidFilePath(path) || !utf8.ValidString(value) || len(value) > policy.MaxFileBytes || total > policy.MaxPackageBytes {
			return d, errors.New("draft exceeds source limits")
		}
	}
	root, _ := draftRoot(home, id)
	file := filepath.Join(root, filepath.FromSlash(name))
	if content == nil {
		if err := os.Remove(file); err != nil && !errors.Is(err, os.ErrNotExist) {
			return d, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return d, err
		}
		if err := writeDraftAtomic(root, file, []byte(*content)); err != nil {
			return d, err
		}
	}
	return ReadDraft(home, id)
}

func SetDraftBase(home, id, baseHash string) (Draft, error) {
	if !revisionID.MatchString(baseHash) {
		return Draft{}, errors.New("invalid draft base revision")
	}
	root, err := draftRoot(home, id)
	if err != nil {
		return Draft{}, err
	}
	if err := writeDraftAtomic(root, filepath.Join(root, ".base"), []byte(baseHash)); err != nil {
		return Draft{}, err
	}
	return ReadDraft(home, id)
}

func writeDraftAtomic(root, file string, data []byte) error {
	parent := filepath.Dir(file)
	f, err := os.CreateTemp(filepath.Dir(root), ".draft-write-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), file); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

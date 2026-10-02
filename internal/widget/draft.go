package widget

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

var ErrDraftConflict = errors.New("widget draft changed")

// DraftMu serializes draft edits and commits within the daemon. A tool edit
// must not race the HTTP draft/commit path after checking expectedDraftHash.
var DraftMu sync.Mutex

// Draft is the mutable working copy. Only a committed revision is usable by the runtime.
type Draft struct {
	BaseRevisionHash string            `json:"baseRevisionHash"`
	DraftHash        string            `json:"draftHash"`
	Files            map[string]string `json:"-"`
}

func DraftRoot(home, id string) (string, error) {
	if home == "" || !identifier.MatchString(id) {
		return "", errors.New("invalid widget draft reference")
	}
	return filepath.Join(home, "studio", id, "draft"), nil
}

func ReadDraft(home, id string) (Draft, error) {
	root, err := DraftRoot(home, id)
	if err != nil {
		return Draft{Files: map[string]string{}}, err
	}
	return ReadDraftDir(root, ValidFilePath)
}

// ReadDraftDir reads one working copy, whose files may be incomplete.
func ReadDraftDir(root string, validPath func(string) bool) (Draft, error) {
	d := Draft{Files: map[string]string{}}
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
		if skipFinderMetadata(entry) {
			return nil
		}
		if entry.IsDir() || file == filepath.Join(root, ".base") {
			return nil
		}
		name, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		name = filepath.ToSlash(name)
		if !validPath(name) {
			return fmt.Errorf("invalid draft path %q", name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		policy := contracts.Widget()
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
	return PackageHash(files)
}

func StartDraft(home, id, baseHash string) (Draft, error) {
	root, err := DraftRoot(home, id)
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
	if err := InstallDraft(root, baseHash, files); err != nil {
		return Draft{}, err
	}
	return ReadDraft(home, id)
}

// InstallDraft atomically creates a working copy at root; root must not exist.
func InstallDraft(root, baseHash string, files map[string]string) error {
	parent := filepath.Dir(root)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".draft-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := os.WriteFile(filepath.Join(staging, ".base"), []byte(baseHash), 0600); err != nil {
		return err
	}
	for name, content := range files {
		file := filepath.Join(staging, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(file, []byte(content), 0600); err != nil {
			return err
		}
	}
	return os.Rename(staging, root)
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
	policy := contracts.Widget()
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
	root, _ := DraftRoot(home, id)
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
	root, err := DraftRoot(home, id)
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

package attachment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SessionIDs lists only the managed, durable attachment area. Draft files have
// their own lifetime and are never swept by session deletion.
func (s *Service) SessionIDs() ([]string, error) {
	root, err := s.openSessionsRoot()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Name() == DraftSessionID {
			continue
		}
		ids = append(ids, entry.Name())
	}
	return ids, nil
}

// DeleteSession removes blobs and their .model derivatives together. Root
// relative removal never follows a session symlink into another workspace.
func (s *Service) DeleteSession(sessionID string) error {
	if sessionID == "" || sessionID == "." || sessionID == ".." || sessionID == DraftSessionID || filepath.Base(sessionID) != sessionID || strings.ContainsAny(sessionID, `/\\`) {
		return fmt.Errorf("attachment: invalid session id %q", sessionID)
	}
	root, err := s.openSessionsRoot()
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	return root.RemoveAll(sessionID)
}

func (s *Service) openSessionsRoot() (*os.Root, error) {
	if s == nil || s.home == "" {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(s.home)
	if err != nil {
		return nil, err
	}
	for _, name := range []string{attachmentsDirName, sessionDirName} {
		info, err := root.Lstat(name)
		if err != nil {
			root.Close()
			return nil, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			root.Close()
			return nil, fmt.Errorf("attachment: %s must be a directory", name)
		}
		next, err := root.OpenRoot(name)
		root.Close()
		if err != nil {
			return nil, err
		}
		opened, err := next.Stat(".")
		if err != nil || !os.SameFile(info, opened) {
			next.Close()
			return nil, errors.New("attachment: directory changed while opening")
		}
		root = next
	}
	return root, nil
}

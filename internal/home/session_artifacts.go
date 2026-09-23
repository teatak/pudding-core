package home

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// OpenSessionArtifacts opens the current session's persistent analysis area.
// It remains under the global temp scope and is not a project workspace.
func OpenSessionArtifacts(dir, sessionID string) (*os.Root, string, error) {
	return sessionArtifactsRoot(dir, sessionID, true)
}

// ExistingSessionArtifacts validates an existing area without creating one.
func ExistingSessionArtifacts(dir, sessionID string) (string, bool, error) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(sessionID) == "" {
		return "", false, nil
	}
	root, path, err := sessionArtifactsRoot(dir, sessionID, false)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	defer root.Close()
	return path, true, nil
}

func RemoveSessionArtifacts(dir, sessionID string) error {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(sessionID) == "" {
		return nil
	}
	base, _, err := sessionArtifactsBase(dir, false)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer base.Close()
	return base.RemoveAll(safePathComponent(strings.TrimSpace(sessionID)))
}

func sessionArtifactsRoot(dir, sessionID string, create bool) (*os.Root, string, error) {
	if strings.TrimSpace(dir) == "" || strings.TrimSpace(sessionID) == "" {
		return nil, "", errors.New("home: session artifacts require home and session id")
	}
	base, path, err := sessionArtifactsBase(dir, create)
	if err != nil {
		return nil, "", err
	}
	defer base.Close()
	component := safePathComponent(strings.TrimSpace(sessionID))
	root, err := openArtifactDirectory(base, component, create)
	return root, filepath.Join(path, component), err
}

func sessionArtifactsBase(dir string, create bool) (*os.Root, string, error) {
	resolvedHome, err := filepath.EvalSymlinks(strings.TrimSpace(dir))
	if err != nil {
		return nil, "", err
	}
	resolvedHome, err = filepath.Abs(resolvedHome)
	if err != nil {
		return nil, "", err
	}
	root, err := os.OpenRoot(resolvedHome)
	if err != nil {
		return nil, "", err
	}
	defer root.Close()
	temp, err := openArtifactDirectory(root, "temp", create)
	if err != nil {
		return nil, "", err
	}
	defer temp.Close()
	base, err := openArtifactDirectory(temp, "session-artifacts", create)
	return base, filepath.Join(resolvedHome, "temp", "session-artifacts"), err
}

// Validate each managed component before opening it. Root-relative operations
// keep creation and cleanup confined if a path is replaced concurrently.
func openArtifactDirectory(parent *os.Root, name string, create bool) (*os.Root, error) {
	if create {
		if err := parent.Mkdir(name, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
	}
	info, err := parent.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("home: session artifact component %q must be a directory, not a symlink", name)
	}
	root, err := parent.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, errors.New("home: session artifact directory changed while opening")
	}
	return root, nil
}

package tool

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	errWorkspaceDirsRequired     = errors.New("workspace directories are required")
	errWorkspacePathNotAllowed   = errors.New("path is outside authorized workspace directories")
	errWorkspaceFilePathRequired = errors.New("file path is required")
)

func normalizeWorkspaceDirs(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		cleaned := filepath.Clean(dir)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

func resolveWorkspacePath(roots []string, rawPath string, allowRoot, allowMissing bool) (string, string, string, error) {
	roots = normalizeWorkspaceDirs(roots)
	if len(roots) == 0 {
		return "", "", "", errWorkspaceDirsRequired
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "."
	}
	candidates := make([]string, 0, len(roots))
	if filepath.IsAbs(rawPath) {
		candidates = append(candidates, filepath.Clean(rawPath))
	} else {
		for _, root := range roots {
			candidates = append(candidates, filepath.Join(root, rawPath))
		}
	}
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		for _, root := range roots {
			root = filepath.Clean(root)
			if !pathInsideRoot(candidate, root) {
				continue
			}
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			if resolvedCandidate, err := filepath.EvalSymlinks(candidate); err == nil {
				if !pathInsideRoot(resolvedCandidate, resolvedRoot) {
					continue
				}
				rel, err := filepath.Rel(resolvedRoot, resolvedCandidate)
				if err != nil {
					continue
				}
				if rel == "" {
					rel = "."
				}
				if rel == "." && !allowRoot {
					return "", "", "", errWorkspaceFilePathRequired
				}
				return root, resolvedCandidate, filepath.ToSlash(rel), nil
			}
			if !allowMissing {
				continue
			}
			resolvedParent, err := resolveExistingParent(candidate)
			if err != nil || !pathInsideRoot(resolvedParent, resolvedRoot) {
				continue
			}
			rel, err := filepath.Rel(root, candidate)
			if err != nil {
				continue
			}
			if rel == "" {
				rel = "."
			}
			if rel == "." && !allowRoot {
				return "", "", "", errWorkspaceFilePathRequired
			}
			return root, candidate, filepath.ToSlash(rel), nil
		}
	}
	return "", "", "", errWorkspacePathNotAllowed
}

func resolveExistingParent(path string) (string, error) {
	parent := filepath.Dir(filepath.Clean(path))
	for {
		if _, err := os.Stat(parent); err == nil {
			return filepath.EvalSymlinks(parent)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", os.ErrNotExist
		}
		parent = next
	}
}

func pathInsideRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"reason":"encode_error"}`
	}
	return string(b)
}

// Package projectpath resolves paths inside explicitly authorized project roots.
package projectpath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrRootsRequired  = errors.New("project directories are required")
	ErrPathNotAllowed = errors.New("path is outside authorized project directories")
	ErrFileRequired   = errors.New("file path is required")
)

func NormalizeRoots(roots []string) []string {
	seen := make(map[string]bool, len(roots))
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" || !filepath.IsAbs(root) {
			continue
		}
		cleaned := filepath.Clean(root)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

// Resolve returns the authorized root, resolved target and slash-separated
// relative path. Existing symlinks may only resolve inside the same root.
func Resolve(roots []string, rawPath string, allowRoot, allowMissing bool) (string, string, string, error) {
	roots = NormalizeRoots(roots)
	if len(roots) == 0 {
		return "", "", "", ErrRootsRequired
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
			if !Inside(candidate, root) {
				continue
			}
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			if resolvedCandidate, err := filepath.EvalSymlinks(candidate); err == nil {
				if !Inside(resolvedCandidate, resolvedRoot) {
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
					return "", "", "", ErrFileRequired
				}
				return root, resolvedCandidate, filepath.ToSlash(rel), nil
			}
			if !allowMissing {
				continue
			}
			resolvedParent, err := ResolveExistingParent(candidate)
			if err != nil || !Inside(resolvedParent, resolvedRoot) {
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
				return "", "", "", ErrFileRequired
			}
			return root, candidate, filepath.ToSlash(rel), nil
		}
	}
	return "", "", "", ErrPathNotAllowed
}

func Inside(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func ResolveExistingParent(path string) (string, error) {
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

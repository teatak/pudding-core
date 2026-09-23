// Package projectpath resolves paths inside explicitly authorized project roots.
package projectpath

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrRootsRequired        = errors.New("project directories are required")
	ErrAbsolutePathRequired = errors.New("an absolute path is required when multiple project directories are authorized")
	ErrPathNotAllowed       = errors.New("path is outside authorized project directories")
	ErrFileRequired         = errors.New("file path is required")
)

type AbsolutePathRequiredError struct {
	Roots []string
}

func (e *AbsolutePathRequiredError) Error() string {
	return ErrAbsolutePathRequired.Error() + "; authorized roots: " + strings.Join(e.Roots, ", ")
}
func (e *AbsolutePathRequiredError) Unwrap() error { return ErrAbsolutePathRequired }

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
// relative path. A relative target requires exactly one root. Target selection
// never depends on whether a file exists or whether the caller may create it.
// Existing symlinks must resolve inside an authorized root. Authorization uses
// canonical paths so aliases of a root or its ancestors have the same identity.
func Resolve(roots []string, rawPath string, allowRoot, allowMissing bool) (string, string, string, error) {
	roots = NormalizeRoots(roots)
	if len(roots) == 0 {
		return "", "", "", ErrRootsRequired
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "."
	}
	candidate := filepath.Clean(rawPath)
	if !filepath.IsAbs(candidate) {
		if len(roots) != 1 {
			return "", "", "", &AbsolutePathRequiredError{Roots: roots}
		}
		candidate = filepath.Join(roots[0], candidate)
	}
	for _, root := range roots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			if Inside(candidate, root) {
				return "", "", "", err
			}
			continue
		}
		resolvedCandidate, resolveErr := filepath.EvalSymlinks(candidate)
		if resolveErr != nil {
			if !errors.Is(resolveErr, os.ErrNotExist) {
				return "", "", "", resolveErr
			}
			rawParent, resolvedParent, err := resolveExistingParent(candidate)
			if err != nil {
				return "", "", "", err
			}
			if !Inside(resolvedParent, resolvedRoot) {
				continue
			}
			// A dangling symlink is not a new file: following it while writing
			// could leave the authorized root.
			if info, err := os.Lstat(candidate); err == nil && info.Mode()&os.ModeSymlink != 0 {
				return "", "", "", resolveErr
			}
			missingSuffix, err := filepath.Rel(rawParent, candidate)
			if err != nil {
				return "", "", "", err
			}
			resolvedCandidate = filepath.Join(resolvedParent, missingSuffix)
		}
		if !Inside(resolvedCandidate, resolvedRoot) {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolvedCandidate)
		if err != nil {
			return "", "", "", err
		}
		if rel == "." && !allowRoot {
			return "", "", "", ErrFileRequired
		}
		if resolveErr != nil && !allowMissing {
			return "", "", "", resolveErr
		}
		return root, resolvedCandidate, filepath.ToSlash(rel), nil
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
	_, resolved, err := resolveExistingParent(path)
	return resolved, err
}

func resolveExistingParent(path string) (string, string, error) {
	parent := filepath.Dir(filepath.Clean(path))
	for {
		if _, err := os.Lstat(parent); err == nil {
			resolved, err := filepath.EvalSymlinks(parent)
			return parent, resolved, err
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", "", os.ErrNotExist
		}
		parent = next
	}
}

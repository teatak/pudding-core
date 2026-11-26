package tool

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func commandEnvKeyRequiresApproval(key, value string, projectDirs []string) bool {
	if !commandEnvironmentRequiresApproval(map[string]string{key: value}) {
		return false
	}
	if (key != "PYTHONPATH" && key != "VIRTUAL_ENV") || value == "" {
		return true
	}
	paths := []string{value}
	if key == "PYTHONPATH" {
		paths = filepath.SplitList(value)
	}
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			return true
		}
		_, resolved, _, err := resolveProjectPath(projectDirs, path, true, false)
		if err != nil {
			return true
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return true
		}
	}
	return false
}

// Check both spellings: a writable alias to a trusted bin is replaceable,
// and a symlink inside a toolchain can point out of its installation.
func readOnlyToolchainPATH(value string, writableRoots []string) bool {
	if value == "" || len(writableRoots) == 0 {
		return false
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	realHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		return false
	}
	for _, dir := range filepath.SplitList(value) {
		if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir || strings.TrimSpace(dir) != dir || !toolchainBinDirectory(dir, home) {
			return false
		}
		real, err := filepath.EvalSymlinks(dir)
		if err != nil || !toolchainBinDirectory(real, realHome) {
			return false
		}
		info, err := os.Stat(real)
		if err != nil || !info.IsDir() {
			return false
		}
		for _, root := range writableRoots {
			resolved, err := filepath.EvalSymlinks(root)
			if err != nil || pathInsideRoot(strings.ToLower(dir), strings.ToLower(root)) ||
				pathInsideRoot(strings.ToLower(real), strings.ToLower(resolved)) {
				return false
			}
		}
	}
	return true
}

func toolchainBinDirectory(dir, home string) bool {
	if slices.Contains(systemExecutableDirs(), dir) {
		return true
	}
	// Restrict the read policy to executable directories. Package caches and
	// general system read roots do not qualify as PATH entries.
	base := filepath.Base(dir)
	for _, root := range sandboxToolchainCandidates(home) {
		if filepath.Base(root) == "pnpm" {
			if dir == root {
				return true
			}
			continue // Its store is a package cache.
		}
		if pathInsideRoot(dir, root) && (base == "bin" || base == "sbin" || base == "shims") {
			return true
		}
	}
	return (base == "bin" || base == "sbin") && (pathInsideRoot(dir, "/usr/local/Cellar") || pathInsideRoot(dir, "/usr/local/opt") ||
		pathInsideRoot(dir, "/opt/homebrew/Cellar") || pathInsideRoot(dir, "/opt/homebrew/opt"))
}

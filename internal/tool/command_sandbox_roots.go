package tool

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sandboxStaticReadRoots is the single source of truth for host paths that a
// sandboxed command may read without extending the authorized project roots.
func sandboxStaticReadRoots() []string {
	return []string{
		"/System",
		"/Library",
		"/usr",
		"/bin",
		"/sbin",
		"/Applications",
		"/opt/homebrew",
		"/private/etc",
		"/private/var/db",
	}
}

func sandboxKnownReadRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	candidates := []string{
		filepath.Join(home, "go", "pkg", "mod"),
		filepath.Join(home, ".cargo", "registry"),
		filepath.Join(home, ".cargo", "git"),
		filepath.Join(home, ".cache", "pip"),
		filepath.Join(home, "Library", "Caches", "pip"),
		filepath.Join(home, ".npm", "_cacache"),
		filepath.Join(home, "Library", "pnpm", "store"),
		filepath.Join(home, ".local", "share", "pnpm", "store"),
	}
	candidates = append(candidates, sandboxToolchainCandidates(home)...)
	return sandboxExistingPaths(candidates, true)
}

func sandboxToolchainCandidates(home string) []string {
	return []string{
		filepath.Join(home, ".asdf", "installs"),
		filepath.Join(home, ".asdf", "shims"),
		filepath.Join(home, ".cargo", "bin"),
		filepath.Join(home, ".nvm", "versions"),
		filepath.Join(home, ".npm-global"),
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, ".local", "share", "pipx", "venvs"),
		filepath.Join(home, ".volta", "tools", "image"),
		filepath.Join(home, ".volta", "bin"),
		filepath.Join(home, ".pyenv", "versions"),
		filepath.Join(home, ".rustup", "toolchains"),
		filepath.Join(home, ".sdkman", "candidates"),
		filepath.Join(home, ".local", "share", "mise", "installs"),
		filepath.Join(home, ".local", "share", "mise", "shims"),
		filepath.Join(home, "go", "bin"),
		filepath.Join(home, "Library", "Android", "sdk"),
		filepath.Join(home, "Library", "pnpm"),
	}
}

func sandboxExistingPaths(paths []string, directories bool) []string {
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || info.IsDir() != directories {
			continue
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			out = append(out, resolved)
		}
	}
	return sandboxUniquePaths(out)
}

func sandboxUniquePaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func commandPathReadableInSandbox(path string) bool {
	path = strings.Trim(strings.TrimSpace(path), "\"'")
	if !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	for _, root := range append(sandboxStaticReadRoots(), sandboxKnownReadRoots()...) {
		if pathInsideRoot(path, root) {
			return true
		}
	}
	return false
}

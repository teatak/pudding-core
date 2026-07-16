package tool

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func mergedExecutablePATH(current string) string {
	seen := map[string]struct{}{}
	dirs := make([]string, 0)
	add := func(dir string) {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			return
		}
		if _, ok := seen[dir]; ok {
			return
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}
	for _, dir := range filepath.SplitList(current) {
		add(dir)
	}
	for _, dir := range commonExecutableDirs() {
		add(dir)
	}
	return strings.Join(dirs, string(os.PathListSeparator))
}

func commonExecutableDirs() []string {
	dirs := []string{
		"/opt/homebrew/bin",
		"/opt/homebrew/sbin",
		"/usr/local/bin",
		"/usr/local/sbin",
		"/usr/bin",
		"/bin",
		"/usr/sbin",
		"/sbin",
	}
	homeDir, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(homeDir) == "" {
		return dirs
	}
	dirs = append(dirs,
		filepath.Join(homeDir, ".volta", "bin"),
		filepath.Join(homeDir, ".npm-global", "bin"),
		filepath.Join(homeDir, ".local", "bin"),
		filepath.Join(homeDir, ".cargo", "bin"),
		filepath.Join(homeDir, "go", "bin"),
		filepath.Join(homeDir, "Library", "pnpm"),
		filepath.Join(homeDir, ".asdf", "shims"),
		filepath.Join(homeDir, ".local", "share", "mise", "shims"),
	)
	nodeRoot := filepath.Join(homeDir, ".nvm", "versions", "node")
	if entries, err := os.ReadDir(nodeRoot); err == nil {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				names = append(names, entry.Name())
			}
		}
		sort.Sort(sort.Reverse(sort.StringSlice(names)))
		for _, name := range names {
			dirs = append(dirs, filepath.Join(nodeRoot, name, "bin"))
		}
	}
	return dirs
}

func resolveExecutableFromEnv(command, cwd string, env []string) (string, error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return "", errors.New("command executable is required")
	}
	if strings.ContainsRune(command, filepath.Separator) {
		return command, nil
	}
	for _, dir := range filepath.SplitList(executableEnvValue(env, "PATH")) {
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) && strings.TrimSpace(cwd) != "" {
			dir = filepath.Join(cwd, dir)
		}
		candidate := filepath.Join(dir, command)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate, nil
		}
	}
	if found, err := exec.LookPath(command); err == nil {
		return found, nil
	}
	return "", fmt.Errorf("command executable %q was not found on PATH", command)
}

func executableEnvValue(env []string, key string) string {
	for _, item := range env {
		gotKey, value, ok := strings.Cut(item, "=")
		if ok && strings.EqualFold(gotKey, key) {
			return value
		}
	}
	return ""
}

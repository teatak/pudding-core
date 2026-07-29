package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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
		filepath.Join(homeDir, ".pyenv", "shims"),
		filepath.Join(homeDir, ".rbenv", "shims"),
		filepath.Join(homeDir, ".bun", "bin"),
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
		sort.SliceStable(names, func(i, j int) bool {
			return compareVersionNames(names[i], names[j]) > 0
		})
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
	names := executableCandidateNames(command, env)
	for _, dir := range filepath.SplitList(executableEnvValue(env, "PATH")) {
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) && strings.TrimSpace(cwd) != "" {
			dir = filepath.Join(cwd, dir)
		}
		for _, name := range names {
			candidate := filepath.Join(dir, name)
			info, err := os.Stat(candidate)
			if err == nil && !info.IsDir() && (runtime.GOOS == "windows" || info.Mode()&0o111 != 0) {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("command executable %q was not found on PATH", command)
}

func executableCandidateNames(command string, env []string) []string {
	if runtime.GOOS != "windows" || filepath.Ext(command) != "" {
		return []string{command}
	}
	extensions := filepath.SplitList(executableEnvValue(env, "PATHEXT"))
	if len(extensions) == 0 {
		extensions = []string{".COM", ".EXE", ".BAT", ".CMD"}
	}
	names := make([]string, 0, len(extensions)+1)
	names = append(names, command)
	for _, extension := range extensions {
		extension = strings.TrimSpace(extension)
		if extension == "" {
			continue
		}
		if !strings.HasPrefix(extension, ".") {
			extension = "." + extension
		}
		names = append(names, command+strings.ToLower(extension), command+strings.ToUpper(extension))
	}
	return names
}

func compareVersionNames(left, right string) int {
	leftParts := versionNameParts(left)
	rightParts := versionNameParts(right)
	for index := 0; index < len(leftParts) || index < len(rightParts); index++ {
		leftValue := 0
		if index < len(leftParts) {
			leftValue = leftParts[index]
		}
		rightValue := 0
		if index < len(rightParts) {
			rightValue = rightParts[index]
		}
		if leftValue > rightValue {
			return 1
		}
		if leftValue < rightValue {
			return -1
		}
	}
	return strings.Compare(left, right)
}

func versionNameParts(value string) []int {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r < '0' || r > '9'
	})
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		part, err := strconv.Atoi(field)
		if err == nil {
			parts = append(parts, part)
		}
	}
	return parts
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

//go:build darwin

package tool

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	macOSCommandSandboxExecutable = "/usr/bin/sandbox-exec"
	macOSCommandSandboxKind       = "macos-seatbelt"
)

//go:embed assets/command_sandbox_macos.sbpl
var macOSCommandSandboxBasePolicy string

type macOSCommandRunner struct {
	homeDir string
	direct  commandRunner
}

func newPlatformCommandRunner(homeDir string) commandRunner {
	return &macOSCommandRunner{
		homeDir: strings.TrimSpace(homeDir),
		direct:  newDirectCommandRunner(),
	}
}

func (r *macOSCommandRunner) Prepare(spec commandSpec) (*commandExecution, error) {
	if spec.SandboxMode == CommandSandboxBypass {
		return r.direct.Prepare(spec)
	}
	if r.homeDir == "" {
		return nil, errors.New("command sandbox home directory is required")
	}
	if _, err := os.Stat(macOSCommandSandboxExecutable); err != nil {
		return nil, fmt.Errorf("command sandbox unavailable: %w", err)
	}

	projectRoots, err := sandboxCanonicalRoots(spec.ProjectDirs)
	if err != nil {
		return nil, err
	}
	stateRoot, err := r.prepareStateRoot(projectRoots)
	if err != nil {
		return nil, err
	}
	executable, err := sandboxExecutable(spec.Executable, spec.CWD, spec.Env)
	if err != nil {
		return nil, err
	}
	toolchainRoot, err := sandboxExecutableRoot(executable, projectRoots)
	if err != nil {
		return nil, err
	}

	readRoots := append([]string(nil), projectRoots...)
	readRoots = append(readRoots, stateRoot)
	readRoots = append(readRoots, sandboxKnownReadRoots()...)
	readRoots = append(readRoots, toolchainRoot)
	readRoots = sandboxUniquePaths(readRoots)
	writeRoots := sandboxUniquePaths(append(projectRoots, stateRoot))
	profile, definitions := sandboxProfile(readRoots, writeRoots)

	args := []string{"-p", profile}
	for _, definition := range definitions {
		args = append(args, "-D"+definition.key+"="+definition.path)
	}
	args = append(args, "--", executable)
	args = append(args, spec.Args...)

	cmd := exec.Command(macOSCommandSandboxExecutable, args...)
	cmd.Dir = spec.CWD
	cmd.Env = sandboxEnvironment(spec.Env, stateRoot)
	configureCommandProcess(cmd)
	return &commandExecution{
		Cmd:         cmd,
		Sandboxed:   true,
		SandboxKind: macOSCommandSandboxKind,
	}, nil
}

func (r *macOSCommandRunner) prepareStateRoot(projectRoots []string) (string, error) {
	homeRoot, err := filepath.EvalSymlinks(r.homeDir)
	if err != nil {
		return "", fmt.Errorf("resolve command sandbox home: %w", err)
	}
	homeInfo, err := os.Stat(homeRoot)
	if err != nil || !homeInfo.IsDir() {
		return "", errors.New("command sandbox home must be a directory")
	}
	hash := sha256.Sum256([]byte(strings.Join(projectRoots, "\x00")))
	runtimeRoot := filepath.Join(homeRoot, "runtime")
	sandboxRoot := filepath.Join(runtimeRoot, "command-sandbox")
	stateRoot := filepath.Join(sandboxRoot, hex.EncodeToString(hash[:]))
	for _, dir := range []string{
		runtimeRoot,
		sandboxRoot,
		stateRoot,
		filepath.Join(stateRoot, "cache"),
		filepath.Join(stateRoot, "tmp"),
	} {
		if err := ensureSandboxStateDir(dir); err != nil {
			return "", fmt.Errorf("prepare command sandbox state: %w", err)
		}
	}
	resolved, err := filepath.EvalSymlinks(stateRoot)
	if err != nil {
		return "", fmt.Errorf("resolve command sandbox state: %w", err)
	}
	if !pathInsideRoot(resolved, homeRoot) {
		return "", errors.New("command sandbox state escapes Pudding home")
	}
	return resolved, nil
}

func ensureSandboxStateDir(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("sandbox state path must be a directory, not a symlink")
	}
	return nil
}

func sandboxCanonicalRoots(roots []string) ([]string, error) {
	roots = normalizeProjectDirs(roots)
	if len(roots) == 0 {
		return nil, errProjectDirsRequired
	}
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil, fmt.Errorf("resolve project root %q: %w", root, err)
		}
		info, err := os.Stat(resolved)
		if err != nil {
			return nil, fmt.Errorf("inspect project root %q: %w", root, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("project root %q is not a directory", root)
		}
		out = append(out, resolved)
	}
	return sandboxUniquePaths(out), nil
}

func sandboxExecutable(executable, cwd string, env []string) (string, error) {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return "", errors.New("command executable is required")
	}
	if strings.ContainsRune(executable, filepath.Separator) {
		if !filepath.IsAbs(executable) {
			executable = filepath.Join(cwd, executable)
		}
		resolved, err := filepath.EvalSymlinks(filepath.Clean(executable))
		if err != nil {
			return "", fmt.Errorf("resolve command executable: %w", err)
		}
		return resolved, nil
	}
	pathValue := sandboxEnvValue(env, "PATH")
	for _, dir := range filepath.SplitList(pathValue) {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(dir, executable)
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
			continue
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("command executable %q was not found on PATH", executable)
}

func sandboxExecutableRoot(executable string, projectRoots []string) (string, error) {
	for _, root := range append(projectRoots, sandboxStaticReadRoots()...) {
		if pathInsideRoot(executable, root) {
			return "", nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	for _, root := range sandboxToolchainCandidates(home) {
		if pathInsideRoot(executable, root) {
			return root, nil
		}
	}
	return "", fmt.Errorf("command executable %q is outside the project and trusted toolchains", executable)
}

func sandboxStaticReadRoots() []string {
	return []string{
		"/System",
		"/Library",
		"/usr",
		"/bin",
		"/sbin",
		"/Applications",
		"/opt/homebrew",
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

type sandboxDefinition struct {
	key  string
	path string
}

func sandboxProfile(readRoots, writeRoots []string) (string, []sandboxDefinition) {
	var policy strings.Builder
	policy.WriteString(macOSCommandSandboxBasePolicy)
	policy.WriteString("\n; Per-command filesystem grants.\n")
	ancestorRoots := append(append([]string(nil), sandboxStaticReadRoots()...), readRoots...)
	ancestorRoots = append(ancestorRoots, writeRoots...)
	ancestors := sandboxAncestorPaths(ancestorRoots)
	definitions := make([]sandboxDefinition, 0, len(ancestors)+len(readRoots)+len(writeRoots))
	writeRule := func(operation, prefix, filter string, paths []string) {
		if len(paths) == 0 {
			return
		}
		fmt.Fprintf(&policy, "(allow %s\n", operation)
		for index, path := range paths {
			key := fmt.Sprintf("%s_%d", prefix, index)
			fmt.Fprintf(&policy, "  (%s (param \"%s\"))\n", filter, key)
			definitions = append(definitions, sandboxDefinition{key: key, path: path})
		}
		policy.WriteString(")\n")
	}
	writeRule("file-read-metadata file-test-existence", "ANCESTOR", "literal", ancestors)
	writeRule("file-read* file-test-existence file-map-executable", "READ_ROOT", "subpath", readRoots)
	writeRule("file-write*", "WRITE_ROOT", "subpath", writeRoots)
	return policy.String(), definitions
}

func sandboxAncestorPaths(paths []string) []string {
	var ancestors []string
	for _, path := range paths {
		path = filepath.Clean(path)
		if !filepath.IsAbs(path) {
			continue
		}
		for parent := filepath.Dir(path); parent != string(filepath.Separator); parent = filepath.Dir(parent) {
			ancestors = append(ancestors, parent)
		}
	}
	return sandboxUniquePaths(ancestors)
}

func sandboxEnvironment(env []string, stateRoot string) []string {
	out := append([]string(nil), env...)
	tmpDir := filepath.Join(stateRoot, "tmp")
	cacheDir := filepath.Join(stateRoot, "cache")
	goPath := filepath.Join(stateRoot, "go")
	out = sandboxSetEnv(out, "TMPDIR", tmpDir+string(filepath.Separator))
	out = sandboxSetEnv(out, "TMP", tmpDir)
	out = sandboxSetEnv(out, "TEMP", tmpDir)
	out = sandboxSetEnv(out, "XDG_CACHE_HOME", cacheDir)
	out = sandboxSetEnv(out, "GOPATH", goPath)
	out = sandboxSetEnv(out, "GOMODCACHE", filepath.Join(goPath, "pkg", "mod"))
	out = sandboxSetEnv(out, "GOCACHE", filepath.Join(cacheDir, "go-build"))
	out = sandboxSetEnv(out, "NPM_CONFIG_CACHE", filepath.Join(cacheDir, "npm"))
	out = sandboxSetEnv(out, "npm_config_store_dir", filepath.Join(cacheDir, "pnpm-store"))
	out = sandboxSetEnv(out, "YARN_CACHE_FOLDER", filepath.Join(cacheDir, "yarn"))
	out = sandboxSetEnv(out, "PIP_CACHE_DIR", filepath.Join(cacheDir, "pip"))
	out = sandboxSetEnv(out, "UV_CACHE_DIR", filepath.Join(cacheDir, "uv"))
	out = sandboxSetEnv(out, "GIT_CONFIG_GLOBAL", "/dev/null")
	out = sandboxSetEnv(out, "GIT_CONFIG_NOSYSTEM", "1")
	return out
}

func sandboxSetEnv(env []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	for index, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			env[index] = key + "=" + value
			return env
		}
	}
	return append(env, key+"="+value)
}

func sandboxEnvValue(env []string, key string) string {
	prefix := strings.ToUpper(key) + "="
	for _, entry := range env {
		if strings.HasPrefix(strings.ToUpper(entry), prefix) {
			return entry[len(prefix):]
		}
	}
	return ""
}

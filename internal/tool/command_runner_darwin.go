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
	stateRoot, err := r.prepareStateRoot(spec.StateKey, projectRoots)
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
	readRoots = append(readRoots, sandboxStaticReadRoots()...)
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

func (r *macOSCommandRunner) prepareStateRoot(stateKey string, projectRoots []string) (string, error) {
	homeRoot, err := filepath.EvalSymlinks(r.homeDir)
	if err != nil {
		return "", fmt.Errorf("resolve command sandbox home: %w", err)
	}
	homeInfo, err := os.Stat(homeRoot)
	if err != nil || !homeInfo.IsDir() {
		return "", errors.New("command sandbox home must be a directory")
	}
	stateKey = strings.TrimSpace(stateKey)
	if stateKey == "" && len(projectRoots) > 0 {
		stateKey = projectRoots[0]
	}
	if stateKey == "" {
		return "", errors.New("command sandbox state key is required")
	}
	runtimeRoot := filepath.Join(homeRoot, "runtime")
	sandboxRoot := filepath.Join(runtimeRoot, "command-sandbox")
	stateRoot := sandboxStateRoot(sandboxRoot, stateKey)
	for _, dir := range []string{
		runtimeRoot,
		sandboxRoot,
	} {
		if err := ensureSandboxStateDir(dir); err != nil {
			return "", fmt.Errorf("prepare command sandbox state: %w", err)
		}
	}
	if err := migrateLegacySandboxState(sandboxRoot, stateRoot, projectRoots); err != nil {
		return "", fmt.Errorf("migrate command sandbox state: %w", err)
	}
	for _, dir := range []string{
		stateRoot,
		filepath.Join(stateRoot, "cache"),
		filepath.Join(stateRoot, "cache", "corepack"),
		filepath.Join(stateRoot, "cache", "python-bytecode"),
		filepath.Join(stateRoot, "config"),
		filepath.Join(stateRoot, "node"),
		filepath.Join(stateRoot, "node", "npm-prefix"),
		filepath.Join(stateRoot, "node", "npm-prefix", "bin"),
		filepath.Join(stateRoot, "node", "pnpm-home"),
		filepath.Join(stateRoot, "node", "yarn-global"),
		filepath.Join(stateRoot, "python"),
		filepath.Join(stateRoot, "tmp"),
	} {
		if err := ensureSandboxStateDir(dir); err != nil {
			return "", fmt.Errorf("prepare command sandbox state: %w", err)
		}
	}
	for _, file := range []string{
		filepath.Join(stateRoot, "config", "npm-user.conf"),
		filepath.Join(stateRoot, "config", "npm-global.conf"),
	} {
		if err := ensureSandboxStateFile(file); err != nil {
			return "", fmt.Errorf("prepare command sandbox config: %w", err)
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

func sandboxStateRoot(sandboxRoot, stateKey string) string {
	hash := sha256.Sum256([]byte(stateKey))
	return filepath.Join(sandboxRoot, hex.EncodeToString(hash[:]))
}

func migrateLegacySandboxState(sandboxRoot, stateRoot string, projectRoots []string) error {
	legacyRoot := sandboxStateRoot(sandboxRoot, strings.Join(projectRoots, "\x00"))
	if legacyRoot == stateRoot {
		return nil
	}
	if _, err := os.Lstat(stateRoot); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	legacyInfo, err := os.Lstat(legacyRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if legacyInfo.Mode()&os.ModeSymlink != 0 || !legacyInfo.IsDir() {
		return errors.New("legacy sandbox state path must be a directory, not a symlink")
	}
	if err := os.Rename(legacyRoot, stateRoot); err != nil {
		// Another command may have completed the same atomic migration.
		if _, targetErr := os.Lstat(stateRoot); targetErr == nil {
			return nil
		}
		return err
	}
	return nil
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

func ensureSandboxStateFile(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return errors.New("sandbox state path must be a regular file, not a symlink")
		}
		return nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	return file.Close()
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

type sandboxDefinition struct {
	key  string
	path string
}

func sandboxProfile(readRoots, writeRoots []string) (string, []sandboxDefinition) {
	var policy strings.Builder
	policy.WriteString(macOSCommandSandboxBasePolicy)
	policy.WriteString("\n; Per-command filesystem grants.\n")
	ancestorRoots := append([]string(nil), readRoots...)
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
	nodeDir := filepath.Join(stateRoot, "node")
	npmPrefix := filepath.Join(nodeDir, "npm-prefix")
	pnpmHome := filepath.Join(nodeDir, "pnpm-home")
	out = sandboxSetEnv(out, "TMPDIR", tmpDir+string(filepath.Separator))
	out = sandboxSetEnv(out, "TMP", tmpDir)
	out = sandboxSetEnv(out, "TEMP", tmpDir)
	out = sandboxSetEnv(out, "XDG_CACHE_HOME", cacheDir)
	out = sandboxSetEnv(out, "GOPATH", goPath)
	out = sandboxSetEnv(out, "GOMODCACHE", filepath.Join(goPath, "pkg", "mod"))
	out = sandboxSetEnv(out, "GOCACHE", filepath.Join(cacheDir, "go-build"))
	out = sandboxSetEnv(out, "NPM_CONFIG_CACHE", filepath.Join(cacheDir, "npm"))
	out = sandboxSetEnv(out, "NPM_CONFIG_PREFIX", npmPrefix)
	out = sandboxSetEnv(out, "NPM_CONFIG_USERCONFIG", filepath.Join(stateRoot, "config", "npm-user.conf"))
	out = sandboxSetEnv(out, "NPM_CONFIG_GLOBALCONFIG", filepath.Join(stateRoot, "config", "npm-global.conf"))
	out = sandboxSetEnv(out, "npm_config_store_dir", filepath.Join(cacheDir, "pnpm-store"))
	out = sandboxSetEnv(out, "PNPM_HOME", pnpmHome)
	out = sandboxSetEnv(out, "COREPACK_HOME", filepath.Join(cacheDir, "corepack"))
	out = sandboxSetEnv(out, "YARN_CACHE_FOLDER", filepath.Join(cacheDir, "yarn"))
	out = sandboxSetEnv(out, "YARN_GLOBAL_FOLDER", filepath.Join(nodeDir, "yarn-global"))
	out = sandboxSetEnv(out, "NODE_REPL_HISTORY", filepath.Join(nodeDir, "repl_history"))
	out = sandboxSetEnv(out, "NODE_PATH", filepath.Join(npmPrefix, "lib", "node_modules"))
	out = sandboxPrependPath(out, filepath.Join(npmPrefix, "bin"), pnpmHome)
	out = sandboxSetEnv(out, "PIP_CACHE_DIR", filepath.Join(cacheDir, "pip"))
	out = sandboxSetEnv(out, "UV_CACHE_DIR", filepath.Join(cacheDir, "uv"))
	pythonUserBase := filepath.Join(stateRoot, "python")
	out = sandboxSetEnv(out, "PYTHONUSERBASE", pythonUserBase)
	out = sandboxSetEnv(out, "PYTHONPYCACHEPREFIX", filepath.Join(cacheDir, "python-bytecode"))
	out = sandboxPrependPath(out, filepath.Join(pythonUserBase, "bin"))
	if certificateBundle := sandboxCertificateBundle(); certificateBundle != "" {
		out = sandboxSetEnvDefault(out, "SSL_CERT_FILE", certificateBundle)
		out = sandboxSetEnvDefault(out, "REQUESTS_CA_BUNDLE", certificateBundle)
		out = sandboxSetEnvDefault(out, "CURL_CA_BUNDLE", certificateBundle)
		out = sandboxSetEnvDefault(out, "PIP_CERT", certificateBundle)
	}
	out = sandboxSetEnv(out, "GIT_CONFIG_GLOBAL", "/dev/null")
	out = sandboxSetEnv(out, "GIT_CONFIG_NOSYSTEM", "1")
	return out
}

func sandboxCertificateBundle() string {
	for _, candidate := range []string{
		"/private/etc/ssl/cert.pem",
		"/etc/ssl/cert.pem",
		"/opt/homebrew/etc/ca-certificates/cert.pem",
		"/opt/homebrew/etc/openssl@3/cert.pem",
		"/usr/local/etc/openssl@3/cert.pem",
	} {
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return ""
}

func sandboxPrependPath(env []string, paths ...string) []string {
	current := filepath.SplitList(sandboxEnvValue(env, "PATH"))
	return sandboxSetEnv(env, "PATH", strings.Join(append(paths, current...), string(os.PathListSeparator)))
}

func sandboxSetEnvDefault(env []string, key, value string) []string {
	if sandboxEnvValue(env, key) != "" {
		return env
	}
	return sandboxSetEnv(env, key, value)
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

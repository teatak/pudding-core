package tool

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// CommandSessionGrant describes a deliberately narrow, user-selected lease.
// It is not an executable-prefix allowlist. Dynamic or ineligible commands
// remain single-call approvals. Key is private to the approval engine.
type CommandSessionGrant struct {
	Key             string               `json:"-"`
	Kind            string               `json:"kind"`
	Execution       CommandExecutionMode `json:"execution"`
	Executable      string               `json:"executable"`
	CWD             string               `json:"cwd"`
	InputPath       string               `json:"inputPath"`
	OutputDirectory string               `json:"outputDirectory"`
	ProfilePath     string               `json:"profilePath"`
	argv            []string
	env             []string
	callArgs        json.RawMessage
}

// Grants bind a literal invocation, not a program prefix. Sandbox grants never
// grant host access. The one host exception remains the bounded Chrome preview.
func CommandSessionGrantForCall(call Call) *CommandSessionGrant {
	if call.Name != CommandRun || runtime.GOOS == "windows" {
		return nil
	}
	args, err := decodeCommandRunArgs(call.Args)
	if err != nil || args.Background || args.TTY {
		return nil
	}
	argv := literalGrantCommand(args.Command)
	if len(argv) == 0 {
		return nil
	}
	env, err := commandEnvironment(args.Env)
	if err != nil {
		return nil
	}
	var grant *CommandSessionGrant
	if args.Execution == CommandExecutionHost {
		grant = chromeSessionGrantForCall(call, args, argv, env)
	} else {
		grant = sandboxSessionGrantForCall(call, args, argv, env)
	}
	if grant == nil {
		return nil
	}
	grant.Execution = args.Execution
	launchPath, err := resolveExecutableFromEnv(argv[0], grant.CWD, env)
	if err != nil {
		return nil
	}
	if !filepath.IsAbs(launchPath) {
		launchPath = filepath.Join(grant.CWD, launchPath)
	}
	grant.argv = append([]string{launchPath}, argv[1:]...)
	grant.env = env
	grant.callArgs = append(json.RawMessage(nil), call.Args...)
	// Chrome's key permits only its documented filename/viewport variations.
	roots := make([]string, 0, len(call.ProjectDirs))
	for _, root := range normalizeProjectDirs(call.ProjectDirs) {
		resolved, err := filepath.EvalSymlinks(root)
		if err != nil {
			return nil
		}
		roots = append(roots, resolved)
	}
	data, _ := json.Marshal([]any{grant.Key, launchPath, args.Execution, call.CommandStateKey, roots, env})
	hash := sha256.Sum256(data)
	grant.Key = hex.EncodeToString(hash[:])
	return grant
}

func literalGrantCommand(command string) []string {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(command), "command")
	if err != nil || len(file.Stmts) != 1 {
		return nil
	}
	stmt := file.Stmts[0]
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown || len(stmt.Redirs) != 0 {
		return nil
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 0 || len(call.Args) == 0 {
		return nil
	}
	argv := make([]string, 0, len(call.Args))
	for _, word := range call.Args {
		if !isStaticCommandWord(word) {
			return nil
		}
		for _, part := range word.Parts {
			if lit, ok := part.(*syntax.Lit); ok && (strings.ContainsAny(lit.Value, "\\*?[]") || strings.HasPrefix(lit.Value, "~")) {
				return nil
			}
			if quoted, ok := part.(*syntax.DblQuoted); ok {
				for _, inner := range quoted.Parts {
					if strings.Contains(inner.(*syntax.Lit).Value, "\\") {
						return nil
					}
				}
			}
		}
		value, ok := staticShellWord(word)
		if !ok {
			return nil
		}
		argv = append(argv, value)
	}
	return argv
}

func sandboxSessionGrantForCall(call Call, args commandRunArgs, argv, env []string) *CommandSessionGrant {
	if call.CommandSandbox != CommandSandboxEnforce {
		return nil
	}
	_, cwd, _, err := resolveProjectPath(call.ProjectDirs, args.CWD, true, false)
	if err != nil {
		return nil
	}
	// Only project-local Python path configuration may relax the environment
	// classifier for a reusable grant. Never lease PATH, NODE_OPTIONS, etc.
	for key, value := range args.Env {
		if !commandEnvironmentRequiresApproval(map[string]string{key: value}) {
			continue
		}
		if key != "PYTHONPATH" && key != "VIRTUAL_ENV" {
			return nil
		}
		for _, path := range filepath.SplitList(value) {
			_, resolved, _, err := resolveProjectPath(call.ProjectDirs, path, true, false)
			if !filepath.IsAbs(path) || err != nil {
				return nil
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.IsDir() {
				return nil
			}
		}
		if value == "" {
			return nil
		}
	}
	base := args
	base.Env = nil
	raw, _ := json.Marshal(base)
	risk, ok := classifyCommandCall(raw, call.ProjectDirs)
	if !ok || !risk.LowRisk || risk.Class != RiskClassCommand {
		return nil
	}
	// Limit reuse to ordinary development programs, not shell builtins,
	// wrappers or arbitrary application launchers. Arguments remain exact.
	switch commandOperation(argv[0]) {
	case "python", "python3", "node", "go", "npm", "pnpm", "yarn", "bun", "cargo", "rustc", "gcc", "g++", "clang", "clang++", "make", "cmake", "ctest", "pytest", "uv", "pip", "pip3", "git", "cat", "head", "tail", "wc", "rg":
	default:
		return nil
	}
	executable, err := resolveExecutableFromEnv(argv[0], cwd, env)
	if err != nil {
		return nil
	}
	if !filepath.IsAbs(executable) {
		executable = filepath.Join(cwd, executable)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil || commandPathInsideProject(executable, cwd, normalizeProjectDirs(call.ProjectDirs)) {
		return nil
	}
	resolvedArgv := append([]string{executable}, argv[1:]...)
	if commandRequiresApproval(resolvedArgv) || commandNeedsHostAccess(resolvedArgv) {
		return nil
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil
	}
	data, _ := json.Marshal([]any{executable, argv[1:], cwd, normalizeProjectDirs(call.ProjectDirs), env, info.Size(), info.ModTime().UnixNano(), info.Mode()})
	hash := sha256.Sum256(data)
	return &CommandSessionGrant{Key: hex.EncodeToString(hash[:]), Kind: "sandbox_command", Executable: executable, CWD: cwd}
}

// Validate again immediately before execution. An absolute, quoted invocation
// pins PATH lookup while preserving aliases such as .venv/bin/python. The fixed
// shell is still needed: the platform runner canonicalizes its own executable.
func (g *CommandSessionGrant) invocation(call Call) (string, []string, []string, error) {
	checkCall := call
	if g.Execution == CommandExecutionHost {
		checkCall.CommandSandbox = CommandSandboxEnforce
	}
	fresh := CommandSessionGrantForCall(checkCall)
	if !bytes.Equal(g.callArgs, call.Args) || fresh == nil || fresh.Key != g.Key {
		return "", nil, nil, errors.New("command approval context changed; request approval again")
	}
	return "/bin/sh", []string{"-c", "exec " + joinShellCommand(g.argv)}, append([]string(nil), g.env...), nil
}

func chromeSessionGrantForCall(call Call, args commandRunArgs, literalArgv, env []string) *CommandSessionGrant {
	if len(args.Env) != 0 {
		return nil
	}
	argv := append([]string(nil), literalArgv...)
	for _, arg := range argv {
		// The grant must bind literal paths, not shell-expanded host paths.
		if strings.ContainsAny(arg, "$`") || strings.HasPrefix(arg, "~") {
			return nil
		}
	}
	if !filepath.IsAbs(argv[0]) || !strings.HasSuffix(filepath.ToSlash(argv[0]), "/Google Chrome.app/Contents/MacOS/Google Chrome") {
		return nil
	}
	executable, err := filepath.EvalSymlinks(argv[0])
	if err != nil {
		return nil
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil
	}
	_, cwd, _, err := resolveProjectPath(call.ProjectDirs, args.CWD, true, false)
	if err != nil {
		return nil
	}
	// Never lease a program the agent can rewrite through its project access.
	// This also excludes a project-local script merely named Google Chrome.
	if commandPathInsideProject(executable, cwd, normalizeProjectDirs(call.ProjectDirs)) {
		return nil
	}
	grant := &CommandSessionGrant{Kind: "chrome_headless_screenshot", Executable: executable, CWD: cwd}
	argv[0] = executable
	seen := make(map[string]bool)
	headless := false
	for i := 1; i < len(argv); i++ {
		arg := argv[i]
		name, value, hasValue := strings.Cut(arg, "=")
		if seen[name] {
			return nil
		}
		seen[name] = true
		switch name {
		case "--headless":
			if hasValue && value != "new" {
				return nil
			}
			headless = true
		case "--disable-gpu", "--hide-scrollbars", "--no-sandbox", "--no-first-run", "--no-default-browser-check":
			if hasValue {
				return nil
			}
		case "--window-size":
			width, height, ok := strings.Cut(value, ",")
			w, wErr := strconv.Atoi(width)
			h, hErr := strconv.Atoi(height)
			if !ok || wErr != nil || hErr != nil || w < 1 || h < 1 || w > 8192 || h > 8192 {
				return nil
			}
			argv[i] = name + "=<viewport>"
		case "--screenshot", "--user-data-dir":
			if !hasValue || value == "" {
				return nil
			}
			target := commandGrantProjectPath(call.ProjectDirs, cwd, value, false)
			if target == "" {
				return nil
			}
			if name == "--screenshot" {
				if strings.ToLower(filepath.Ext(target)) != ".png" {
					return nil
				}
				grant.OutputDirectory = filepath.Dir(target)
				argv[i] = name + "=" + filepath.Join(grant.OutputDirectory, "<screenshot.png>")
			} else {
				for _, root := range normalizeProjectDirs(call.ProjectDirs) {
					resolved, err := filepath.EvalSymlinks(root)
					if err != nil || target == resolved {
						return nil
					}
				}
				grant.ProfilePath = target
				argv[i] = name + "=" + target
			}
		default:
			if strings.HasPrefix(arg, "-") || grant.InputPath != "" {
				return nil
			}
			if strings.HasPrefix(arg, "file:") {
				u, err := url.Parse(arg)
				if err != nil || u.Host != "" || u.RawQuery != "" || u.Fragment != "" {
					return nil
				}
				arg = u.Path
			} else if strings.Contains(arg, "://") {
				return nil
			}
			grant.InputPath = commandGrantProjectPath(call.ProjectDirs, cwd, arg, true)
			if grant.InputPath == "" {
				return nil
			}
			argv[i] = grant.InputPath
		}
	}
	if !headless || grant.InputPath == "" || grant.OutputDirectory == "" || grant.ProfilePath == "" {
		return nil
	}
	// Environment and executable updates invalidate the lease. Project identity
	// is added by the engine; roots also bind temporary directory grants.
	data, _ := json.Marshal([]any{grant.Kind, argv, cwd, normalizeProjectDirs(call.ProjectDirs), env, info.Size(), info.ModTime().UnixNano(), info.Mode()})
	hash := sha256.Sum256(data)
	grant.Key = hex.EncodeToString(hash[:])
	return grant
}

func commandGrantProjectPath(roots []string, cwd, path string, requireFile bool) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	_, target, _, err := resolveProjectPath(roots, path, false, !requireFile)
	if err != nil {
		return ""
	}
	info, err := os.Lstat(target)
	if err == nil && (info.Mode()&os.ModeSymlink != 0 || (requireFile && !info.Mode().IsRegular())) {
		return ""
	}
	if requireFile && err != nil || err != nil && !os.IsNotExist(err) {
		return ""
	}
	return target
}

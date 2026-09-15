package tool

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CommandSessionGrant describes a deliberately narrow, user-selected lease.
// It is not an executable-prefix allowlist. Unknown flags/compound commands
// remain single-call approvals. Key is private to the approval engine.
type CommandSessionGrant struct {
	Key             string `json:"-"`
	Kind            string `json:"kind"`
	Executable      string `json:"executable"`
	CWD             string `json:"cwd"`
	InputPath       string `json:"inputPath"`
	OutputDirectory string `json:"outputDirectory"`
	ProfilePath     string `json:"profilePath"`
}

// CommandSessionGrantForCall recognizes only foreground Chrome screenshots of
// one local project page, using an explicit project-local browser profile.
// File names and viewport dimensions may vary; all other arguments are bound.
func CommandSessionGrantForCall(call Call) *CommandSessionGrant {
	if call.Name != CommandRun {
		return nil
	}
	args, err := decodeCommandRunArgs(call.Args)
	if err != nil || args.Execution != CommandExecutionHost || args.Background || args.TTY || len(args.Env) != 0 {
		return nil
	}
	analysis, err := analyzeShellCommand(args.Command)
	if err != nil || analysis.Dynamic || analysis.Background || len(analysis.Commands) != 1 || len(analysis.Redirections) != 0 {
		return nil
	}
	argv := append([]string(nil), analysis.Commands[0]...)
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
	env, err := commandEnvironment(nil)
	if err != nil {
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

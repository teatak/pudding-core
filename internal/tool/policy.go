package tool

import (
	"encoding/json"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

type RiskClass string

const (
	RiskClassRead        RiskClass = "read"
	RiskClassWrite       RiskClass = "write"
	RiskClassDestructive RiskClass = "destructive"
	RiskClassCommand     RiskClass = "command"
)

type ToolRisk struct {
	Class         RiskClass `json:"class"`
	Operation     string    `json:"operation"`
	Scope         string    `json:"scope"`
	Paths         []string  `json:"paths,omitempty"`
	Summary       string    `json:"summary"`
	LowRisk       bool      `json:"lowRisk,omitempty"`
	SandboxBypass bool      `json:"sandboxBypass,omitempty"`
}

func ClassifyToolCall(name string, raw json.RawMessage) (ToolRisk, bool) {
	return classifyToolCall(name, raw, nil)
}

// ClassifyToolCallForProject uses the authorized roots to distinguish an
// in-project absolute path from a real project escape.
func ClassifyToolCallForProject(name string, raw json.RawMessage, projectDirs []string) (ToolRisk, bool) {
	return classifyToolCall(name, raw, projectDirs)
}

func classifyToolCall(name string, raw json.RawMessage, projectDirs []string) (ToolRisk, bool) {
	if name == AppSave {
		request, err := decodeAppSaveRequest(raw)
		if err != nil {
			return ToolRisk{}, false
		}
		summary := "Create an installed App package."
		if request.Operation == "update" {
			summary = "Replace an installed App package."
		}
		return ToolRisk{
			Class:     RiskClassWrite,
			Operation: "app_save",
			Scope:     "app",
			Paths:     compactRiskPaths(request.AppID),
			Summary:   summary,
		}, true
	}
	if name == CodeSymbols || name == CodeDefinition || name == CodeReferences || name == CodeDiagnostics || name == CodeRename {
		return classifyCodeReadCall(name, raw)
	}
	if name == CommandRun {
		return classifyCommandCall(raw, projectDirs)
	}
	if name == GitStatus || name == GitDiff || name == GitLog {
		return classifyGitReadCall(name, raw)
	}
	if name == GitStage || name == GitUnstage || name == GitCommit {
		return classifyGitWriteCall(name, raw)
	}
	if name == FilePatch {
		args, argumentErr := decodeFilePatchArgs(raw)
		if argumentErr != nil || strings.TrimSpace(args.Scope) != managedScopeProject || len(args.Files) == 0 || len(args.Files) > patchMaxFiles {
			return ToolRisk{}, false
		}
		paths := make([]string, 0, len(args.Files))
		destructive := false
		for _, file := range args.Files {
			path := strings.TrimSpace(file.Path)
			if path == "" {
				return ToolRisk{}, false
			}
			paths = append(paths, path)
			destructive = destructive || file.Delete
		}
		if destructive {
			return ToolRisk{
				Class:     RiskClassDestructive,
				Operation: "file_patch",
				Scope:     managedScopeProject,
				Paths:     compactRiskPaths(paths...),
				Summary:   "Apply a multi-file patch that deletes project files.",
			}, true
		}
		return ToolRisk{
			Class:     RiskClassWrite,
			Operation: "file_patch",
			Scope:     managedScopeProject,
			Paths:     compactRiskPaths(paths...),
			Summary:   "Apply a multi-file patch to project files.",
			LowRisk:   true,
		}, true
	}
	var args struct {
		Scope         string `json:"scope"`
		Path          string `json:"path"`
		AttachmentKey string `json:"attachmentKey"`
		FromPath      string `json:"from_path"`
		ToPath        string `json:"to_path"`
		Recursive     bool   `json:"recursive"`
		Overwrite     bool   `json:"overwrite"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return ToolRisk{}, false
	}
	args.Scope = strings.TrimSpace(args.Scope)
	if !isProjectFileScope(args.Scope) {
		return ToolRisk{}, false
	}
	args.Scope = managedScopeProject
	switch name {
	case AttachmentExport:
		path := strings.TrimSpace(args.Path)
		if path == "" || strings.TrimSpace(args.AttachmentKey) == "" {
			return ToolRisk{}, false
		}
		summary := "Export a session attachment to a project file."
		if args.Overwrite {
			summary = "Export a session attachment and overwrite the destination project file."
		}
		return ToolRisk{Class: RiskClassWrite, Operation: "attachment_export", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: summary, LowRisk: true}, true
	case FileWrite:
		path := strings.TrimSpace(args.Path)
		return ToolRisk{Class: RiskClassWrite, Operation: "write", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: "Overwrite a project file.", LowRisk: true}, true
	case FileDelete:
		path := strings.TrimSpace(args.Path)
		summary := "Delete a project file."
		if args.Recursive {
			summary = "Delete a project path recursively."
		}
		return ToolRisk{Class: RiskClassDestructive, Operation: "delete", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: summary}, true
	case FileMove:
		return ToolRisk{Class: RiskClassWrite, Operation: "move", Scope: args.Scope, Paths: compactRiskPaths(args.FromPath, args.ToPath), Summary: "Move or rename a project path.", LowRisk: true}, true
	case FileCopy:
		summary := "Copy a project path."
		if args.Overwrite {
			summary = "Copy a project path and overwrite the destination if it exists."
		}
		return ToolRisk{Class: RiskClassWrite, Operation: "copy", Scope: args.Scope, Paths: compactRiskPaths(args.FromPath, args.ToPath), Summary: summary, LowRisk: true}, true
	default:
		return ToolRisk{}, false
	}
}

func classifyCodeReadCall(name string, raw json.RawMessage) (ToolRisk, bool) {
	var args struct {
		Scope string   `json:"scope"`
		Path  string   `json:"path"`
		Paths []string `json:"paths"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.Scope) != managedScopeProject {
		return ToolRisk{}, false
	}
	paths := args.Paths
	if path := strings.TrimSpace(args.Path); path != "" {
		paths = append(paths, path)
	}
	if name == CodeRename {
		return ToolRisk{
			Class:     RiskClassWrite,
			Operation: "code_rename",
			Scope:     managedScopeProject,
			Paths:     compactRiskPaths(paths...),
			Summary:   "Rename a project symbol and update its references.",
			LowRisk:   true,
		}, true
	}
	return ToolRisk{
		Class:     RiskClassRead,
		Operation: strings.TrimPrefix(name, "builtin_"),
		Scope:     managedScopeProject,
		Paths:     compactRiskPaths(paths...),
		Summary:   "Read semantic code information from the project language server.",
		LowRisk:   true,
	}, true
}

func classifyGitWriteCall(name string, raw json.RawMessage) (ToolRisk, bool) {
	operation := "git_commit"
	summary := "Create a Git commit from the reviewed staged changes."
	paths := []string(nil)
	if name == GitStage || name == GitUnstage {
		action := "stage"
		operation = "git_stage"
		summary = "Stage explicit project files in Git."
		if name == GitUnstage {
			action = "unstage"
			operation = "git_unstage"
			summary = "Unstage explicit project files without changing the worktree."
		}
		args, err := decodeGitPathsArgs(raw, action)
		if err != nil {
			return ToolRisk{}, false
		}
		paths = compactRiskPaths(args.Paths...)
	} else if _, err := decodeGitCommitArgs(raw); err != nil {
		return ToolRisk{}, false
	}
	return ToolRisk{
		Class:     RiskClassWrite,
		Operation: operation,
		Scope:     managedScopeProject,
		Paths:     paths,
		Summary:   summary,
	}, true
}

func classifyGitReadCall(name string, raw json.RawMessage) (ToolRisk, bool) {
	var args gitBaseArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.Scope) != managedScopeProject {
		return ToolRisk{}, false
	}
	operation := strings.TrimPrefix(name, "builtin_")
	summary := "Read Git repository data."
	switch name {
	case GitStatus:
		summary = "Read Git worktree status."
	case GitDiff:
		summary = "Read Git changes."
	case GitLog:
		summary = "Read Git commit history."
	}
	return ToolRisk{
		Class:     RiskClassRead,
		Operation: operation,
		Scope:     managedScopeProject,
		Paths:     compactRiskPaths(args.CWD),
		Summary:   summary,
		LowRisk:   true,
	}, true
}

func classifyCommandCall(raw json.RawMessage, projectDirs []string) (ToolRisk, bool) {
	var args commandRunArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.Scope) != managedScopeProject || validateCommandInput(args) != nil {
		return ToolRisk{}, false
	}
	analysis, err := analyzeShellCommand(args.Command)
	if err != nil {
		return ToolRisk{}, false
	}
	operation := "shell"
	if len(analysis.Commands) == 1 {
		operation = commandOperation(analysis.Commands[0][0])
	}
	lowRisk := !analysis.Dynamic && len(analysis.Commands) > 0
	risk := ToolRisk{
		Class:     RiskClassCommand,
		Operation: operation,
		Scope:     managedScopeProject,
		Paths:     compactRiskPaths(args.CWD),
		Summary:   "Run project command: " + compactShellCommand(args.Command),
		LowRisk:   lowRisk,
	}
	for _, argv := range analysis.Commands {
		commandOperation := commandOperation(argv[0])
		executableAllowed := commandExecutableAllowedForAuto(argv[0], args.CWD, projectDirs)
		argsEscapeProject := commandPathArgsEscapeProject(argv, args.CWD, projectDirs)
		commandLowRisk := executableAllowed &&
			!commandRequiresApproval(argv) &&
			!argsEscapeProject
		risk.LowRisk = risk.LowRisk && commandLowRisk
		risk.SandboxBypass = risk.SandboxBypass ||
			!executableAllowed ||
			argsEscapeProject ||
			commandNeedsHostAccess(argv)
		if isDestructiveCommand(commandOperation) {
			risk.Class = RiskClassDestructive
			risk.LowRisk = false
		}
	}
	if !commandRedirectionsInsideProject(analysis.Redirections, args.CWD, projectDirs) {
		risk.LowRisk = false
		risk.SandboxBypass = true
	}
	if len(args.Env) > 0 {
		risk.Summary = "Run project command with custom environment: " + compactShellCommand(args.Command)
		if commandEnvironmentRequiresApproval(args.Env) {
			risk.LowRisk = false
		}
		if commandEnvironmentNeedsHostAccess(args.Env, args.CWD, projectDirs) {
			risk.SandboxBypass = true
		}
	}
	if args.Background {
		risk.Operation = "process_start"
		risk.Summary = "Start background project command: " + compactShellCommand(args.Command)
		if args.TTY {
			risk.Summary = "Start interactive project command: " + compactShellCommand(args.Command)
		}
	}
	if risk.Class == RiskClassDestructive {
		risk.Summary = "Run destructive project command: " + compactShellCommand(args.Command)
	}
	return risk, true
}

func compactShellCommand(command string) string {
	return compactCommand([]string{strings.Join(strings.Fields(command), " ")})
}

func commandRedirectionsInsideProject(redirections []shellRedirection, cwd string, projectDirs []string) bool {
	roots := normalizeProjectDirs(projectDirs)
	resolvedCWD := ""
	if len(roots) > 0 {
		_, resolved, _, err := resolveProjectPath(roots, cwd, true, false)
		if err != nil {
			return false
		}
		resolvedCWD = resolved
	}
	for _, redirection := range redirections {
		path := strings.TrimSpace(redirection.Path)
		if isSafeDeviceRedirection(path) {
			continue
		}
		if path == "" {
			return false
		}
		if len(roots) == 0 {
			cleaned := filepath.Clean(path)
			if filepath.IsAbs(path) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
				return false
			}
			continue
		}
		if !commandPathInsideProject(path, resolvedCWD, roots) {
			return false
		}
	}
	return true
}

func isSafeDeviceRedirection(path string) bool {
	switch filepath.Clean(strings.TrimSpace(path)) {
	case "/dev/null", "/dev/stdin", "/dev/stdout", "/dev/stderr":
		return true
	default:
		return false
	}
}

func commandPathArgsEscapeProject(argv []string, cwd string, projectDirs []string) bool {
	if len(argv) == 0 {
		return false
	}
	return commandPathsEscapeProject(commandPathArgs(argv), cwd, projectDirs)
}

func commandPathsEscapeProject(paths []string, cwd string, projectDirs []string) bool {
	roots := normalizeProjectDirs(projectDirs)
	resolvedCWD := ""
	if len(roots) > 0 {
		_, resolvedCWD, _, _ = resolveProjectPath(roots, cwd, true, false)
	}
	for _, path := range paths {
		candidate := strings.TrimSpace(path)
		if index := strings.IndexByte(candidate, '='); index >= 0 {
			candidate = candidate[index+1:]
		}
		candidate = strings.Trim(candidate, "\"'")
		if candidate == "" {
			continue
		}
		if sandboxManagedPath(candidate) {
			continue
		}
		cleaned := filepath.Clean(candidate)
		if filepath.IsAbs(candidate) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			if resolvedCWD == "" || !commandPathInsideProject(candidate, resolvedCWD, roots) {
				return true
			}
		}
	}
	return false
}

func commandPathArgs(argv []string) []string {
	if len(argv) < 2 {
		return nil
	}
	operation := commandOperation(argv[0])
	args := argv[1:]
	switch operation {
	case "mkdir", "touch", "cp", "mv", "rm", "rmdir", "unlink", "shred", "truncate",
		"cat", "ls", "stat", "wc", "du", "file", "readlink", "realpath":
		return commandNonOptionArgs(args, 0)
	case "chmod", "chown", "chgrp":
		return commandNonOptionArgs(args, 1)
	case "dd":
		var paths []string
		for _, arg := range args {
			if strings.HasPrefix(arg, "if=") || strings.HasPrefix(arg, "of=") {
				paths = append(paths, arg)
			}
		}
		return paths
	case "find":
		var paths []string
		for _, arg := range args {
			arg = strings.TrimSpace(arg)
			if arg == "" {
				continue
			}
			if strings.HasPrefix(arg, "-") || arg == "!" || arg == "(" {
				break
			}
			paths = append(paths, arg)
		}
		return paths
	case "python", "python3", "py":
		return commandScriptPath(args, "-c", "-m")
	case "node":
		return commandScriptPath(args, "-e", "--eval", "-p", "--print")
	case "deno":
		if commandSubcommand(args) == "eval" {
			return nil
		}
		return commandScriptPath(args, "-e", "--eval")
	case "ruby", "perl", "php", "lua", "luajit", "julia", "elixir", "swift", "r", "rscript":
		return commandScriptPath(args, "-e", "-r", "--eval")
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		return commandScriptPath(args, "-c", "-lc")
	case "git":
		var paths []string
		for index := 0; index < len(args); index++ {
			arg := strings.TrimSpace(args[index])
			switch {
			case arg == "-C" && index+1 < len(args):
				index++
				paths = append(paths, args[index])
			case strings.HasPrefix(arg, "--git-dir="), strings.HasPrefix(arg, "--work-tree="):
				paths = append(paths, arg)
			}
		}
		return paths
	case "go":
		subcommand := commandSubcommand(args)
		switch subcommand {
		case "build", "clean", "generate", "install", "list", "run", "test", "vet":
			for index, arg := range args {
				if strings.EqualFold(strings.TrimSpace(arg), subcommand) {
					return commandGoPathArgs(args[index+1:])
				}
			}
		}
		return nil
	default:
		return nil
	}
}

func commandNonOptionArgs(args []string, skip int) []string {
	paths := make([]string, 0, len(args))
	optionsEnded := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		if !optionsEnded && arg == "--" {
			optionsEnded = true
			continue
		}
		if !optionsEnded && strings.HasPrefix(arg, "-") {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		paths = append(paths, arg)
	}
	return paths
}

func commandGoPathArgs(args []string) []string {
	var paths []string
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		if arg == "" {
			continue
		}
		if arg == "--" {
			return append(paths, args[index+1:]...)
		}
		if strings.HasPrefix(arg, "-") {
			if !strings.Contains(arg, "=") && !containsAnyArg([]string{arg},
				"-a", "-asan", "-cover", "-failfast", "-json", "-msan", "-n",
				"-race", "-short", "-v", "-work", "-x") {
				index++
			}
			continue
		}
		paths = append(paths, arg)
	}
	return paths
}

func commandScriptPath(args []string, inlineOptions ...string) []string {
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		if arg == "" {
			continue
		}
		if containsAnyArg([]string{arg}, inlineOptions...) || containsAttachedShortOption([]string{arg}, inlineOptions...) {
			return nil
		}
		if arg == "--" {
			if index+1 < len(args) {
				return []string{args[index+1]}
			}
			return nil
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		return []string{arg}
	}
	return nil
}

func commandPathInsideProject(rawPath, resolvedCWD string, projectDirs []string) bool {
	target := rawPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(resolvedCWD, target)
	}
	_, _, _, err := resolveProjectPath(projectDirs, target, true, true)
	return err == nil
}

func commandOperation(executable string) string {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(executable)))
	return strings.TrimSuffix(name, ".exe")
}

func isDestructiveCommand(operation string) bool {
	switch operation {
	case "rm", "rmdir", "unlink", "shred", "truncate", "dd", "mkfs", "diskutil", "format", "del", "erase", "shutdown", "reboot", "kill", "pkill", "taskkill":
		return true
	default:
		return false
	}
}

func commandRequiresApproval(argv []string) bool {
	if len(argv) == 0 {
		return true
	}
	operation := commandOperation(argv[0])
	args := argv[1:]
	if isDestructiveCommand(operation) || isAlwaysRiskyCommand(operation) || commandRequestsWildcardBind(args) {
		return true
	}
	switch operation {
	case "git":
		return !isLowRiskGitCommand(args)
	case "find":
		return !isReadOnlyFind(args)
	case "fd", "fdfind":
		return containsAnyArg(args, "-x", "-X") || containsArgPrefix(args, "--exec", "--exec-batch")
	case "rg":
		return containsArgPrefix(args, "--pre", "--hostname-bin")
	case "go":
		return commandSubcommand(args) == "env" && containsAnyArg(args, "-w", "-u")
	case "npm", "pnpm", "yarn", "bun", "cargo":
		return commandHasPublishingArgument(args)
	case "gem":
		return commandSubcommand(args) == "push"
	case "twine":
		return commandSubcommand(args) == "upload"
	case "curl", "wget":
		return !commandUsesOnlyLoopbackURLs(args)
	case "sh", "bash", "zsh", "dash", "ksh", "fish", "awk", "gawk", "mawk", "nawk", "powershell", "pwsh", "cmd":
		return commandUsesInlineCode(operation, args)
	default:
		return false
	}
}

func commandNeedsHostAccess(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	operation := commandOperation(argv[0])
	args := argv[1:]
	switch operation {
	case "sudo", "doas", "su", "launchctl", "systemsetup", "networksetup", "scutil", "csrutil", "nvram",
		"mount", "umount", "hdiutil", "diskutil", "mkfs", "security", "ssh-add", "gpg", "pass",
		"open", "osascript", "pbcopy", "pbpaste", "ssh", "scp", "sftp", "ftp",
		"gh", "glab", "docker", "podman", "kubectl", "helm", "terraform", "tofu", "ansible", "ansible-playbook",
		"brew", "port":
		return true
	case "git":
		switch commandSubcommand(args) {
		case "push", "credential", "credential-cache", "credential-store":
			return true
		}
	case "npm", "pnpm", "yarn", "bun", "cargo":
		return commandHasPublishingArgument(args)
	case "gem":
		return commandSubcommand(args) == "push"
	case "twine":
		return commandSubcommand(args) == "upload"
	}
	return false
}

func commandExecutableAllowedForAuto(executable, cwd string, projectDirs []string) bool {
	if isBareCommand(executable) {
		return true
	}
	roots := normalizeProjectDirs(projectDirs)
	if len(roots) > 0 {
		_, resolvedCWD, _, err := resolveProjectPath(roots, cwd, true, false)
		if err == nil && commandPathInsideProject(executable, resolvedCWD, roots) {
			return true
		}
	}
	if !filepath.IsAbs(executable) {
		return false
	}
	cleaned := filepath.Clean(executable)
	for _, root := range []string{"/bin", "/sbin", "/usr/bin", "/usr/sbin", "/usr/local/bin", "/opt/homebrew/bin"} {
		if pathInsideRoot(cleaned, root) {
			return true
		}
	}
	return false
}

func isAlwaysRiskyCommand(operation string) bool {
	switch operation {
	case "sudo", "doas", "su", "launchctl", "systemsetup", "networksetup", "scutil", "csrutil", "nvram",
		"mount", "umount", "hdiutil", "chmod", "chown", "chgrp", "security", "ssh-add", "gpg", "pass",
		"open", "osascript", "pbcopy", "pbpaste", "ssh", "scp", "sftp", "ftp", "nc", "ncat", "socat", "rsync",
		"gh", "glab", "docker", "podman", "kubectl", "helm", "terraform", "tofu", "ansible", "ansible-playbook",
		"brew", "port", "xargs", "parallel", "env", "nice", "nohup", "time", "command", "arch", "caffeinate",
		"script", "timeout", "gtimeout", "xcrun", "setsid", "stdbuf", "unbuffer", "daemon", "chronic", "chpst",
		"ionice", "taskset", "watch", "busybox", "toybox":
		return true
	default:
		return false
	}
}

func commandEnvironmentRequiresApproval(env map[string]string) bool {
	for key, value := range env {
		key = strings.ToUpper(strings.TrimSpace(key))
		if commandBindEnvironmentKey(key) && isWildcardBindAddress(value) {
			return true
		}
		switch key {
		case "PATH", "HOME", "SHELL", "BASH_ENV", "ENV", "ZDOTDIR", "PYTHONPATH", "NODE_OPTIONS", "RUBYOPT", "PERL5OPT", "SSH_AUTH_SOCK":
			return true
		case "CC", "CXX", "CPP", "LD", "AR", "RANLIB", "STRIP", "OBJCOPY", "OBJDUMP", "NM", "PKG_CONFIG",
			"RUSTC", "RUSTDOC", "RUSTC_WRAPPER", "RUSTC_WORKSPACE_WRAPPER", "CARGO_BUILD_RUSTC_WRAPPER",
			"RUSTFLAGS", "RUSTDOCFLAGS", "CARGO_ENCODED_RUSTFLAGS", "CARGO_HOME", "RUSTUP_HOME",
			"GOENV", "GOROOT", "JAVA_HOME", "JAVA_TOOL_OPTIONS", "_JAVA_OPTIONS", "JDK_JAVA_OPTIONS", "CLASSPATH",
			"MAKEFLAGS", "MFLAGS", "MAKEFILES", "CMAKE_TOOLCHAIN_FILE", "CMAKE_PROJECT_INCLUDE", "CMAKE_PROJECT_INCLUDE_BEFORE", "CMAKE_MAKE_PROGRAM",
			"NPM_CONFIG_SCRIPT_SHELL", "NPM_CONFIG_USERCONFIG", "YARN_RC_FILENAME", "PNPM_HOME", "BUN_INSTALL",
			"PYTHONSTARTUP", "PYTHONHOME", "PIP_CONFIG_FILE", "VIRTUAL_ENV",
			"RUBYLIB", "GEM_HOME", "GEM_PATH", "BUNDLE_GEMFILE", "PERL5LIB", "LUA_PATH", "LUA_CPATH", "PHPRC", "PHP_INI_SCAN_DIR":
			return true
		}
		if strings.HasPrefix(key, "CARGO_TARGET_") &&
			(strings.HasSuffix(key, "_LINKER") || strings.HasSuffix(key, "_RUNNER") || strings.HasSuffix(key, "_RUSTFLAGS")) {
			return true
		}
		if strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "GIT_") {
			return true
		}
		if key == "GOFLAGS" && strings.Contains(strings.ToLower(value), "-toolexec") {
			return true
		}
		switch key {
		case "PAGER", "MANPAGER", "LESSOPEN", "EDITOR", "VISUAL":
			return true
		}
	}
	return false
}

func commandEnvironmentNeedsHostAccess(env map[string]string, cwd string, projectDirs []string) bool {
	for key, value := range env {
		key = strings.ToUpper(strings.TrimSpace(key))
		var candidates []string
		switch key {
		case "PATH":
			candidates = filepath.SplitList(value)
		case "HOME", "SSH_AUTH_SOCK", "BASH_ENV", "ENV", "ZDOTDIR", "PYTHONPATH",
			"PYTHONSTARTUP", "PYTHONHOME", "PIP_CONFIG_FILE", "VIRTUAL_ENV",
			"GOENV", "GOROOT", "JAVA_HOME", "CLASSPATH", "CARGO_HOME", "RUSTUP_HOME",
			"CC", "CXX", "CPP", "LD", "AR", "RANLIB", "STRIP", "OBJCOPY", "OBJDUMP",
			"NM", "PKG_CONFIG", "RUSTC", "RUSTDOC", "RUSTC_WRAPPER",
			"RUSTC_WORKSPACE_WRAPPER", "CARGO_BUILD_RUSTC_WRAPPER",
			"CMAKE_TOOLCHAIN_FILE", "CMAKE_PROJECT_INCLUDE", "CMAKE_PROJECT_INCLUDE_BEFORE",
			"NPM_CONFIG_SCRIPT_SHELL", "NPM_CONFIG_USERCONFIG", "YARN_RC_FILENAME",
			"NODE_OPTIONS", "RUBYOPT", "PERL5OPT",
			"RUBYLIB", "GEM_HOME", "GEM_PATH", "BUNDLE_GEMFILE", "PERL5LIB",
			"LUA_PATH", "LUA_CPATH", "PHPRC", "PHP_INI_SCAN_DIR":
			candidates = filepath.SplitList(value)
		default:
			if strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "GIT_") {
				candidates = filepath.SplitList(value)
			}
		}
		if commandPathsEscapeProject(candidates, cwd, projectDirs) {
			return true
		}
	}
	return false
}

func commandRequestsWildcardBind(args []string) bool {
	for index, arg := range args {
		arg = strings.TrimSpace(arg)
		lower := strings.ToLower(arg)
		switch lower {
		case "--host", "--hostname", "--bind", "--listen", "--listen-address":
			if index+1 < len(args) && isWildcardBindAddress(args[index+1]) {
				return true
			}
		}
		for _, prefix := range []string{"--host=", "--hostname=", "--bind=", "--listen=", "--listen-address="} {
			if strings.HasPrefix(lower, prefix) && isWildcardBindAddress(arg[len(prefix):]) {
				return true
			}
		}
		if isWildcardBindEndpoint(arg) {
			return true
		}
	}
	return false
}

func commandBindEnvironmentKey(key string) bool {
	switch key {
	case "HOST", "HOSTNAME", "BIND", "BIND_ADDRESS", "LISTEN", "LISTEN_ADDRESS":
		return true
	default:
		return strings.HasSuffix(key, "_HOST") || strings.HasSuffix(key, "_BIND_ADDRESS") || strings.HasSuffix(key, "_LISTEN_ADDRESS")
	}
}

func isWildcardBindAddress(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "\"'"))
	switch value {
	case "*", "0.0.0.0", "::", "[::]":
		return true
	}
	return strings.HasPrefix(value, "*:") || strings.HasPrefix(value, "0.0.0.0:") || strings.HasPrefix(value, "[::]:")
}

func isWildcardBindEndpoint(value string) bool {
	value = strings.ToLower(strings.Trim(strings.TrimSpace(value), "\"'"))
	return strings.HasPrefix(value, "*:") || strings.HasPrefix(value, "0.0.0.0:") || strings.HasPrefix(value, "[::]:")
}

func commandSubcommand(args []string) string {
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		return arg
	}
	return ""
}

func commandHasPublishingArgument(args []string) bool {
	for _, arg := range args {
		switch strings.ToLower(strings.TrimSpace(arg)) {
		case "publish", "unpublish", "login", "logout", "owner", "token", "yank":
			return true
		}
	}
	return false
}

func commandUsesInlineCode(operation string, args []string) bool {
	switch operation {
	case "sh", "bash", "zsh", "dash", "ksh", "fish":
		return containsAnyArg(args, "-c", "-lc") || containsAttachedShortOption(args, "-c", "-lc")
	case "python", "python3", "py":
		return containsAnyArg(args, "-c") || containsAttachedShortOption(args, "-c")
	case "node", "deno":
		return containsAnyArg(args, "-e", "--eval", "-p", "--print") ||
			containsArgPrefix(args, "-e", "--eval", "-p", "--print") ||
			containsAttachedShortOption(args, "-e", "-p") ||
			commandSubcommand(args) == "eval"
	case "ruby", "perl", "php", "lua", "luajit", "julia", "elixir", "swift", "r", "rscript":
		return containsAnyArg(args, "-e", "-r", "--eval") ||
			containsArgPrefix(args, "--eval") ||
			containsAttachedShortOption(args, "-e", "-r")
	case "awk", "gawk", "mawk", "nawk":
		return !containsAnyArg(args, "-f", "--file") && !containsArgPrefix(args, "--file")
	case "powershell", "pwsh":
		return containsAnyArg(args, "-c", "-command", "-encodedcommand") ||
			containsAttachedShortOption(args, "-c", "-command", "-encodedcommand")
	case "cmd":
		return containsAnyArg(args, "/c", "/k") || containsAttachedShortOption(args, "/c", "/k")
	default:
		return false
	}
}

func sandboxManagedPath(path string) bool {
	path = strings.Trim(strings.TrimSpace(path), "\"'")
	for _, variable := range []string{
		"$TMPDIR",
		"$TMP",
		"$TEMP",
		"$XDG_CACHE_HOME",
		"$GOCACHE",
		"$GOMODCACHE",
		"$GOPATH",
		"$NPM_CONFIG_CACHE",
		"$NPM_CONFIG_PREFIX",
		"$NPM_CONFIG_USERCONFIG",
		"$NPM_CONFIG_GLOBALCONFIG",
		"$PNPM_HOME",
		"$COREPACK_HOME",
		"$NODE_REPL_HISTORY",
		"$NODE_PATH",
		"$npm_config_store_dir",
		"$YARN_CACHE_FOLDER",
		"$YARN_GLOBAL_FOLDER",
		"$PIP_CACHE_DIR",
		"$UV_CACHE_DIR",
		"$PYTHONUSERBASE",
		"$PYTHONPYCACHEPREFIX",
	} {
		if path == variable || strings.HasPrefix(path, variable+"/") {
			return true
		}
	}
	return false
}

func containsAttachedShortOption(args []string, options ...string) bool {
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		for _, option := range options {
			option = strings.ToLower(option)
			if len(arg) > len(option) && strings.HasPrefix(arg, option) {
				return true
			}
		}
	}
	return false
}

func commandUsesOnlyLoopbackURLs(args []string) bool {
	found := false
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		host := ""
		if parsed, err := url.Parse(arg); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		} else {
			candidate := strings.TrimPrefix(arg, "//")
			candidate = strings.SplitN(candidate, "/", 2)[0]
			if parsedHost, _, err := net.SplitHostPort(candidate); err == nil {
				host = strings.Trim(parsedHost, "[]")
			} else if strings.EqualFold(candidate, "localhost") {
				host = candidate
			}
		}
		if host == "" {
			continue
		}
		found = true
		if !strings.EqualFold(host, "localhost") {
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsLoopback() {
				return false
			}
		}
	}
	return found
}

func isBareCommand(executable string) bool {
	executable = strings.TrimSpace(executable)
	return executable != "" && executable == filepath.Base(executable) && !strings.ContainsAny(executable, `/\\`)
}

func isLowRiskGitCommand(args []string) bool {
	for len(args) > 0 {
		rawArg := strings.TrimSpace(args[0])
		arg := strings.ToLower(rawArg)
		switch {
		case rawArg == "-C" || arg == "--git-dir" || arg == "--work-tree" || arg == "--namespace":
			if len(args) < 2 {
				return false
			}
			args = args[2:]
		case rawArg == "-c":
			return false
		case strings.HasPrefix(arg, "--git-dir=") || strings.HasPrefix(arg, "--work-tree=") || strings.HasPrefix(arg, "--namespace="):
			args = args[1:]
		case arg == "--no-pager" || arg == "--paginate" || arg == "--literal-pathspecs" || arg == "--no-literal-pathspecs" || arg == "--no-optional-locks":
			args = args[1:]
		case strings.HasPrefix(arg, "-"):
			return false
		default:
			switch arg {
			case "status", "diff", "log", "show", "rev-parse", "ls-files", "grep", "blame", "describe", "shortlog", "ls-remote":
				return true
			case "clone", "fetch":
				return !gitArgsRequireApproval(arg, args[1:])
			case "pull":
				return !gitArgsRequireApproval(arg, args[1:])
			default:
				return false
			}
		}
	}
	return false
}

func gitArgsRequireApproval(command string, args []string) bool {
	for _, rawArg := range args {
		rawArg = strings.TrimSpace(rawArg)
		arg := strings.ToLower(rawArg)
		switch command {
		case "clone", "fetch":
			if arg == "-u" || strings.HasPrefix(arg, "-u") || arg == "--upload-pack" || strings.HasPrefix(arg, "--upload-pack=") ||
				arg == "-c" || strings.HasPrefix(arg, "-c") || arg == "--config" || strings.HasPrefix(arg, "--config=") {
				return true
			}
		case "pull":
			if rawArg == "-x" || strings.HasPrefix(rawArg, "-x") || arg == "--exec" || strings.HasPrefix(arg, "--exec=") ||
				arg == "--upload-pack" || strings.HasPrefix(arg, "--upload-pack=") {
				return true
			}
		}
	}
	return false
}

func containsArgPrefix(args []string, prefixes ...string) bool {
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		for _, prefix := range prefixes {
			if arg == strings.ToLower(prefix) || strings.HasPrefix(arg, strings.ToLower(prefix)+"=") {
				return true
			}
		}
	}
	return false
}

func isReadOnlyFind(args []string) bool {
	for _, arg := range args {
		arg = strings.ToLower(strings.TrimSpace(arg))
		switch arg {
		case "-delete", "-exec", "-execdir", "-ok", "-okdir", "-fls":
			return false
		}
		if strings.HasPrefix(arg, "-fprint") {
			return false
		}
	}
	return true
}

func containsAnyArg(args []string, values ...string) bool {
	for _, arg := range args {
		for _, value := range values {
			if strings.EqualFold(strings.TrimSpace(arg), value) {
				return true
			}
		}
	}
	return false
}

func compactCommand(argv []string) string {
	const maxRunes = 240
	text := []rune(strings.Join(argv, " "))
	if len(text) <= maxRunes {
		return string(text)
	}
	return string(text[:maxRunes]) + "..."
}

func compactRiskPaths(paths ...string) []string {
	out := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

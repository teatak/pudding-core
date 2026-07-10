package tool

import (
	"encoding/json"
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
	Class     RiskClass `json:"class"`
	Operation string    `json:"operation"`
	Scope     string    `json:"scope"`
	Paths     []string  `json:"paths,omitempty"`
	Summary   string    `json:"summary"`
	LowRisk   bool      `json:"lowRisk,omitempty"`
}

func ClassifyToolCall(name string, raw json.RawMessage) (ToolRisk, bool) {
	if name == CodeSymbols || name == CodeDefinition || name == CodeReferences || name == CodeDiagnostics || name == CodeRename {
		return classifyCodeReadCall(name, raw)
	}
	if name == CommandRun {
		return classifyCommandCall(raw)
	}
	if name == GitStatus || name == GitDiff || name == GitLog {
		return classifyGitReadCall(name, raw)
	}
	if name == GitStage || name == GitUnstage || name == GitCommit {
		return classifyGitWriteCall(name, raw)
	}
	if name == PatchApply {
		var args patchApplyArgs
		if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.ProposalID) == "" {
			return ToolRisk{}, false
		}
		return ToolRisk{
			Class:     RiskClassWrite,
			Operation: "patch_apply",
			Scope:     managedScopeProject,
			Summary:   "Apply a reviewed patch proposal to project files.",
		}, true
	}
	var args struct {
		Scope     string `json:"scope"`
		Path      string `json:"path"`
		FromPath  string `json:"from_path"`
		ToPath    string `json:"to_path"`
		Recursive bool   `json:"recursive"`
		Overwrite bool   `json:"overwrite"`
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
	case FileWrite:
		path := strings.TrimSpace(args.Path)
		return ToolRisk{Class: RiskClassWrite, Operation: "write", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: "Overwrite a project file."}, true
	case FilePatch:
		path := strings.TrimSpace(args.Path)
		return ToolRisk{Class: RiskClassWrite, Operation: "patch", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: "Modify a project file."}, true
	case FileDelete:
		path := strings.TrimSpace(args.Path)
		summary := "Delete a project file."
		if args.Recursive {
			summary = "Delete a project path recursively."
		}
		return ToolRisk{Class: RiskClassDestructive, Operation: "delete", Scope: args.Scope, Paths: compactRiskPaths(path), Summary: summary}, true
	case FileMove:
		return ToolRisk{Class: RiskClassWrite, Operation: "move", Scope: args.Scope, Paths: compactRiskPaths(args.FromPath, args.ToPath), Summary: "Move or rename a project path."}, true
	case FileCopy:
		summary := "Copy a project path."
		if args.Overwrite {
			summary = "Copy a project path and overwrite the destination if it exists."
		}
		return ToolRisk{Class: RiskClassWrite, Operation: "copy", Scope: args.Scope, Paths: compactRiskPaths(args.FromPath, args.ToPath), Summary: summary}, true
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
	summary := "Read semantic code information from the project language server."
	if name == CodeRename {
		summary = "Prepare a semantic rename proposal without changing project files."
	}
	return ToolRisk{
		Class:     RiskClassRead,
		Operation: strings.TrimPrefix(name, "builtin_"),
		Scope:     managedScopeProject,
		Paths:     compactRiskPaths(paths...),
		Summary:   summary,
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

func classifyCommandCall(raw json.RawMessage) (ToolRisk, bool) {
	var args commandRunArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil || strings.TrimSpace(args.Scope) != managedScopeProject || validateCommandArgv(args.Argv) != nil {
		return ToolRisk{}, false
	}
	operation := commandOperation(args.Argv[0])
	risk := ToolRisk{
		Class:     RiskClassCommand,
		Operation: operation,
		Scope:     managedScopeProject,
		Paths:     compactRiskPaths(args.CWD),
		Summary:   "Run project command: " + compactCommand(args.Argv),
		LowRisk:   isLowRiskCommand(args.Argv) && !commandArgsEscapeProject(args.Argv[1:]),
	}
	if len(args.Env) > 0 {
		risk.LowRisk = false
		risk.Summary = "Run project command with custom environment: " + compactCommand(args.Argv)
	}
	if isDestructiveCommand(operation) {
		risk.Class = RiskClassDestructive
		risk.LowRisk = false
		risk.Summary = "Run destructive project command: " + compactCommand(args.Argv)
	}
	return risk, true
}

func commandArgsEscapeProject(args []string) bool {
	for _, arg := range args {
		candidate := strings.TrimSpace(arg)
		if index := strings.IndexByte(candidate, '='); index >= 0 {
			candidate = candidate[index+1:]
		}
		candidate = strings.Trim(candidate, "\"'")
		if candidate == "" {
			continue
		}
		cleaned := filepath.Clean(candidate)
		if filepath.IsAbs(candidate) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func commandOperation(executable string) string {
	name := strings.ToLower(filepath.Base(strings.TrimSpace(executable)))
	return strings.TrimSuffix(name, ".exe")
}

func isDestructiveCommand(operation string) bool {
	switch operation {
	case "rm", "rmdir", "unlink", "shred", "dd", "mkfs", "diskutil", "format", "del", "erase", "shutdown", "reboot", "kill", "pkill", "taskkill":
		return true
	default:
		return false
	}
}

func isLowRiskCommand(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	op := commandOperation(argv[0])
	args := argv[1:]
	first := ""
	if len(args) > 0 {
		first = strings.ToLower(strings.TrimSpace(args[0]))
	}
	switch op {
	case "go":
		switch first {
		case "test", "vet", "build", "list", "version":
			return true
		case "env":
			return !containsAnyArg(args[1:], "-w", "-u")
		}
	case "npm", "pnpm", "yarn", "bun":
		if first == "test" {
			return true
		}
		if first == "run" && len(args) > 1 {
			return isVerificationTarget(args[1])
		}
	case "cargo":
		return first == "test" || first == "check" || first == "build" || first == "clippy"
	case "pytest":
		return true
	case "python", "python3", "py":
		return len(args) > 1 && first == "-m" && strings.EqualFold(args[1], "pytest")
	case "make", "gmake":
		return len(args) > 0 && isVerificationTarget(args[len(args)-1])
	case "mvn", "mvnw", "gradle", "gradlew":
		return containsVerificationTarget(args)
	case "dotnet":
		return first == "test" || first == "build"
	case "git":
		return first == "status" || first == "diff" || first == "log" || first == "show"
	}
	return false
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

func containsVerificationTarget(args []string) bool {
	for _, arg := range args {
		if isVerificationTarget(arg) {
			return true
		}
	}
	return false
}

func isVerificationTarget(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "test", "tests", "check", "lint", "build", "typecheck", "type-check", "verify":
		return true
	default:
		return false
	}
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

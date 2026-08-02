package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

const maxCommandMutationTargets = 256

// ProjectMutationTracking describes the exact project paths a tool call owns
// for mutation tracking.
type ProjectMutationTracking struct {
	Targets []string
	Origin  store.FileChangeOrigin
}

type mutationTrackingSink func(targets []string)

type mutationTrackingSinkContextKey struct{}

// WithMutationTrackingSink lets tools report an exact mutation scope once it
// becomes known during execution, before any filesystem writes occur.
func WithMutationTrackingSink(ctx context.Context, sink func(targets []string)) context.Context {
	if sink == nil {
		return ctx
	}
	return context.WithValue(ctx, mutationTrackingSinkContextKey{}, mutationTrackingSink(sink))
}

func reportMutationTracking(ctx context.Context, targets []string) {
	sink, _ := ctx.Value(mutationTrackingSinkContextKey{}).(mutationTrackingSink)
	if sink != nil && len(targets) > 0 {
		sink(targets)
	}
}

type mutationPathArgs struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

type mutationMoveArgs struct {
	Scope    string `json:"scope"`
	FromPath string `json:"from_path"`
	ToPath   string `json:"to_path"`
}

type mutationCopyArgs struct {
	Scope  string `json:"scope"`
	ToPath string `json:"to_path"`
}

// MutationTrackingForCall resolves only explicit project paths. Foreground
// commands are included when their write targets can be determined statically;
// opaque, root-wide, and background command effects are deliberately excluded.
func MutationTrackingForCall(call Call) (ProjectMutationTracking, bool) {
	switch call.Name {
	case CommandRun:
		return commandMutationTracking(call)
	case FilePatch:
		args, argumentErr := decodeFilePatchArgs(call.Args)
		if argumentErr != nil || strings.TrimSpace(args.Scope) != managedScopeProject || len(args.Files) == 0 {
			return ProjectMutationTracking{}, false
		}
		targets := make([]string, 0, len(args.Files))
		for _, file := range args.Files {
			target, ok := resolveMutationTarget(call.ProjectDirs, file.Path, true)
			if !ok {
				return ProjectMutationTracking{}, false
			}
			targets = append(targets, target)
		}
		return structuredMutationTracking(targets)
	case FileWrite, FileDelete, AttachmentExport:
		var args mutationPathArgs
		if !decodeProjectMutationArgs(call.Args, &args) {
			return ProjectMutationTracking{}, false
		}
		target, ok := resolveMutationTarget(call.ProjectDirs, args.Path, true)
		if !ok {
			return ProjectMutationTracking{}, false
		}
		return structuredMutationTracking([]string{target})
	case FileMove:
		var args mutationMoveArgs
		if !decodeProjectMutationArgs(call.Args, &args) {
			return ProjectMutationTracking{}, false
		}
		from, fromOK := resolveMutationTarget(call.ProjectDirs, args.FromPath, false)
		to, toOK := resolveMutationTarget(call.ProjectDirs, args.ToPath, true)
		if !fromOK || !toOK {
			return ProjectMutationTracking{}, false
		}
		return structuredMutationTracking([]string{from, to})
	case FileCopy:
		var args mutationCopyArgs
		if !decodeProjectMutationArgs(call.Args, &args) {
			return ProjectMutationTracking{}, false
		}
		target, ok := resolveMutationTarget(call.ProjectDirs, args.ToPath, true)
		if !ok {
			return ProjectMutationTracking{}, false
		}
		return structuredMutationTracking([]string{target})
	default:
		return ProjectMutationTracking{}, false
	}
}

func structuredMutationTracking(targets []string) (ProjectMutationTracking, bool) {
	if len(targets) == 0 {
		return ProjectMutationTracking{}, false
	}
	return ProjectMutationTracking{
		Targets: targets,
		Origin:  store.FileChangeOriginStructured,
	}, true
}

func commandMutationTracking(call Call) (ProjectMutationTracking, bool) {
	if runtime.GOOS == "windows" {
		// command_run uses PowerShell on Windows, while the static analyzer is
		// intentionally POSIX-shell based.
		return ProjectMutationTracking{}, false
	}
	args, err := decodeCommandRunArgs(call.Args)
	if err != nil || args.Background {
		return ProjectMutationTracking{}, false
	}
	cwd := strings.TrimSpace(args.CWD)
	if cwd == "" {
		cwd = "."
	}
	_, resolvedCWD, _, err := resolveProjectPath(call.ProjectDirs, cwd, true, false)
	if err != nil {
		return ProjectMutationTracking{}, false
	}
	analysis, err := analyzeShellCommand(args.Command)
	if err != nil || analysis.Background {
		return ProjectMutationTracking{}, false
	}

	rawTargets := make([]string, 0, len(analysis.Redirections))
	for _, redirect := range analysis.Redirections {
		if redirect.Writes && !isSafeDeviceRedirection(redirect.Path) {
			rawTargets = append(rawTargets, redirect.Path)
		}
	}
	if !analysis.Dynamic {
		for _, argv := range analysis.Commands {
			rawTargets = append(rawTargets, commandMutationPaths(argv)...)
		}
	}

	targets := make([]string, 0, len(rawTargets))
	seen := make(map[string]struct{}, len(rawTargets))
	for _, raw := range rawTargets {
		if len(targets) >= maxCommandMutationTargets {
			return ProjectMutationTracking{}, false
		}
		target, ok := resolveCommandMutationTarget(call.ProjectDirs, resolvedCWD, raw)
		if !ok {
			continue
		}
		if _, duplicate := seen[target]; duplicate {
			continue
		}
		seen[target] = struct{}{}
		targets = append(targets, target)
	}
	if len(targets) == 0 {
		return ProjectMutationTracking{}, false
	}
	sort.Strings(targets)
	return ProjectMutationTracking{
		Targets: targets,
		Origin:  store.FileChangeOriginCommandObserved,
	}, true
}

func resolveCommandMutationTarget(roots []string, cwd, raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.ContainsAny(raw, "$*?[") {
		return "", false
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(cwd, target)
	}
	_, resolved, relative, err := resolveProjectPath(roots, target, true, true)
	if err != nil || relative == "." {
		return "", false
	}
	return resolved, true
}

func commandMutationPaths(argv []string) []string {
	if len(argv) < 2 {
		return nil
	}
	operation := commandOperation(argv[0])
	args := argv[1:]
	switch operation {
	case "npx", "bunx":
		return wrappedCommandMutationPaths(args)
	case "npm", "pnpm", "yarn", "bun":
		return packageManagerExecMutationPaths(args)
	case "git":
		subcommand := commandSubcommand(args)
		for index, arg := range args {
			if strings.EqualFold(strings.TrimSpace(arg), subcommand) {
				switch subcommand {
				case "mv":
					return commandMutationPaths(append([]string{"mv"}, args[index+1:]...))
				case "rm":
					return commandMutationPaths(append([]string{"rm"}, args[index+1:]...))
				}
				break
			}
		}
	case "touch":
		return commandMutationOperands(args, map[string]bool{
			"-d": true, "--date": true, "-r": true, "--reference": true, "-t": true,
		})
	case "mkdir":
		return commandMutationOperands(args, map[string]bool{
			"-m": true, "--mode": true, "-Z": true, "--context": true,
		})
	case "rm", "rmdir", "unlink", "shred":
		return commandMutationOperands(args, nil)
	case "mv":
		if containsAnyArg(args, "-t", "--target-directory", "-b", "--backup", "-S", "--suffix") ||
			containsArgPrefix(args, "--target-directory", "--backup", "--suffix") {
			return nil
		}
		operands := commandMutationOperands(args, nil)
		if len(operands) < 2 {
			return nil
		}
		return operands
	case "cp":
		if containsAnyArg(args, "-t", "--target-directory", "--parents", "-b", "--backup", "-S", "--suffix") ||
			containsArgPrefix(args, "--target-directory", "--backup", "--suffix") {
			return nil
		}
		operands := commandMutationOperands(args, nil)
		if len(operands) < 2 {
			return nil
		}
		return operands[len(operands)-1:]
	case "tee":
		if containsAnyArg(args, "--output-error") {
			return nil
		}
		return commandMutationOperands(args, nil)
	case "dd":
		for _, arg := range args {
			if strings.HasPrefix(arg, "of=") && len(arg) > len("of=") {
				return []string{strings.TrimPrefix(arg, "of=")}
			}
		}
	case "gofmt", "goimports", "gofumpt":
		if !containsAnyArg(args, "-w") {
			return nil
		}
		return commandMutationOperands(args, map[string]bool{"-r": true})
	case "prettier":
		if !containsAnyArg(args, "--write") {
			return nil
		}
		return commandMutationOperands(args, prettierValueOptions)
	case "clang-format":
		if !containsAnyArg(args, "-i", "--in-place") {
			return nil
		}
		return commandMutationOperands(args, map[string]bool{
			"--assume-filename": true, "--fallback-style": true, "--style": true,
		})
	case "eslint":
		if !containsAnyArg(args, "--fix") {
			return nil
		}
		return commandMutationOperands(args, eslintValueOptions)
	}
	return nil
}

func wrappedCommandMutationPaths(args []string) []string {
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		switch {
		case arg == "":
			continue
		case arg == "--":
			if index+1 < len(args) {
				return commandMutationPaths(args[index+1:])
			}
			return nil
		case arg == "-y" || arg == "--yes" || arg == "--no-install" || arg == "--quiet":
			continue
		case arg == "-p" || arg == "--package":
			index++
			continue
		case strings.HasPrefix(arg, "--package="):
			continue
		case strings.HasPrefix(arg, "-"):
			return nil
		default:
			return commandMutationPaths(args[index:])
		}
	}
	return nil
}

func packageManagerExecMutationPaths(args []string) []string {
	if len(args) < 2 {
		return nil
	}
	subcommand := strings.ToLower(strings.TrimSpace(args[0]))
	if subcommand != "exec" && subcommand != "dlx" {
		return nil
	}
	command := args[1:]
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	return commandMutationPaths(command)
}

var prettierValueOptions = map[string]bool{
	"--config": true, "--config-precedence": true, "--cursor-offset": true,
	"--end-of-line": true, "--html-whitespace-sensitivity": true,
	"--ignore-path": true, "--insert-pragma": false, "--jsx-single-quote": false,
	"--log-level": true, "--parser": true, "--plugin": true,
	"--print-width": true, "--prose-wrap": true, "--quote-props": true,
	"--range-end": true, "--range-start": true, "--require-pragma": false,
	"--semi": false, "--single-attribute-per-line": false, "--single-quote": false,
	"--stdin-filepath": true, "--tab-width": true, "--trailing-comma": true,
	"--use-tabs": false, "--vue-indent-script-and-style": false,
}

var eslintValueOptions = map[string]bool{
	"--cache-location": true, "--config": true, "--env": true,
	"--ext": true, "--fix-type": true, "--format": true,
	"--global": true, "--ignore-pattern": true, "--max-warnings": true,
	"--output-file": true, "--parser": true, "--parser-options": true,
	"--plugin": true, "--resolve-plugins-relative-to": true, "--rule": true,
}

func commandMutationOperands(args []string, valueOptions map[string]bool) []string {
	operands := make([]string, 0, len(args))
	optionsEnded := false
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		if arg == "" {
			continue
		}
		if !optionsEnded && arg == "--" {
			optionsEnded = true
			continue
		}
		if !optionsEnded && strings.HasPrefix(arg, "-") {
			name := arg
			if equals := strings.IndexByte(name, '='); equals >= 0 {
				name = name[:equals]
			}
			if valueOptions[name] && !strings.ContainsRune(arg, '=') && index+1 < len(args) {
				index++
			}
			continue
		}
		operands = append(operands, arg)
	}
	return operands
}

func decodeProjectMutationArgs(raw json.RawMessage, dst any) bool {
	if len(raw) == 0 || json.Unmarshal(raw, dst) != nil {
		return false
	}
	scope, ok := mutationScope(dst)
	return ok && strings.TrimSpace(scope) == managedScopeProject
}

func mutationScope(value any) (string, bool) {
	switch args := value.(type) {
	case *mutationPathArgs:
		return args.Scope, true
	case *mutationMoveArgs:
		return args.Scope, true
	case *mutationCopyArgs:
		return args.Scope, true
	default:
		return "", false
	}
}

func resolveMutationTarget(roots []string, path string, allowMissing bool) (string, bool) {
	_, target, _, err := resolveProjectPath(roots, path, true, allowMissing)
	return target, err == nil
}

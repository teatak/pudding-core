package tool

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	commandMaxDiagnostics     = 200
	commandDiagnosticContext  = 2
	commandDiagnosticMaxRunes = 1000
)

type commandDiagnostic struct {
	Path          string `json:"path"`
	RelativePath  string `json:"relativePath"`
	Line          int    `json:"line"`
	Column        int    `json:"column,omitempty"`
	LineStart     int    `json:"lineStart"`
	LineEnd       int    `json:"lineEnd"`
	Severity      string `json:"severity"`
	Message       string `json:"message"`
	Code          string `json:"code,omitempty"`
	Source        string `json:"source,omitempty"`
	Excerpt       string `json:"excerpt,omitempty"`
	RawPath       string `json:"rawPath,omitempty"`
	Unresolved    bool   `json:"unresolved,omitempty"`
	inferSeverity bool
}

var (
	typeScriptDiagnosticPattern = regexp.MustCompile(`^(.+)\((\d+),(\d+)\):\s*(error|warning|info)\s+([A-Za-z]+\d+):\s*(.+)$`)
	pathColumnDiagnosticPattern = regexp.MustCompile(`^(.+):(\d+):(\d+):\s*(?:(error|warning|info)(?:\s+([A-Za-z][A-Za-z0-9_.-]*))?:\s*)?(.+)$`)
	pathLineDiagnosticPattern   = regexp.MustCompile(`^(.+):(\d+):\s*(?:(error|warning|info)(?:\s+([A-Za-z][A-Za-z0-9_.-]*))?:\s*)?(.+)$`)
	eslintDetailPattern         = regexp.MustCompile(`^\s*(\d+):(\d+)\s+(error|warning|info)\s+(.+?)(?:\s{2,}([@A-Za-z0-9_./-]+))?\s*$`)
)

func commandVerificationKind(argv []string) string {
	if len(argv) == 0 {
		return ""
	}
	op := commandOperation(argv[0])
	args := argv[1:]
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" || arg == "-h" || arg == "--version" {
			return ""
		}
	}
	first := lowerArg(args, 0)
	second := lowerArg(args, 1)
	switch op {
	case "go":
		switch first {
		case "test":
			return "test"
		case "vet":
			return "lint"
		case "build":
			return "build"
		}
	case "npm", "pnpm", "yarn", "bun":
		// These options can return success without running the requested script.
		for _, arg := range args {
			if arg == "--if-present" || arg == "--dry-run" || arg == "--help" || arg == "-h" {
				return ""
			}
		}
		args = packageManagerVerificationArgs(op, args)
		first, second = lowerArg(args, 0), lowerArg(args, 1)
		if first == "test" {
			return "test"
		}
		if first == "run" {
			return verificationTargetKind(second)
		}
		return verificationTargetKind(first)
	case "npx":
		if first == "--yes" || first == "-y" || first == "--no-install" {
			args = args[1:]
		}
		if len(args) == 0 || strings.HasPrefix(args[0], "-") {
			return ""
		}
		return commandVerificationKind(args)
	case "node":
		for _, arg := range args {
			if arg == "--test" {
				return "test"
			}
			if !strings.HasPrefix(arg, "-") || arg == "--" || arg == "-e" || arg == "--eval" || arg == "-p" || arg == "--print" {
				break
			}
		}
	case "cargo":
		switch first {
		case "test":
			return "test"
		case "clippy":
			return "lint"
		case "build":
			return "build"
		case "check":
			return "check"
		}
	case "pytest":
		return "test"
	case "python", "python3":
		if first == "-m" && second == "pytest" {
			return "test"
		}
	case "tsc":
		return "check"
	case "eslint", "golangci-lint", "ruff", "clippy":
		return "lint"
	case "mvn", "mvnw":
		return verificationTargetKind(first)
	case "gradle", "gradlew":
		return verificationTargetKind(first)
	case "make":
		return verificationTargetKind(first)
	}
	return ""
}

// Skip only known directory selectors, whose values cannot be script names.
// Unknown leading options stay unclassified rather than guessing their arity.
func packageManagerVerificationArgs(op string, args []string) []string {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flag, _, inline := strings.Cut(args[0], "=")
		known := (op == "npm" && flag == "--prefix") ||
			(op == "pnpm" && (flag == "--dir" || flag == "-C")) ||
			(op == "yarn" && flag == "--cwd") ||
			(op == "bun" && flag == "--cwd")
		if !known {
			return nil
		}
		if inline {
			args = args[1:]
		} else {
			if len(args) < 2 {
				return nil
			}
			args = args[2:]
		}
	}
	return args
}

func lowerArg(args []string, index int) string {
	if index < 0 || index >= len(args) {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(args[index]))
}

func verificationTargetKind(target string) string {
	target = strings.ToLower(strings.TrimSpace(target))
	switch {
	case strings.Contains(target, "test"):
		return "test"
	case strings.Contains(target, "lint") || strings.Contains(target, "vet") || strings.Contains(target, "clippy"):
		return "lint"
	case strings.Contains(target, "build") || strings.Contains(target, "compile"):
		return "build"
	case strings.Contains(target, "check") || strings.Contains(target, "verify"):
		return "check"
	default:
		return ""
	}
}

func commandVerificationStatus(kind string, exitCode int, timedOut, cancelled bool, reason string) string {
	if kind == "" {
		return ""
	}
	switch {
	case timedOut:
		return "timed_out"
	case cancelled:
		return "cancelled"
	case reason == "start_failed" || exitCode < 0:
		return "failed"
	case exitCode == 0:
		return "passed"
	default:
		return "failed"
	}
}

func parseCommandDiagnostics(stdout, stderr, cwd string, projectDirs []string, failed bool) []commandDiagnostic {
	first, second := newCommandDiagnosticCollector(cwd, projectDirs), newCommandDiagnosticCollector(cwd, projectDirs)
	first.write([]byte(stdout))
	second.write([]byte(stderr))
	diagnostics, _ := finishCommandDiagnostics(first, second, failed)
	return diagnostics
}

func appendCommandDiagnostic(out *[]commandDiagnostic, seen map[string]bool, cwd string, projectDirs []string, rawPath, rawLine, rawColumn, severity, message, code, source string) {
	line, err := strconv.Atoi(rawLine)
	if err != nil || line < 1 {
		return
	}
	column, _ := strconv.Atoi(rawColumn)
	target, rel, _, ok := resolveCommandDiagnosticPath(rawPath, cwd, projectDirs)
	message = truncateDiagnosticText(strings.TrimSpace(message), commandDiagnosticMaxRunes)
	if message == "" {
		return
	}
	key := rawPath + "\x00" + strconv.Itoa(line) + "\x00" + strconv.Itoa(column) + "\x00" + message
	if seen[key] {
		return
	}
	seen[key] = true
	excerpt, lineStart, lineEnd := "", line, line
	if ok {
		excerpt, lineStart, lineEnd = commandDiagnosticExcerpt(target, line)
	}
	unresolvedPath := ""
	if !ok {
		unresolvedPath = truncateDiagnosticText(rawPath, commandDiagnosticMaxRunes)
	}
	*out = append(*out, commandDiagnostic{
		Path:          target,
		RelativePath:  rel,
		Line:          line,
		Column:        column,
		LineStart:     lineStart,
		LineEnd:       lineEnd,
		Severity:      normalizeDiagnosticSeverity(severity),
		inferSeverity: severity == "",
		Message:       message,
		Code:          strings.TrimSpace(code),
		Source:        source,
		Excerpt:       excerpt,
		RawPath:       unresolvedPath,
		Unresolved:    !ok,
	})
}

// Directory flags change the script's working directory, not the command's
// authorization roots. Only statically known package-manager flags are used.
func commandVerificationDetails(args commandRunArgs, cwd string) ([]string, string) {
	argv, directory := commandVerificationInvocation(args.Command)
	if directory != "" {
		if !filepath.IsAbs(directory) {
			// CDPATH can redirect a relative cd to a different directory. It is
			// not inherited by our shell, but may be explicitly supplied.
			if args.Env["CDPATH"] != "" {
				return nil, cwd
			}
			directory = filepath.Join(cwd, directory)
		}
		cwd = filepath.Clean(directory)
	}
	return argv, commandDiagnosticCWD(argv, cwd)
}

func commandDiagnosticCWD(argv []string, cwd string) string {
	if len(argv) == 0 {
		return cwd
	}
	op := commandOperation(argv[0])
	args := argv[1:]
	result := cwd
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		flag, value, inline := strings.Cut(args[0], "=")
		known := (op == "npm" && flag == "--prefix") ||
			(op == "pnpm" && (flag == "--dir" || flag == "-C")) ||
			((op == "yarn" || op == "bun") && flag == "--cwd")
		if !known {
			break
		}
		args = args[1:]
		if !inline {
			if len(args) == 0 {
				break
			}
			value, args = args[0], args[1:]
		}
		if value != "" {
			if !filepath.IsAbs(value) {
				value = filepath.Join(cwd, value)
			}
			result = filepath.Clean(value)
		}
	}
	return result
}

func resolveCommandDiagnosticPath(rawPath, cwd string, projectDirs []string) (string, string, string, bool) {
	rawPath = strings.Trim(strings.TrimSpace(rawPath), `"'`)
	if rawPath == "" {
		return "", "", "", false
	}
	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(cwd, filepath.FromSlash(candidate))
	}
	candidate = filepath.Clean(candidate)
	resolvedCandidate, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", "", "", false
	}
	info, err := os.Stat(resolvedCandidate)
	if err != nil || !info.Mode().IsRegular() {
		return "", "", "", false
	}
	for _, root := range normalizeProjectDirs(projectDirs) {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil || !pathInsideRoot(resolvedCandidate, resolvedRoot) {
			continue
		}
		rel, err := filepath.Rel(resolvedRoot, resolvedCandidate)
		if err != nil {
			continue
		}
		return resolvedCandidate, filepath.ToSlash(rel), resolvedRoot, true
	}
	return "", "", "", false
}

func commandDiagnosticExcerpt(path string, line int) (string, int, int) {
	start := max(1, line-commandDiagnosticContext)
	end := line + commandDiagnosticContext
	records, err := readLineRange(path, start, end)
	if err != nil || len(records) == 0 {
		return "", line, line
	}
	lines := make([]string, 0, len(records))
	for _, item := range records {
		lines = append(lines, truncateDiagnosticText(item.text, maxFileSearchExcerptLineChars))
	}
	return strings.Join(lines, "\n"), records[0].number, records[len(records)-1].number
}

func truncateDiagnosticText(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	return string([]rune(value)[:limit])
}

func normalizeDiagnosticSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warning", "warn":
		return "warning"
	case "info", "note":
		return "info"
	default:
		return "error"
	}
}

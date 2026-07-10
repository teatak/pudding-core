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
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
	Line         int    `json:"line"`
	Column       int    `json:"column,omitempty"`
	LineStart    int    `json:"lineStart"`
	LineEnd      int    `json:"lineEnd"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	Code         string `json:"code,omitempty"`
	Source       string `json:"source,omitempty"`
	Excerpt      string `json:"excerpt,omitempty"`
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
		if first == "test" {
			return "test"
		}
		if first == "run" {
			return verificationTargetKind(second)
		}
		return verificationTargetKind(first)
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
	combined := stdout
	if combined != "" && stderr != "" {
		combined += "\n"
	}
	combined += stderr
	lines := strings.Split(strings.ReplaceAll(combined, "\r\n", "\n"), "\n")
	diagnostics := make([]commandDiagnostic, 0)
	seen := make(map[string]bool)
	currentESLintPath := ""
	for _, line := range lines {
		if len(diagnostics) >= commandMaxDiagnostics {
			break
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if match := typeScriptDiagnosticPattern.FindStringSubmatch(trimmed); match != nil {
			appendCommandDiagnostic(&diagnostics, seen, cwd, projectDirs, match[1], match[2], match[3], match[4], match[6], match[5], "typescript")
			continue
		}
		if currentESLintPath != "" {
			if match := eslintDetailPattern.FindStringSubmatch(line); match != nil {
				appendCommandDiagnostic(&diagnostics, seen, cwd, projectDirs, currentESLintPath, match[1], match[2], match[3], match[4], match[5], "eslint")
				continue
			}
		}
		if match := pathColumnDiagnosticPattern.FindStringSubmatch(trimmed); match != nil {
			severity := match[4]
			if severity == "" {
				if failed {
					severity = "error"
				} else {
					severity = "warning"
				}
			}
			appendCommandDiagnostic(&diagnostics, seen, cwd, projectDirs, match[1], match[2], match[3], severity, match[6], match[5], "compiler")
			continue
		}
		if match := pathLineDiagnosticPattern.FindStringSubmatch(trimmed); match != nil {
			severity := match[3]
			if severity == "" {
				if failed {
					severity = "error"
				} else {
					severity = "warning"
				}
			}
			appendCommandDiagnostic(&diagnostics, seen, cwd, projectDirs, match[1], match[2], "", severity, match[5], match[4], "compiler")
			continue
		}
		if _, _, _, ok := resolveCommandDiagnosticPath(trimmed, cwd, projectDirs); ok {
			currentESLintPath = trimmed
		} else if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			currentESLintPath = ""
		}
	}
	return diagnostics
}

func appendCommandDiagnostic(out *[]commandDiagnostic, seen map[string]bool, cwd string, projectDirs []string, rawPath, rawLine, rawColumn, severity, message, code, source string) {
	line, err := strconv.Atoi(rawLine)
	if err != nil || line < 1 {
		return
	}
	column, _ := strconv.Atoi(rawColumn)
	target, rel, _, ok := resolveCommandDiagnosticPath(rawPath, cwd, projectDirs)
	if !ok {
		return
	}
	message = truncateDiagnosticText(strings.TrimSpace(message), commandDiagnosticMaxRunes)
	if message == "" {
		return
	}
	key := target + "\x00" + strconv.Itoa(line) + "\x00" + strconv.Itoa(column) + "\x00" + message
	if seen[key] {
		return
	}
	seen[key] = true
	excerpt, lineStart, lineEnd := commandDiagnosticExcerpt(target, line)
	*out = append(*out, commandDiagnostic{
		Path:         target,
		RelativePath: rel,
		Line:         line,
		Column:       column,
		LineStart:    lineStart,
		LineEnd:      lineEnd,
		Severity:     normalizeDiagnosticSeverity(severity),
		Message:      message,
		Code:         strings.TrimSpace(code),
		Source:       source,
		Excerpt:      excerpt,
	})
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
	slice, err := readLineRange(path, start, end)
	if err != nil || len(slice.lines) == 0 {
		return "", line, line
	}
	lines := make([]string, 0, len(slice.lines))
	for _, item := range slice.lines {
		lines = append(lines, truncateDiagnosticText(item.text, maxFileSearchExcerptLineChars))
	}
	return strings.Join(lines, "\n"), slice.start, slice.end
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

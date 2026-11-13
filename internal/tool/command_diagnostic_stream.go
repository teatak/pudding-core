package tool

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"
)

const commandDiagnosticLineBytes = 64 << 10

var diagnosticANSI = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]`)

// Each output stream has an independent collector: neither chunk boundaries nor
// interleaved stderr can corrupt a line or ESLint's current filename.
type commandDiagnosticCollector struct {
	cwd, eslintPath string
	projectDirs     []string
	pending         []byte
	droppingLine    bool
	truncated       bool
	diagnostics     []commandDiagnostic
	seen            map[string]bool
	tests           *commandTestCollector
}

func newCommandDiagnosticCollector(cwd string, projectDirs []string) *commandDiagnosticCollector {
	return &commandDiagnosticCollector{cwd: cwd, projectDirs: projectDirs, seen: make(map[string]bool)}
}

func (c *commandDiagnosticCollector) write(data []byte) {
	for len(data) > 0 {
		end := bytes.IndexByte(data, '\n')
		part := data
		if end >= 0 {
			part = data[:end]
		}
		if !c.droppingLine {
			if len(c.pending)+len(part) > commandDiagnosticLineBytes {
				c.pending = nil
				c.droppingLine, c.truncated = true, true
				if c.tests != nil {
					c.tests.truncated = true
					c.tests.execution.incomplete = true
				}
			} else {
				c.pending = append(c.pending, part...)
			}
		}
		if end < 0 {
			return
		}
		if !c.droppingLine {
			c.line(string(c.pending))
		}
		c.pending, c.droppingLine = c.pending[:0], false
		data = data[end+1:]
	}
}

func (c *commandDiagnosticCollector) line(line string) {
	line = diagnosticANSI.ReplaceAllString(line, "")
	if c.tests != nil {
		c.tests.line(line)
		if c.tests.tap || (c.tests.goJSON && strings.HasPrefix(strings.TrimSpace(line), "{")) {
			return
		}
	}
	if len(c.diagnostics) >= commandMaxDiagnostics {
		c.truncated = true
		return
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return
	}
	add := func(path, line, column, severity, message, code, source string) {
		appendCommandDiagnostic(&c.diagnostics, c.seen, c.cwd, c.projectDirs, path, line, column, severity, message, code, source)
	}
	if match := typeScriptDiagnosticPattern.FindStringSubmatch(trimmed); match != nil {
		add(match[1], match[2], match[3], match[4], match[6], match[5], "typescript")
		return
	}
	if match := eslintDetailPattern.FindStringSubmatch(line); match != nil {
		if c.eslintPath != "" {
			add(c.eslintPath, match[1], match[2], match[3], match[4], match[5], "eslint")
		}
		return
	}
	if match := pathColumnDiagnosticPattern.FindStringSubmatch(trimmed); match != nil {
		add(match[1], match[2], match[3], match[4], match[6], match[5], "compiler")
		return
	}
	if match := pathLineDiagnosticPattern.FindStringSubmatch(trimmed); match != nil {
		add(match[1], match[2], "", match[3], match[5], match[4], "compiler")
		return
	}
	if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
		// Validate the path only if an ESLint diagnostic follows it, avoiding a
		// filesystem lookup for every ordinary build progress line.
		c.eslintPath = trimmed
	}
}

func finishCommandDiagnostics(first, second *commandDiagnosticCollector, failed bool) ([]commandDiagnostic, bool) {
	out := make([]commandDiagnostic, 0)
	seen := make(map[string]bool)
	truncated := false
	for _, c := range []*commandDiagnosticCollector{first, second} {
		if len(c.pending) > 0 && !c.droppingLine {
			c.line(string(c.pending))
			c.pending = nil
		}
		truncated = truncated || c.truncated
		for _, diagnostic := range c.diagnostics {
			key := diagnostic.Path + diagnostic.RawPath + "\x00" + strconv.Itoa(diagnostic.Line) + "\x00" + strconv.Itoa(diagnostic.Column) + "\x00" + diagnostic.Message
			if seen[key] {
				continue
			}
			seen[key] = true
			if len(out) == commandMaxDiagnostics {
				truncated = true
				break
			}
			if diagnostic.inferSeverity && !failed {
				diagnostic.Severity = "warning"
			}
			out = append(out, diagnostic)
		}
	}
	return out, truncated
}

func newCommandOutputBuffers(args commandRunArgs, cwd string, projectDirs []string) (*truncatingBuffer, *truncatingBuffer) {
	stdout, stderr := newTruncatingBuffer(commandOutputLimitBytes), newTruncatingBuffer(commandOutputLimitBytes)
	argv, cwd := commandVerificationDetails(args, cwd)
	if kind := commandVerificationKind(argv); kind != "" {
		stdout.diagnostics = newCommandDiagnosticCollector(cwd, projectDirs)
		stderr.diagnostics = newCommandDiagnosticCollector(cwd, projectDirs)
		if kind == "test" {
			stdout.diagnostics.tests = newCommandTestCollector(argv, cwd, projectDirs)
		}
	}
	return stdout, stderr
}

package tool

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const commandMaxTestFailures = 50
const commandTestOutputBytes = 16 << 10
const commandMaxPendingTests = 256

type commandTestFailure struct {
	commandDiagnostic
	Name    string `json:"name"`
	Package string `json:"package,omitempty"`
	Kind    string `json:"kind"`
}

// Only observed failures are reported. Neither incomplete reporter output nor
// an empty failure list changes the process exit status or claims tests passed.
type commandTestCollector struct {
	cwd        string
	roots      []string
	goJSON     bool
	pending    map[string]string
	tap        bool
	tapFailure *commandTestFailure
	tapIndent  int
	tapYAML    string
	failures   []commandTestFailure
	truncated  bool
	execution  *commandTestExecution
}

var tapFailurePattern = regexp.MustCompile(`^(\s*)not ok\s+\d+\s*-?\s*(.*)$`)
var testLocationPattern = regexp.MustCompile(`^(.+?):(\d+)(?::(\d+))?$`)

func newCommandTestCollector(argv []string, cwd string, roots []string) *commandTestCollector {
	c := &commandTestCollector{cwd: cwd, roots: roots, pending: make(map[string]string)}
	if len(argv) > 1 && commandOperation(argv[0]) == "go" && argv[1] == "test" {
		for _, arg := range argv[2:] {
			if arg == "-json" {
				c.goJSON = true
			}
		}
	}
	c.execution = newCommandTestExecution(argv, c.goJSON)
	return c
}

func (c *commandTestCollector) line(line string) {
	c.execution.line(line)
	if c.goJSON {
		var event struct{ Action, Package, Test, Output string }
		if json.Unmarshal([]byte(line), &event) != nil || event.Package == "" {
			return
		}
		key := event.Package + "\x00" + event.Test
		switch event.Action {
		case "output":
			if _, exists := c.pending[key]; !exists && len(c.pending) >= commandMaxPendingTests {
				c.truncated = true
				return
			}
			text := c.pending[key] + event.Output
			if len(text) > commandTestOutputBytes {
				text = strings.ToValidUTF8(text[len(text)-commandTestOutputBytes:], "")
				c.truncated = true
			}
			c.pending[key] = text
		case "pass", "skip":
			delete(c.pending, key)
		case "fail":
			kind, name := "test", event.Test
			if name == "" {
				kind, name = "package", event.Package
			}
			c.add(commandTestFailure{commandDiagnostic: commandDiagnostic{Message: strings.TrimSpace(c.pending[key]), Severity: "error", Source: "go-test"}, Name: name, Package: event.Package, Kind: kind})
			delete(c.pending, key)
		}
		return
	}
	if strings.TrimSpace(line) == "TAP version 13" {
		c.tap = true
		return
	}
	if !c.tap {
		return
	}
	if match := tapFailurePattern.FindStringSubmatch(line); match != nil {
		c.finishTAP()
		name := match[2]
		// TAP TODO and SKIP results are not actionable failures.
		upper := strings.ToUpper(name)
		if strings.Contains(upper, " # TODO") || strings.Contains(upper, " # SKIP") {
			return
		}
		c.tapIndent = len(match[1])
		c.tapFailure = &commandTestFailure{commandDiagnostic: commandDiagnostic{Severity: "error", Source: "node-test"}, Name: name, Kind: "test"}
		return
	}
	if c.tapFailure == nil {
		return
	}
	trimmed := strings.TrimSpace(line)
	if trimmed == "..." {
		c.finishTAP()
		return
	}
	if trimmed == "---" {
		return
	}
	indent := len(line) - len(strings.TrimLeft(line, " \t"))
	if trimmed != "" && indent <= c.tapIndent {
		c.finishTAP()
		return
	}
	if len(c.tapYAML)+len(line)+1 <= commandTestOutputBytes {
		c.tapYAML += line + "\n"
	} else {
		c.truncated = true
	}
}

func (c *commandTestCollector) finishTAP() {
	if c.tapFailure == nil {
		return
	}
	failure := *c.tapFailure
	var fields struct {
		Location    string `yaml:"location"`
		Error       string `yaml:"error"`
		FailureType string `yaml:"failureType"`
		Code        string `yaml:"code"`
		Expected    any    `yaml:"expected"`
		Actual      any    `yaml:"actual"`
	}
	if err := yaml.Unmarshal([]byte(c.tapYAML), &fields); err == nil {
		failure.Message, failure.Code = fields.Error, fields.Code
		if failure.Message == "" {
			failure.Message = fields.FailureType
		}
		if fields.Expected != nil || fields.Actual != nil {
			expected, _ := json.Marshal(fields.Expected)
			actual, _ := json.Marshal(fields.Actual)
			failure.Message += "\nexpected: " + string(expected) + "\nactual: " + string(actual)
		}
		c.setLocation(&failure, fields.Location)
	} else {
		failure.Message = strings.TrimSpace(c.tapYAML)
		c.truncated = true
	}
	c.add(failure)
	c.tapFailure, c.tapYAML = nil, ""
}

func (c *commandTestCollector) setLocation(failure *commandTestFailure, location string) {
	c.setLocationAt(failure, location, c.cwd)
}

func (c *commandTestCollector) setLocationAt(failure *commandTestFailure, location, cwd string) {
	match := testLocationPattern.FindStringSubmatch(location)
	if match == nil {
		return
	}
	path := match[1]
	if parsed, err := url.Parse(path); err == nil && parsed.Scheme == "file" && (parsed.Host == "" || parsed.Host == "localhost") {
		path = filepath.FromSlash(parsed.Path)
	}
	failure.Line, _ = strconv.Atoi(match[2])
	failure.Column, _ = strconv.Atoi(match[3])
	resolved, rel, ok := "", "", false
	if cwd != "" || filepath.IsAbs(path) {
		resolved, rel, _, ok = resolveCommandDiagnosticPath(path, cwd, c.roots)
	}
	if !ok {
		failure.RawPath, failure.Unresolved = truncateDiagnosticText(path, 1000), true
		return
	}
	failure.Path, failure.RelativePath = resolved, rel
	failure.Excerpt, failure.LineStart, failure.LineEnd = commandDiagnosticExcerpt(resolved, failure.Line)
}

func (c *commandTestCollector) add(failure commandTestFailure) {
	if len(c.failures) >= commandMaxTestFailures {
		c.truncated = true
		return
	}
	// Go output often names a file relative to its package. Keep that location
	// as raw evidence when the command directory cannot resolve it safely.
	if failure.Source == "go-test" {
		for _, line := range strings.Split(failure.Message, "\n") {
			if match := pathLineDiagnosticPattern.FindStringSubmatch(strings.TrimSpace(line)); match != nil {
				c.setLocationAt(&failure, match[1]+":"+match[2], c.goPackageDirectory(failure.Package))
				break
			}
		}
	}
	if utf8.RuneCountInString(failure.Message) > 1000 || utf8.RuneCountInString(failure.Name) > 500 || utf8.RuneCountInString(failure.Package) > 500 {
		c.truncated = true
	}
	failure.Message = truncateDiagnosticText(failure.Message, 1000)
	failure.Name = truncateDiagnosticText(failure.Name, 500)
	failure.Package = truncateDiagnosticText(failure.Package, 500)
	c.failures = append(c.failures, failure)
}

func (c *commandTestCollector) goPackageDirectory(pkg string) string {
	for dir := c.cwd; ; dir = filepath.Dir(dir) {
		moduleFile, _, _, allowed := resolveCommandDiagnosticPath("go.mod", dir, c.roots)
		if allowed {
			if info, err := os.Stat(moduleFile); err == nil && info.Size() <= 64<<10 {
				data, err := os.ReadFile(moduleFile)
				if err != nil {
					return ""
				}
				for _, line := range strings.Split(string(data), "\n") {
					fields := strings.Fields(line)
					if len(fields) < 2 || fields[0] != "module" {
						continue
					}
					module := fields[1]
					if unquoted, err := strconv.Unquote(module); err == nil {
						module = unquoted
					}
					if pkg == module {
						return dir
					}
					if strings.HasPrefix(pkg, module+"/") {
						return filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(pkg, module+"/")))
					}
					return ""
				}
			}
		}
		if filepath.Dir(dir) == dir {
			return ""
		}
	}
}

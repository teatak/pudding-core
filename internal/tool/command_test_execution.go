package tool

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Execution evidence complements the exit code. Missing/unsupported reporter
// output is unknown, never evidence that zero tests ran.
type commandTestExecution struct {
	goCommand, goJSON, collected             bool
	goPackages                               map[string]*goTestExecution
	goPlainRan, goPlainSkipped, goPlainEmpty bool
	tap                                      *tapTestExecution
	ran, skipped, empty, unknown, incomplete bool
}

type goTestExecution struct{ done, ran, skipped bool }
type tapTestExecution struct {
	counts    map[string]int
	wrappers  int
	emptyPlan bool
	subtest   string
}

var goTestSummaryPattern = regexp.MustCompile(`^(ok|\?)\s+\S+\s+(.+)$`)
var tapTestCountPattern = regexp.MustCompile(`^# (tests|pass|fail|cancelled|skipped|todo) (\d+)\s*$`)
var tapTestResultPattern = regexp.MustCompile(`^(?:not )?ok\s+\d+\s*-?\s*(.*)$`)

func newCommandTestExecution(argv []string, goJSON bool) *commandTestExecution {
	e := &commandTestExecution{goJSON: goJSON, goPackages: make(map[string]*goTestExecution)}
	if len(argv) == 0 {
		return e
	}
	op, args := commandOperation(argv[0]), argv[1:]
	e.goCommand = op == "go" && len(args) > 0 && args[0] == "test"
	pytest := op == "pytest" || ((op == "python" || op == "python3") && len(args) > 1 && args[0] == "-m" && args[1] == "pytest")
	for _, arg := range args {
		if arg == "--" || (e.goCommand && arg == "-args") {
			break
		}
		if (e.goCommand && (arg == "-list" || strings.HasPrefix(arg, "-list="))) || (pytest && (arg == "--collect-only" || arg == "--co")) {
			e.collected = true
		}
	}
	return e
}

func (e *commandTestExecution) line(line string) {
	if e.goJSON {
		var event struct{ Action, Package, Test string }
		if json.Unmarshal([]byte(line), &event) != nil {
			if strings.HasPrefix(line, "{") {
				e.incomplete = true
			}
			return
		}
		if event.Package == "" {
			return
		}
		state := e.goPackages[event.Package]
		if state == nil {
			if len(e.goPackages) >= 1024 {
				e.unknown = true
				return
			}
			state = &goTestExecution{}
			e.goPackages[event.Package] = state
		}
		if event.Test == "" {
			if event.Action == "pass" || event.Action == "skip" || event.Action == "fail" {
				state.done = true
			}
		} else {
			switch event.Action {
			case "pass", "fail":
				state.ran = true
			case "skip":
				state.skipped = true
			}
		}
		return
	}
	if e.goCommand {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--- PASS:") || strings.HasPrefix(trimmed, "--- FAIL:") {
			e.goPlainRan = true
		}
		if strings.HasPrefix(trimmed, "--- SKIP:") {
			e.goPlainSkipped = true
		}
		if match := goTestSummaryPattern.FindStringSubmatch(line); match != nil {
			if strings.Contains(match[2], "[no tests to run]") || strings.Contains(match[2], "[no test files]") {
				e.goPlainEmpty = true
			} else {
				// A plain `ok` can also mean all tests skipped. Without verbose
				// events it is not evidence of actual test execution.
				e.unknown = true
			}
		}
		return
	}
	if line == "TAP version 13" {
		e.finishTAP()
		e.tap = &tapTestExecution{counts: make(map[string]int)}
		return
	}
	if e.tap == nil {
		return
	}
	if match := tapTestCountPattern.FindStringSubmatch(line); match != nil {
		count, err := strconv.Atoi(match[2])
		if err != nil {
			e.unknown = true
			return
		}
		e.tap.counts[match[1]] = count
	}
	// Node can report an unmatched file as a successful wrapper test after
	// emitting 1..0. Exclude only that paired absolute file wrapper, never a
	// normal successful case in another file or a nested suite's empty plan.
	if line == "1..0" {
		e.tap.emptyPlan = true
		e.tap.subtest = ""
	}
	if strings.HasPrefix(line, "# Subtest: ") {
		e.tap.subtest = strings.TrimPrefix(line, "# Subtest: ")
	}
	if match := tapTestResultPattern.FindStringSubmatch(line); match != nil {
		if e.tap.emptyPlan && match[1] == e.tap.subtest && filepath.IsAbs(e.tap.subtest) && strings.HasPrefix(line, "ok ") {
			e.tap.wrappers++
		}
		e.tap.emptyPlan = false
		e.tap.subtest = ""
	}
}

func (e *commandTestExecution) finishTAP() {
	if e.tap == nil {
		return
	}
	tap := e.tap
	e.tap = nil
	tests, hasTests := tap.counts["tests"]
	passed, hasPassed := tap.counts["pass"]
	failed, hasFailed := tap.counts["fail"]
	skipped, hasSkipped := tap.counts["skipped"]
	todo, hasTodo := tap.counts["todo"]
	cancelled, hasCancelled := tap.counts["cancelled"]
	if !hasTests || !hasPassed || !hasFailed || !hasSkipped || !hasTodo || !hasCancelled || tap.wrappers > passed || tests != passed+failed+skipped+todo+cancelled {
		e.unknown = true
		return
	}
	if passed > tap.wrappers || failed > 0 || cancelled > 0 {
		e.ran = true
	} else if skipped+todo > 0 {
		e.skipped = true
	} else {
		e.empty = true
	}
}

func (e *commandTestExecution) status() string {
	e.finishTAP()
	if e.collected {
		return "collected"
	}
	for _, pkg := range e.goPackages {
		if !pkg.done {
			e.unknown = true
		}
		if pkg.ran {
			e.ran = true
		} else if pkg.skipped {
			e.skipped = true
		} else if pkg.done {
			e.empty = true
		}
	}
	if e.goPlainRan || e.ran {
		return "ran"
	}
	if e.incomplete {
		return "unknown"
	}
	// Verbose Go skip events are conclusive even though its final ok summary
	// does not distinguish skipped cases from successful cases.
	if e.goCommand && !e.goJSON && e.goPlainSkipped {
		return "all_skipped"
	}
	if e.unknown {
		return "unknown"
	}
	if e.skipped {
		return "all_skipped"
	}
	if e.empty || e.goPlainEmpty {
		return "no_tests"
	}
	return "unknown"
}

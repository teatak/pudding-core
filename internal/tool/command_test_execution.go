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

func newCommandTestExecution(argv []string) *commandTestExecution {
	e := &commandTestExecution{goPackages: make(map[string]*goTestExecution)}
	if len(argv) == 0 {
		return e
	}
	op, args := commandOperation(argv[0]), argv[1:]
	e.goCommand = op == "go" && len(args) > 0 && args[0] == "test"
	if e.goCommand {
		e.goJSON, e.collected = goTestOutputOptions(args[1:])
		return e
	}
	pytest := op == "pytest" || ((op == "python" || op == "python3") && len(args) > 1 && args[0] == "-m" && args[1] == "pytest")
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if pytest && (arg == "--collect-only" || arg == "--co") {
			e.collected = true
		}
	}
	return e
}

// Consume known value flags before interpreting output options. Go accepts
// flags on either side of the package list, but -args/-- ends Go flag parsing.
func goTestOutputOptions(args []string) (jsonOutput, collected bool) {
	inPackages, afterPackages := false, false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || arg == "-args" || arg == "--args" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			if afterPackages {
				break
			}
			inPackages = true
			continue
		}
		if inPackages {
			inPackages, afterPackages = false, true
		}
		flag, value, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if flag == "json" {
			jsonOutput = true
			if inline {
				var err error
				jsonOutput, err = strconv.ParseBool(value)
				if err != nil {
					return false, false
				}
			}
			continue
		}
		switch strings.TrimPrefix(flag, "test.") {
		case "list", "run", "skip", "bench", "benchtime", "count", "cpu", "parallel", "shuffle", "timeout",
			"fuzz", "fuzztime", "fuzzminimizetime", "blockprofile", "blockprofilerate", "coverprofile", "cpuprofile",
			"memprofile", "memprofilerate", "mutexprofile", "mutexprofilefraction", "outputdir", "trace",
			"o", "vet", "covermode", "coverpkg", "tags", "ldflags", "gcflags", "asmflags", "gccgoflags", "toolexec",
			"mod", "modfile", "overlay", "pkgdir", "p", "buildmode", "compiler", "installsuffix", "pgo":
			if !inline {
				i++
				if i == len(args) {
					return false, false
				}
				value = args[i]
			}
			if flag == "list" || flag == "test.list" {
				collected = value != ""
			}
		case "v", "short", "failfast", "fullpath", "benchmem", "artifacts", "c", "a", "n", "x", "work",
			"race", "msan", "asan", "cover", "trimpath", "modcacherw", "linkshared", "buildvcs":
		default:
			if !inline {
				return false, false // Unknown flag arity can hide later output options.
			}
		}
	}
	return jsonOutput, collected
}

// Streams keep independent line/report buffers. Merge only completed evidence,
// so a silent stream does not turn a conclusive report into "unknown".
func (e *commandTestExecution) merge(other *commandTestExecution) {
	e.finishTAP()
	other.finishTAP()
	e.ran = e.ran || other.ran
	e.skipped = e.skipped || other.skipped
	e.empty = e.empty || other.empty
	e.unknown = e.unknown || other.unknown
	e.incomplete = e.incomplete || other.incomplete
	e.goPlainRan = e.goPlainRan || other.goPlainRan
	e.goPlainSkipped = e.goPlainSkipped || other.goPlainSkipped
	e.goPlainEmpty = e.goPlainEmpty || other.goPlainEmpty
	for name, pkg := range other.goPackages {
		state := e.goPackages[name]
		if state == nil {
			if len(e.goPackages) >= 1024 {
				e.unknown = true
				continue
			}
			state = &goTestExecution{}
			e.goPackages[name] = state
		}
		state.done = state.done || pkg.done
		state.ran = state.ran || pkg.ran
		state.skipped = state.skipped || pkg.skipped
	}
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

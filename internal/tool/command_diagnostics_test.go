package tool

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCommandTestExecutionEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell does not emit POSIX verification metadata")
	}
	root := t.TempDir()
	jsonEvents := func(events ...map[string]string) string {
		var out strings.Builder
		for _, event := range events {
			data, _ := json.Marshal(event)
			out.Write(data)
			out.WriteByte('\n')
		}
		return out.String()
	}
	empty := jsonEvents(map[string]string{"Action": "start", "Package": "p"}, map[string]string{"Action": "pass", "Package": "p"})
	skipped := jsonEvents(map[string]string{"Action": "run", "Package": "p", "Test": "TestSkip"}, map[string]string{"Action": "skip", "Package": "p", "Test": "TestSkip"}, map[string]string{"Action": "pass", "Package": "p"})
	ran := jsonEvents(map[string]string{"Action": "pass", "Package": "q", "Test": "TestPass"}, map[string]string{"Action": "pass", "Package": "q"})
	tapSummary := func(tests, passed, skipped int) string {
		return fmt.Sprintf("# tests %d\n# pass %d\n# fail 0\n# cancelled 0\n# skipped %d\n# todo 0\n", tests, passed, skipped)
	}
	wrapper := "1..0\n# Subtest: " + filepath.Join(root, "empty.test.js") + "\nok 1 - " + filepath.Join(root, "empty.test.js") + "\n"
	for _, tc := range []struct{ name, command, output, want string }{
		{"go empty", "go test -json ./...", empty, "no_tests"},
		{"go skipped", "go test -json ./...", skipped, "all_skipped"},
		{"go mixed", "go test -json ./...", empty + skipped + ran, "ran"},
		{"go incomplete", "go test -json ./...", jsonEvents(map[string]string{"Action": "start", "Package": "p"}), "unknown"},
		{"go corrupt", "go test -json ./...", empty + "{invalid\n", "unknown"},
		{"go plain no matches", "go test -run Missing ./...", "ok\tp\t0.01s [no tests to run]\n", "no_tests"},
		{"go plain no files", "go test ./...", "?   p [no test files]\n", "no_tests"},
		{"go plain mixed", "go test ./...", "ok p 0.01s [no tests to run]\nok q 0.02s\n", "unknown"},
		{"go verbose skip", "go test -v ./...", "--- SKIP: TestSkip (0.00s)\nok p 0.01s\n", "all_skipped"},
		{"go list", "go test -list Test ./...", "TestOne\nok p 0.01s\n", "collected"},
		{"pytest collect", "python3 -m pytest --collect-only", "1 test collected\n", "collected"},
		{"tap empty", "node --test", "TAP version 13\n1..0\n" + tapSummary(0, 0, 0), "no_tests"},
		{"tap wrapper", "node --test", "TAP version 13\n" + wrapper + tapSummary(1, 1, 0), "no_tests"},
		{"tap wrapper and real", "node --test", "TAP version 13\n" + wrapper + "ok 2 - real test\n" + tapSummary(2, 2, 0), "ran"},
		{"tap skips", "npm test", "TAP version 13\nok 1 - skipped # SKIP\n" + tapSummary(1, 0, 1), "all_skipped"},
		{"tap mixed", "npm test", "TAP version 13\nok 1 - skipped # SKIP\nok 2 - real\n" + tapSummary(2, 1, 1), "ran"},
		{"nested empty plan", "node --test", "TAP version 13\n    1..0\nok 1 - actual\n" + tapSummary(1, 1, 0), "ran"},
		{"tap incomplete", "node --test", "TAP version 13\n1..0\n# tests 0\n", "unknown"},
		{"unknown reporter", "npm test", "done\n", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := commandRunArgs{Command: tc.command}
			stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
			if stdout.diagnostics == nil {
				t.Fatal("test command was not classified")
			}
			for _, b := range []byte(tc.output) {
				stdout.Write([]byte{b})
			}
			result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 0, stdout, stderr, false, false, 0, "", nil)
			fields := decodeToolResult(t, result)
			wantStatus := tc.want
			if wantStatus == "ran" {
				wantStatus = "passed"
			}
			if wantStatus == "unknown" {
				wantStatus = "completed"
			}
			if fields["testExecutionStatus"] != tc.want || fields["verificationStatus"] != wantStatus || fields["exitCode"] != float64(0) || fields["ok"] != true {
				t.Fatalf("%s", result.Content)
			}
			preview := ModelResultContent(CommandRun, true, result.Content, "turn", "call", true)
			if !strings.Contains(preview, `"testExecutionStatus"`) {
				t.Fatalf("execution evidence lost: %s", preview)
			}
		})
	}
}

func TestCommandTestExecutionDoesNotOverrideExitOrMissingOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell does not emit POSIX verification metadata")
	}
	root := t.TempDir()
	args := commandRunArgs{Command: "go test -list Test"}
	for _, tc := range []struct {
		exit            int
		timeout, cancel bool
		want            string
	}{{1, false, false, "failed"}, {-1, true, false, "timed_out"}, {-1, false, true, "cancelled"}} {
		stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
		if stdout.diagnostics == nil {
			t.Fatal("test command was not classified")
		}
		fields := decodeToolResult(t, commandResult(Result{}, args, "sh", root, []string{root}, nil, tc.exit, stdout, stderr, tc.timeout, tc.cancel, 0, "", nil))
		if fields["verificationStatus"] != tc.want {
			t.Fatalf("exit status masked: %+v", fields)
		}
	}
	e := newCommandTestExecution([]string{"go", "test", "-v"}, false)
	e.line("--- SKIP: TestSkip (0.00s)")
	e.incomplete = true
	if e.status() != "unknown" {
		t.Fatal("dropped output claimed all tests skipped")
	}
}

func TestCommandDiagnosticsPackageDirectoryAndUnresolvedPaths(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "web/src/app.ts"), "const child = 1;\n")
	writePatchTestFile(t, filepath.Join(root, "src/app.ts"), "const wrong = 2;\n")
	writePatchTestFile(t, filepath.Join(outside, "secret.ts"), "private source\n")
	for _, argv := range [][]string{{"npm", "--prefix", "web", "test"}, {"npm", "--prefix=web", "run", "typecheck"}, {"pnpm", "-C", "web", "test"}, {"yarn", "--cwd", "web", "test"}, {"bun", "--cwd=web", "test"}} {
		cwd := commandDiagnosticCWD(argv, root)
		c := newCommandDiagnosticCollector(cwd, []string{root})
		c.write([]byte("src/app.ts(1,7): error TS1234: bad child\nmissing.ts:2:3: error: missing source\n" + filepath.Join(outside, "secret.ts") + ":1:1: error: forbidden source\n"))
		d, _ := finishCommandDiagnostics(c, newCommandDiagnosticCollector(cwd, []string{root}), true)
		if len(d) != 3 || d[0].RelativePath != "web/src/app.ts" || !strings.Contains(d[0].Excerpt, "child") {
			t.Fatalf("%v: %+v", argv, d)
		}
		for _, unresolved := range d[1:] {
			if !unresolved.Unresolved || unresolved.Path != "" || unresolved.Excerpt != "" || unresolved.Message == "" || unresolved.RawPath == "" {
				t.Fatalf("unsafe/dropped diagnostic: %+v", unresolved)
			}
		}
	}
}

func TestCommandDiagnosticsUseChainedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell does not use POSIX verification metadata")
	}
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "web/src/app.ts"), "const child = 1;\n")
	writePatchTestFile(t, filepath.Join(root, "src/app.ts"), "const wrong = 2;\n")
	for _, command := range []string{"cd web && npm run typecheck", "cd . && npm --prefix web run typecheck"} {
		args := commandRunArgs{Command: command}
		stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
		stderr.Write([]byte("src/app.ts(1,7): error TS1234: bad child\n"))
		result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 1, stdout, stderr, false, false, 0, "", nil)
		fields := decodeToolResult(t, result)
		if fields["verificationStatus"] != "failed" || fields["diagnosticCount"] != float64(1) {
			t.Fatalf("%s: %s", command, result.Content)
		}
		diagnostic := fields["diagnostics"].([]any)[0].(map[string]any)
		if diagnostic["relativePath"] != "web/src/app.ts" || !strings.Contains(diagnostic["excerpt"].(string), "child") {
			t.Fatalf("diagnostic resolved against the wrong cwd: %+v", diagnostic)
		}
		if fields["command"] != command || fields["cwd"] != root {
			t.Fatal("diagnostic analysis changed command execution metadata")
		}
	}
	args := commandRunArgs{Command: "cd web && npm test", Env: map[string]string{"CDPATH": "/elsewhere"}}
	if argv, _ := commandVerificationDetails(args, root); argv != nil {
		t.Fatal("explicit CDPATH allowed a guessed diagnostic directory")
	}
	outside := t.TempDir()
	writePatchTestFile(t, filepath.Join(outside, "app.ts"), "private source\n")
	args = commandRunArgs{Command: "cd '" + outside + "' && npm run typecheck"}
	stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
	stderr.Write([]byte("app.ts(1,1): error TS1234: forbidden source\n"))
	diagnostics, _ := finishCommandDiagnostics(stdout.diagnostics, stderr.diagnostics, true)
	if len(diagnostics) != 1 || !diagnostics[0].Unresolved || diagnostics[0].Path != "" || diagnostics[0].Excerpt != "" {
		t.Fatalf("directory prefix bypassed source read authorization: %+v", diagnostics)
	}
}

func TestCommandTestFailuresGoJSONAndNodeTAP(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "go.mod"), "module example.com/project\n\ngo 1.26\n")
	writePatchTestFile(t, filepath.Join(root, "pkg/check_test.go"), "package pkg\n// assertion location\n")
	writePatchTestFile(t, filepath.Join(root, "check.test.js"), "// assertion location\n")
	goOutput := ""
	for _, event := range []map[string]string{
		{"Action": "output", "Package": "example.com/project/pkg", "Test": "TestValue", "Output": "    check_test.go:2: want 2, got 1\n"},
		{"Action": "output", "Package": "example.com/project/pkg", "Test": "TestPass", "Output": "irrelevant passing output\n"},
		{"Action": "pass", "Package": "example.com/project/pkg", "Test": "TestPass"},
		{"Action": "fail", "Package": "example.com/project/pkg", "Test": "TestValue"},
	} {
		data, _ := json.Marshal(event)
		goOutput += string(data) + "\n"
	}
	for _, tc := range []struct{ command, output, name, path string }{
		{"go test -json ./...", goOutput, "TestValue", "pkg/check_test.go"},
		{"node --test --test-reporter=tap", "TAP version 13\nnot ok 1 - value assertion\n  ---\n  location: 'check.test.js:1:3'\n  error: |-\n    Expected values to match\n    多行断言\n  expected: 2\n  actual: 1\n  code: ERR_ASSERTION\n  ...\nnot ok 2 - deferred # TODO later\n", "value assertion", "check.test.js"},
	} {
		args := commandRunArgs{Command: tc.command}
		stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
		noise := strings.Repeat("progress\n", commandOutputLimitBytes)
		stdout.Write([]byte(noise))
		for _, b := range []byte(tc.output) {
			stdout.Write([]byte{b})
		}
		stdout.Write([]byte(noise))
		result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 1, stdout, stderr, false, false, 0, "non_zero_exit", nil)
		var got struct {
			TestFailures          []commandTestFailure
			TestFailuresTruncated bool
			StdoutTruncated       bool
			DiagnosticCount       int
		}
		if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
			t.Fatal(err)
		}
		if !got.StdoutTruncated || got.TestFailuresTruncated || got.DiagnosticCount != 0 || len(got.TestFailures) != 1 || got.TestFailures[0].Name != tc.name || got.TestFailures[0].RelativePath != tc.path || got.TestFailures[0].Message == "" {
			t.Fatalf("%s: %+v", tc.command, got)
		}
		for _, toolName := range []string{CommandRun, CommandSession} {
			result.Name = toolName
			preview := ModelResultContent(toolName, true, result.Content, "turn", "command", true)
			if !strings.Contains(preview, tc.name) || !strings.Contains(preview, "testFailureCount") {
				t.Fatalf("preview lost failure: %s", preview)
			}
		}
	}
}

func TestCommandTestFailuresRemainBoundedAndKeepUnknownLocations(t *testing.T) {
	short := newCommandTestCollector(nil, t.TempDir(), nil)
	short.add(commandTestFailure{Name: "test", commandDiagnostic: commandDiagnostic{Message: strings.Repeat("x", 1500)}})
	if !short.truncated || len(short.failures[0].Message) != 1000 {
		t.Fatal("shortened failure message was not marked incomplete")
	}
	c := newCommandTestCollector([]string{"go", "test", "-json"}, t.TempDir(), nil)
	for i := 0; i < commandMaxPendingTests+10; i++ {
		data, _ := json.Marshal(map[string]string{"Action": "output", "Package": "example.com/test", "Test": fmt.Sprint(i), "Output": strings.Repeat("x", commandTestOutputBytes+1)})
		c.line(string(data))
	}
	if !c.truncated || len(c.pending) > commandMaxPendingTests {
		t.Fatal("unbounded pending tests")
	}
	for i := 0; i < commandMaxTestFailures+1; i++ {
		data, _ := json.Marshal(map[string]string{"Action": "fail", "Package": "example.com/test", "Test": fmt.Sprint(i)})
		c.line(string(data))
	}
	if len(c.failures) != commandMaxTestFailures || !c.truncated {
		t.Fatal("failure limit ignored")
	}
	tap := newCommandTestCollector([]string{"node", "--test"}, t.TempDir(), nil)
	for _, line := range strings.Split("TAP version 13\nnot ok 1 - unknown\n  ---\n  location: 'missing.js:12:3'\n  error: bad\n  ...", "\n") {
		tap.line(line)
	}
	if len(tap.failures) != 1 || !tap.failures[0].Unresolved || tap.failures[0].Path != "" || tap.failures[0].Line != 12 || tap.failures[0].Column != 3 {
		t.Fatalf("lost unresolved location: %+v", tap.failures)
	}
}

func TestCommandDiagnosticsSurviveOutputTruncationAndChunkBoundaries(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "broken.go"), "package broken\nfunc broken() { missing() }\n")
	args := commandRunArgs{Command: "go test ./..."}
	stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
	noise := strings.Repeat("ordinary progress\n", commandOutputLimitBytes/8)
	stdout.Write([]byte(noise))
	// Split UTF-8 and ANSI sequences as a real pipe can; the final line has no LF.
	diagnostic := "\x1b[31mbroken.go:2:17: error: 未定义 missing\x1b[0m\r\n"
	for _, b := range []byte(diagnostic) {
		stdout.Write([]byte{b})
	}
	stdout.Write([]byte(noise))
	stderr.Write([]byte("broken.go:2:17: error: 未定义 missing"))
	result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 1, stdout, stderr, false, false, 0, "non_zero_exit", nil)
	payload := decodeCommandPayload(t, result)
	if !payload.StdoutTruncated || strings.Contains(payload.Stdout, "未定义") || payload.DiagnosticCount != 1 || payload.VerificationStatus != "failed" {
		t.Fatalf("lost or duplicated middle diagnostic: %+v", payload)
	}
	d := payload.Diagnostics[0]
	if d.RelativePath != "broken.go" || d.Line != 2 || d.Column != 17 || d.Message != "未定义 missing" || d.Severity != "error" || !strings.Contains(d.Excerpt, "func broken") {
		t.Fatalf("diagnostic context: %+v", d)
	}
	if decodeToolResult(t, result)["diagnosticsTruncated"] != false {
		t.Fatal("ordinary log truncation marked diagnostics incomplete")
	}
	for _, name := range []string{CommandRun, CommandSession} {
		preview := ModelResultContent(name, true, result.Content, "turn", "command", true)
		if !strings.Contains(preview, "未定义 missing") || !strings.Contains(preview, "broken.go") || !strings.Contains(preview, "diagnosticsTruncated") {
			t.Fatalf("%s model preview lost captured diagnostics: %s", name, preview)
		}
	}
}

func TestCommandDiagnosticsKeepStreamsSeparateAndBoundMemory(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "app.ts"), "const x = 1;\n")
	writePatchTestFile(t, filepath.Join(outside, "secret.ts"), "private source\n")
	first, second := newCommandDiagnosticCollector(root, []string{root}), newCommandDiagnosticCollector(root, []string{root})
	first.write([]byte(filepath.Join(root, "app.ts") + "\n"))
	second.write([]byte("  1:7  error  must not inherit stdout path  rule\n"))
	first.write([]byte("  1:7  warning  keep this ESLint message  rule\n"))
	second.write([]byte(filepath.Join(outside, "secret.ts") + ":1:1: secret error\n"))
	second.write([]byte(strings.Repeat("x", commandDiagnosticLineBytes+1)))
	if len(second.pending) > commandDiagnosticLineBytes {
		t.Fatal("unbounded pending line")
	}
	second.write([]byte("app.ts:1:1: fake suffix\napp.ts:1:1: genuine final error"))
	diagnostics, truncated := finishCommandDiagnostics(first, second, false)
	if !truncated || len(diagnostics) != 3 || diagnostics[0].Source != "eslint" || diagnostics[0].Severity != "warning" || !diagnostics[1].Unresolved || diagnostics[1].Path != "" || diagnostics[1].Excerpt != "" || diagnostics[2].Message != "genuine final error" || diagnostics[2].Severity != "warning" {
		t.Fatalf("stream isolation or recovery failed: %+v, truncated=%t", diagnostics, truncated)
	}
	limited := newCommandDiagnosticCollector(root, []string{root})
	for i := 0; i <= commandMaxDiagnostics; i++ {
		limited.write([]byte(fmt.Sprintf("app.ts:1:1: error %d\n", i)))
	}
	diagnostics, truncated = finishCommandDiagnostics(limited, newCommandDiagnosticCollector(root, nil), true)
	if !truncated || len(diagnostics) != commandMaxDiagnostics || len(limited.seen) > commandMaxDiagnostics {
		t.Fatalf("diagnostics not bounded: %d, %t", len(diagnostics), truncated)
	}
}

func TestCommandVerificationKindRecognizesStaticInvocations(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		kind string
	}{
		{[]string{"npm", "--prefix", "web", "test"}, "test"},
		{[]string{"npm", "--prefix=web", "run", "build"}, "build"},
		{[]string{"pnpm", "-C", "web", "test"}, "test"},
		{[]string{"yarn", "--cwd", "web", "run", "lint"}, "lint"},
		{[]string{"bun", "--cwd=web", "run", "check"}, "check"},
		{[]string{"npx", "tsc", "--noEmit"}, "check"},
		{[]string{"npx", "--yes", "eslint", "."}, "lint"},
		{[]string{"node", "--test", "test/example.test.js"}, "test"},
		{[]string{"node", "--experimental-strip-types", "--test", "test/example.test.ts"}, "test"},
		{[]string{"npm", "--prefix"}, ""},
		{[]string{"npm", "--unknown", "test"}, ""},
		{[]string{"npm", "run", "test", "--if-present"}, ""},
		{[]string{"npm", "run", "test", "--dry-run"}, ""},
		{[]string{"npm", "--version"}, ""},
		{[]string{"tsc", "--version"}, ""},
		{[]string{"node", "--help", "--test"}, ""},
		{[]string{"node", "script.js", "--test"}, ""},
		{[]string{"node", "-e", "--test"}, ""},
		{[]string{"npx", "--package", "typescript", "tsc"}, ""},
	} {
		if got := commandVerificationKind(tc.argv); got != tc.kind {
			t.Errorf("%q: kind=%q, want %q", tc.argv, got, tc.kind)
		}
	}
}

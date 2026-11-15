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
		{"go explicit json", "go test -json=true ./...", empty, "no_tests"},
		{"go disabled json", "go test -json -json=false ./...", "?   p [no test files]\n", "no_tests"},
		{"go reenabled json", "go test -json=false -json=true ./...", skipped, "all_skipped"},
		{"go json flag as pattern", "go test -run -json ./...", "?   p [no test files]\n", "no_tests"},
		{"go list flag as pattern", "go test -run -list ./...", "?   p [no test files]\n", "no_tests"},
		{"go test prefixed pattern", "go test -test.run -json ./...", "?   p [no test files]\n", "no_tests"},
		{"go forwarded json", "go test ./... -args -json", "?   p [no test files]\n", "no_tests"},
		{"go terminated flags", "go test ./... -- -json -list Test", "?   p [no test files]\n", "no_tests"},
		{"go positional arguments", "go test ./... -v arg -json", "?   p [no test files]\n", "no_tests"},
		{"go skipped", "go test -json ./...", skipped, "all_skipped"},
		{"go mixed", "go test -json ./...", empty + skipped + ran, "ran"},
		{"go incomplete", "go test -json ./...", jsonEvents(map[string]string{"Action": "start", "Package": "p"}), "unknown"},
		{"go corrupt", "go test -json ./...", empty + "{invalid\n", "unknown"},
		{"go plain no matches", "go test -run Missing ./...", "ok\tp\t0.01s [no tests to run]\n", "no_tests"},
		{"go plain no files", "go test ./...", "?   p [no test files]\n", "no_tests"},
		{"go plain mixed", "go test ./...", "ok p 0.01s [no tests to run]\nok q 0.02s\n", "unknown"},
		{"go verbose skip", "go test -v ./...", "--- SKIP: TestSkip (0.00s)\nok p 0.01s\n", "all_skipped"},
		{"go list", "go test -list Test ./...", "TestOne\nok p 0.01s\n", "collected"},
		{"go list reset", "go test -list Test -list= ./...", "--- PASS: TestOne (0.00s)\nok p 0.01s\n", "ran"},
		{"pytest collect", "python3 -m pytest --collect-only", "1 test collected\n", "collected"},
		{"tap empty", "node --test", "TAP version 13\n1..0\n" + tapSummary(0, 0, 0), "no_tests"},
		{"tap with preload", "node --import tsx --test", "TAP version 13\n1..0\n" + tapSummary(0, 0, 0), "no_tests"},
		{"tap wrapper", "node --test", "TAP version 13\n" + wrapper + tapSummary(1, 1, 0), "no_tests"},
		{"tap wrapper and real", "node --test", "TAP version 13\n" + wrapper + "ok 2 - real test\n" + tapSummary(2, 2, 0), "ran"},
		{"tap skips", "npm test", "TAP version 13\nok 1 - skipped # SKIP\n" + tapSummary(1, 0, 1), "all_skipped"},
		{"tap mixed", "npm test", "TAP version 13\nok 1 - skipped # SKIP\nok 2 - real\n" + tapSummary(2, 1, 1), "ran"},
		{"nested empty plan", "node --test", "TAP version 13\n    1..0\nok 1 - actual\n" + tapSummary(1, 1, 0), "ran"},
		{"tap incomplete", "node --test", "TAP version 13\n1..0\n# tests 0\n", "unknown"},
		{"unknown reporter", "npm test", "done\n", "unknown"},
		{"vitest unknown reporter", "vitest run", "done\n", "unknown"},
		{"jest empty without evidence", "jest --passWithNoTests", "No tests found, exiting with code 0\n", "unknown"},
		{"vitest TAP", "npx vitest run --reporter=tap", "TAP version 13\nok 1 - real test\n" + tapSummary(1, 1, 0), "ran"},
		{"package exec TAP", "pnpm exec vitest run", "TAP version 13\nok 1 - skipped # SKIP\n" + tapSummary(1, 0, 1), "all_skipped"},
	} {
		for _, stream := range []string{"stdout", "stderr"} {
			t.Run(tc.name+"/"+stream, func(t *testing.T) {
				args := commandRunArgs{Command: tc.command}
				stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
				if stdout.diagnostics == nil {
					t.Fatal("test command was not classified")
				}
				output := stdout
				if stream == "stderr" {
					output = stderr
				}
				for _, b := range []byte(tc.output) {
					output.Write([]byte{b})
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
	e := newCommandTestExecution([]string{"go", "test", "-v"})
	e.line("--- SKIP: TestSkip (0.00s)")
	e.incomplete = true
	if e.status() != "unknown" {
		t.Fatal("dropped output claimed all tests skipped")
	}
}

func TestCommandTestExecutionAcrossStreams(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell does not emit POSIX verification metadata")
	}
	root := t.TempDir()
	tapEmpty := "TAP version 13\n1..0\n# tests 0\n# pass 0\n# fail 0\n# cancelled 0\n# skipped 0\n# todo 0\n"
	tapSkip := "TAP version 13\nok 1 - later # SKIP\n# tests 1\n# pass 0\n# fail 0\n# cancelled 0\n# skipped 1\n# todo 0"
	goEmpty := "{\"Action\":\"pass\",\"Package\":\"p\"}\n"
	for _, tc := range []struct {
		name, command, stdout, stderr, want string
	}{
		{"noise beside report", "node --test", "ordinary progress\n", tapEmpty, "no_tests"},
		{"empty and skipped", "node --test", tapEmpty, tapSkip, "all_skipped"},
		{"incomplete reporter", "node --test", tapEmpty, "TAP version 13\n# tests 0\n", "unknown"},
		{"dropped reporter", "node --test", tapEmpty, strings.Repeat("x", commandDiagnosticLineBytes+1), "unknown"},
		{"go events in both streams", "go test -json", "{\"Action\":\"skip\",\"Package\":\"p\",\"Test\":\"TestSkip\"}\n", goEmpty, "all_skipped"},
		{"go incomplete second package", "go test -json", goEmpty, "{\"Action\":\"start\",\"Package\":\"q\"}\n", "unknown"},
		{"go corrupt second stream", "go test -json", goEmpty, "{invalid\n", "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := commandRunArgs{Command: tc.command}
			stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
			done := make(chan struct{}, 2)
			for output, text := range map[*truncatingBuffer]string{stdout: tc.stdout, stderr: tc.stderr} {
				go func() {
					for _, b := range []byte(text) {
						output.Write([]byte{b})
					}
					done <- struct{}{}
				}()
			}
			<-done
			<-done
			fields := decodeToolResult(t, commandResult(Result{}, args, "sh", root, []string{root}, nil, 0, stdout, stderr, false, false, 0, "", nil))
			if fields["testExecutionStatus"] != tc.want {
				t.Fatalf("incorrect combined evidence: %+v", fields)
			}
		})
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
		{"go test -json=true ./...", goOutput, "TestValue", "pkg/check_test.go"},
		{"node --test --test-reporter=tap", "TAP version 13\nnot ok 1 - value assertion\n  ---\n  location: 'check.test.js:1:3'\n  error: |-\n    Expected values to match\n    多行断言\n  expected: 2\n  actual: 1\n  code: ERR_ASSERTION\n  ...\nnot ok 2 - deferred # TODO later\n", "value assertion", "check.test.js"},
	} {
		for _, stream := range []string{"stdout", "stderr", "both"} {
			t.Run(tc.command+"/"+stream, func(t *testing.T) {
				args := commandRunArgs{Command: tc.command}
				stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
				noise := strings.Repeat("progress\n", commandOutputLimitBytes)
				for name, output := range map[string]*truncatingBuffer{"stdout": stdout, "stderr": stderr} {
					if stream != "both" && stream != name {
						continue
					}
					output.Write([]byte(noise))
					for _, b := range []byte(tc.output) {
						output.Write([]byte{b})
					}
					output.Write([]byte(noise))
				}
				result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 1, stdout, stderr, false, false, 0, "non_zero_exit", nil)
				var got struct {
					TestFailures          []commandTestFailure
					TestFailuresTruncated bool
					StdoutTruncated       bool
					StderrTruncated       bool
					DiagnosticCount       int
				}
				if err := json.Unmarshal([]byte(result.Content), &got); err != nil {
					t.Fatal(err)
				}
				if got.StdoutTruncated != (stream != "stderr") || got.StderrTruncated != (stream != "stdout") || got.TestFailuresTruncated || got.DiagnosticCount != 0 || len(got.TestFailures) != 1 || got.TestFailures[0].Name != tc.name || got.TestFailures[0].RelativePath != tc.path || got.TestFailures[0].Message == "" {
					t.Fatalf("%s: %+v", tc.command, got)
				}
				for _, toolName := range []string{CommandRun, CommandSession} {
					result.Name = toolName
					preview := ModelResultContent(toolName, true, result.Content, "turn", "command", true)
					if !strings.Contains(preview, tc.name) || !strings.Contains(preview, "testFailureCount") {
						t.Fatalf("preview lost failure: %s", preview)
					}
				}
			})
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
	left := newCommandTestCollector(nil, t.TempDir(), nil)
	right := newCommandTestCollector(nil, t.TempDir(), nil)
	for i := 0; i < commandMaxTestFailures/2+1; i++ {
		left.add(commandTestFailure{Name: fmt.Sprintf("left-%d", i)})
		right.add(commandTestFailure{Name: fmt.Sprintf("right-%d", i)})
	}
	_, failures, truncated := finishCommandTests(left, right)
	if !truncated || len(failures) != commandMaxTestFailures {
		t.Fatalf("combined failure limit ignored: %d, %t", len(failures), truncated)
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
	wantVerification := "failed"
	if runtime.GOOS == "windows" {
		wantVerification = "" // Collect diagnostics without claiming POSIX verification semantics.
	}
	if !payload.StdoutTruncated || strings.Contains(payload.Stdout, "未定义") || payload.ExitCode != 1 || payload.DiagnosticCount != 1 || payload.VerificationStatus != wantVerification {
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
		{[]string{"vitest", "run"}, "test"},
		{[]string{"./node_modules/.bin/vitest", "related", "src/file.ts", "--run"}, "test"},
		{[]string{"jest", "--runInBand"}, "test"},
		{[]string{"npx", "--yes", "vitest", "run"}, "test"},
		{[]string{"pnpm", "exec", "vitest", "run"}, "test"},
		{[]string{"npm", "exec", "--", "jest", "--runInBand"}, "test"},
		{[]string{"yarn", "exec", "jest"}, "test"},
		{[]string{"bun", "x", "vitest", "run"}, "test"},
		{[]string{"vitest", "list"}, ""},
		{[]string{"vitest", "init"}, ""},
		{[]string{"vitest", "bench"}, ""},
		{[]string{"vitest", "--listTags=json"}, ""},
		{[]string{"vitest", "--clear-cache"}, ""},
		{[]string{"jest", "--listTests"}, ""},
		{[]string{"jest", "--showConfig"}, ""},
		{[]string{"jest", "--clearCache"}, ""},
		{[]string{"npx", "jest", "--list-tests=true"}, ""},
		{[]string{"pnpm", "exec", "vitest", "--help"}, ""},
		{[]string{"npm", "exec", "--package", "jest", "jest"}, ""},
		{[]string{"jest", "--listTests=false"}, "test"},
		{[]string{"node", "--test", "test/example.test.js"}, "test"},
		{[]string{"node", "--experimental-strip-types", "--test", "test/example.test.ts"}, "test"},
		{[]string{"node", "--import", "tsx", "--test", "test/example.test.ts"}, "test"},
		{[]string{"node", "-r", "ts-node/register", "--test"}, "test"},
		{[]string{"node", "--loader", "tsx", "--test"}, "test"},
		{[]string{"node", "--test-name-pattern", "--help", "--test"}, "test"},
		{[]string{"node", "--test-reporter", "tap", "--test-reporter-destination", "stderr", "--test"}, "test"},
		{[]string{"node", "--test-name-pattern", "--test", "script.js"}, ""},
		{[]string{"node", "--import", "--test", "script.js"}, ""},
		{[]string{"node", "--unknown", "--test"}, ""},
		{[]string{"node", "--test", "--import"}, ""},
		{[]string{"node", "--run", "test", "--test"}, ""},
		{[]string{"node", "--run=test", "--test"}, ""},
		{[]string{"node", "--", "--test"}, ""},
		{[]string{"node", "--test", "--", "test/example.test.js"}, "test"},
		{[]string{"npm", "--prefix"}, ""},
		{[]string{"npm", "--unknown", "test"}, ""},
		{[]string{"npm", "run", "test", "--if-present"}, ""},
		{[]string{"npm", "run", "test", "--dry-run"}, ""},
		{[]string{"npm", "--version"}, ""},
		{[]string{"tsc", "--version"}, ""},
		{[]string{"tsc", "--noEmit"}, "check"},
		{[]string{"tsc", "--listFiles", "--noEmit"}, "check"},
		{[]string{"tsc", "--showConfig", "false", "--noEmit"}, "check"},
		{[]string{"tsc", "--showConfig", "--showConfig", "false"}, "check"},
		{[]string{"tsc", "--build", "--dry", "false"}, "check"},
		{[]string{"tsc", "--build", "--force"}, "check"},
		{[]string{"tsc", "--noEmit", "--noCheck"}, ""},
		{[]string{"tsc", "--noCheck", "true"}, ""},
		{[]string{"tsc", "--noCheck", "false"}, "check"},
		{[]string{"tsc", "--noCheck", "--noCheck", "false"}, "check"},
		{[]string{"tsc", "--noCheck", "false", "--noCheck"}, ""},
		{[]string{"npx", "tsc", "--noEmit", "--noCheck"}, ""},
		{[]string{"pnpm", "exec", "tsc", "--noCheck", "false"}, "check"},
		{[]string{"make", "build", "-n"}, ""},
		{[]string{"make", "build", "-kn"}, ""},
		{[]string{"make", "build", "--dry-run"}, ""},
		{[]string{"make", "build", "--just-print"}, ""},
		{[]string{"make", "build", "--recon"}, ""},
		{[]string{"make", "build", "-f", "-n"}, "build"},
		{[]string{"make", "build", "-f-n"}, "build"},
		{[]string{"make", "build", "--file", "-n"}, "build"},
		{[]string{"make", "build", "--file=-n"}, "build"},
		{[]string{"make", "build", "-I", "-n"}, "build"},
		{[]string{"make", "build", "--", "-n"}, "build"},
		{[]string{"make", "build", "FLAGS=-n"}, "build"},
		{[]string{"make", "build", "-f", "Makefile", "-n"}, ""},
		{[]string{"make", "build", "-t"}, ""},
		{[]string{"make", "build", "-kt"}, ""},
		{[]string{"make", "build", "--touch"}, ""},
		{[]string{"make", "build", "-q"}, ""},
		{[]string{"make", "build", "--question"}, ""},
		{[]string{"make", "build", "-f", "-t"}, "build"},
		{[]string{"make", "build", "-ft"}, "build"},
		{[]string{"make", "build", "--file", "--touch"}, "build"},
		{[]string{"make", "build", "--file=--touch"}, "build"},
		{[]string{"make", "build", "--", "-t"}, "build"},
		{[]string{"make", "build", "FLAGS=-t"}, "build"},
		{[]string{"make", "build", "-Otarget"}, "build"},
		{[]string{"make", "build", "-Onone"}, "build"},
		{[]string{"go", "build", "-n=false", "./..."}, "build"},
		{[]string{"go", "build", "-n", "-n=false", "./..."}, "build"},
		{[]string{"go", "build", "-ldflags", "-n", "./..."}, "build"},
		{[]string{"go", "vet", "./..."}, "lint"},
		{[]string{"eslint", "--fix-dry-run", "src"}, "lint"},
		{[]string{"ruff", "check", "--statistics", "."}, "lint"},
		{[]string{"golangci-lint", "run", "--fix"}, "lint"},
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

func TestCommandNonExecutingModesDoNotClaimVerification(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell does not emit POSIX verification metadata")
	}
	root := t.TempDir()
	for _, command := range []string{
		"tsc --showConfig",
		"tsc --showconfig",
		"tsc --listFilesOnly",
		"tsc --init",
		"tsc --all",
		"tsc --build --dry",
		"tsc -b --clean",
		"tsc --showConfig false --listFilesOnly",
		"tsc --noEmit --noCheck",
		"tsc --noCheck false --noCheck",
		"npx tsc --showConfig",
		"npx tsc --noCheck",
		"pnpm exec tsc --listFilesOnly",
		"pnpm exec tsc --noCheck",
		"make build -n",
		"make build --dry-run",
		"make build -t",
		"make build -kt",
		"make build --touch",
		"make build -q",
		"make build --question",
		"go build -n ./...",
		"go build -n=true ./...",
		"go build --n ./...",
		"go build -n=false -n ./...",
		"go vet -n ./...",
		"eslint --print-config src/app.js",
		"eslint --print-config=src/app.js",
		"eslint --init",
		"eslint --env-info",
		"eslint --inspect-config",
		"eslint --mcp",
		"eslint -v",
		"ruff check --show-files .",
		"ruff check --show-settings app.py",
		"ruff rule E501",
		"ruff config",
		"ruff version",
		"ruff clean",
		"golangci-lint linters",
		"golangci-lint config path",
		"golangci-lint config verify",
		"golangci-lint version",
	} {
		t.Run(command, func(t *testing.T) {
			args := commandRunArgs{Command: command}
			stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
			// Exercise the bounded model view as well as the canonical result:
			// informational output must never gain a verification conclusion.
			stdout.Write([]byte(strings.Repeat("configuration information\n", 1000)))
			result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 0, stdout, stderr, false, false, 0, "", nil)
			for _, content := range []string{result.Content, ModelResultContent(CommandRun, result.Ok, result.Content, "turn", "call", true)} {
				var fields map[string]any
				if err := json.Unmarshal([]byte(content), &fields); err != nil {
					t.Fatal(err)
				}
				if fields["ok"] != true || fields["exitCode"] != float64(0) {
					t.Fatalf("process completion changed: %s", content)
				}
				if _, exists := fields["verificationKind"]; exists {
					t.Fatalf("non-executing invocation claimed a verification kind: %s", content)
				}
				if _, exists := fields["verificationStatus"]; exists {
					t.Fatalf("non-executing invocation claimed a verification outcome: %s", content)
				}
			}
		})
	}
}

func TestCommandVerificationIgnoresFailureOnlyWhenRequested(t *testing.T) {
	for _, tc := range []struct {
		argv []string
		want bool
	}{
		{[]string{"ruff", "check", "--exit-zero", "."}, true},
		{[]string{"ruff", "check", "-e", "."}, true},
		{[]string{"npx", "--yes", "ruff", "check", "--exit-zero"}, true},
		{[]string{"pnpm", "--dir", "web", "exec", "ruff", "check", "--exit-zero"}, true},
		{[]string{"npm", "exec", "--", "ruff", "check", "--exit-zero"}, true},
		{[]string{"yarn", "exec", "ruff", "check", "--exit-zero"}, true},
		{[]string{"bun", "x", "ruff", "check", "--exit-zero"}, true},
		{[]string{"ruff", "check", "."}, false},
		{[]string{"ruff", "check", "--", "--exit-zero"}, false},
		{[]string{"ruff", "check", "--stdin-filename=--exit-zero", "-"}, false},
		{[]string{"ruff", "format", "--exit-zero"}, false},
		{[]string{"npm", "run", "lint", "--", "--exit-zero"}, false},
		{[]string{"eslint", "--exit-zero"}, false},
		{[]string{"make", "build", "-i"}, true},
		{[]string{"make", "build", "-ki"}, true},
		{[]string{"make", "build", "--ignore-errors"}, true},
		{[]string{"make", "-i", "build"}, true},
		{[]string{"pnpm", "exec", "make", "build", "-i"}, true},
		{[]string{"make", "build"}, false},
		{[]string{"make", "build", "-f", "-i"}, false},
		{[]string{"make", "build", "-fi"}, false},
		{[]string{"make", "build", "--file", "--ignore-errors"}, false},
		{[]string{"make", "build", "--file=--ignore-errors"}, false},
		{[]string{"make", "build", "--", "-i"}, false},
		{[]string{"make", "build", "FLAGS=-i"}, false},
		{[]string{"make", "build", "-Oline"}, false},
	} {
		if got := commandVerificationIgnoresFailure(tc.argv); got != tc.want {
			t.Errorf("%q: ignoresFailure=%v, want %v", tc.argv, got, tc.want)
		}
	}
}

func TestCommandMakeIgnoredErrorsDoNotClaimPass(t *testing.T) {
	root := t.TempDir()
	for _, command := range []string{"make build -i", "make build -ki", "make build --ignore-errors", "pnpm exec make build -i"} {
		t.Run(command, func(t *testing.T) {
			args := commandRunArgs{Command: command}
			stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
			stderr.Write([]byte("make: [Makefile:2: build] Error 1 (ignored)\n"))
			result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 0, stdout, stderr, false, false, 0, "", nil)
			for _, content := range []string{result.Content, ModelResultContent(CommandRun, result.Ok, result.Content, "turn", "call", true)} {
				var fields map[string]any
				if err := json.Unmarshal([]byte(content), &fields); err != nil {
					t.Fatal(err)
				}
				if fields["ok"] != true || fields["exitCode"] != float64(0) || !strings.Contains(fields["stderr"].(string), "Error 1 (ignored)") {
					t.Fatalf("process completion or failure output changed: %s", content)
				}
				if runtime.GOOS == "windows" {
					if fields["verificationStatus"] != nil || fields["verificationKind"] != nil {
						t.Fatalf("PowerShell gained a POSIX verification conclusion: %s", content)
					}
				} else if fields["verificationKind"] != "build" || fields["verificationStatus"] != "completed" {
					t.Fatalf("ignored recipe failures claimed a build outcome: %s", content)
				}
			}
		})
	}
}

func TestCommandRuffExitZeroPreservesDiagnosticsWithoutClaimingPass(t *testing.T) {
	root := t.TempDir()
	writePatchTestFile(t, filepath.Join(root, "app.py"), "import os\n")
	for _, command := range []string{"ruff check --exit-zero .", "ruff check -e .", "pnpm exec ruff check --exit-zero ."} {
		t.Run(command, func(t *testing.T) {
			args := commandRunArgs{Command: command}
			stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
			stdout.Write([]byte("app.py:1:1: F401 unused import\n"))
			result := commandResult(Result{}, args, "sh", root, []string{root}, nil, 0, stdout, stderr, false, false, 0, "", nil)
			for _, content := range []string{result.Content, ModelResultContent(CommandRun, result.Ok, result.Content, "turn", "call", true)} {
				var fields map[string]any
				if err := json.Unmarshal([]byte(content), &fields); err != nil {
					t.Fatal(err)
				}
				if fields["ok"] != true || fields["exitCode"] != float64(0) || fields["diagnosticCount"] != float64(1) {
					t.Fatalf("process status or lint diagnostic lost: %s", content)
				}
				diagnostic := fields["diagnostics"].([]any)[0].(map[string]any)
				if diagnostic["relativePath"] != "app.py" || diagnostic["severity"] != "error" || !strings.Contains(diagnostic["message"].(string), "F401") {
					t.Fatalf("exit-zero hid lint failure evidence: %s", content)
				}
				if runtime.GOOS == "windows" {
					if fields["verificationStatus"] != nil || fields["verificationKind"] != nil {
						t.Fatalf("PowerShell gained a POSIX verification conclusion: %s", content)
					}
				} else if fields["verificationKind"] != "lint" || fields["verificationStatus"] != "completed" {
					t.Fatalf("exit-zero claimed a lint outcome: %s", content)
				}
			}
		})
	}
}

func TestWindowsCommandDiagnosticsDoNotProveVerificationSuccess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PowerShell result metadata")
	}
	root := t.TempDir()
	output := "{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"TestPass\"}\n{\"Action\":\"pass\",\"Package\":\"p\"}\n"
	for _, tc := range []struct {
		command string
		literal bool
	}{
		{`go test -json ./...`, true},
		{`go.exe test -json ./...`, true},
		{`& 'C:\Program Files\Go\bin\go.exe' test -json ./...`, true},
		{`go test -json ./...; exit 0`, false},
		{`go test -json ./... | Tee-Object log`, false},
		{`& $exe test -json ./...`, false},
	} {
		t.Run(tc.command, func(t *testing.T) {
			args := commandRunArgs{Command: tc.command}
			stdout, stderr := newCommandOutputBuffers(args, root, []string{root})
			stdout.Write([]byte(output))
			result := commandResult(Result{}, args, "pwsh", root, []string{root}, nil, 0, stdout, stderr, false, false, 0, "", nil)
			fields := decodeToolResult(t, result)
			if fields["ok"] != true || fields["exitCode"] != float64(0) || fields["stdout"] != output {
				t.Fatalf("raw process result changed: %s", result.Content)
			}
			if fields["verificationStatus"] != nil || fields["verificationKind"] != nil {
				t.Fatalf("PowerShell gained a POSIX verification conclusion: %s", result.Content)
			}
			if tc.literal {
				if fields["testExecutionStatus"] != "ran" || fields["testFailureCount"] != float64(0) || fields["diagnosticCount"] != float64(0) {
					t.Fatalf("literal command lost observed test evidence: %s", result.Content)
				}
			} else if fields["testExecutionStatus"] != nil || fields["diagnostics"] != nil || fields["testFailures"] != nil {
				t.Fatalf("complex PowerShell was assigned guessed diagnostic semantics: %s", result.Content)
			}
		})
	}
}

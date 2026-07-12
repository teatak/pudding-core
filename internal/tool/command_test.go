package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type commandPayload struct {
	OK                 bool                `json:"ok"`
	Argv               []string            `json:"argv"`
	Script             string              `json:"script"`
	Shell              string              `json:"shell"`
	CWD                string              `json:"cwd"`
	ExitCode           int                 `json:"exitCode"`
	Stdout             string              `json:"stdout"`
	Stderr             string              `json:"stderr"`
	StdoutTruncated    bool                `json:"stdoutTruncated"`
	StderrTruncated    bool                `json:"stderrTruncated"`
	TimedOut           bool                `json:"timedOut"`
	Cancelled          bool                `json:"cancelled"`
	Reason             string              `json:"reason"`
	VerificationKind   string              `json:"verificationKind"`
	VerificationStatus string              `json:"verificationStatus"`
	DiagnosticCount    int                 `json:"diagnosticCount"`
	Diagnostics        []commandDiagnostic `json:"diagnostics"`
}

func TestCommandRunUsesProjectCWDAndCapturesOutput(t *testing.T) {
	root := t.TempDir()
	cwd := filepath.Join(root, "nested")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	res := commandTestCall(context.Background(), root, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("report"),
		"cwd":   "nested",
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || !payload.OK || payload.ExitCode != 0 {
		t.Fatalf("command should succeed: result=%+v payload=%+v", res, payload)
	}
	if payload.VerificationKind != "" || payload.VerificationStatus != "" {
		t.Fatalf("ordinary command must not be marked as verification: %+v", payload)
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if payload.CWD != resolvedCWD || !strings.Contains(payload.Stdout, resolvedCWD) || !strings.Contains(payload.Stderr, "helper stderr") {
		t.Fatalf("unexpected command output: %+v", payload)
	}
}

func TestCommandRunExecutesFixedShellScript(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell syntax")
	}
	res := commandTestCall(context.Background(), t.TempDir(), map[string]any{
		"scope":  "project",
		"script": `printf "shell-out" | tr 'a-z' 'A-Z'; printf "shell-err" >&2`,
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || !payload.OK || payload.ExitCode != 0 || payload.Shell != "sh" || payload.Stdout != "SHELL-OUT" || payload.Stderr != "shell-err" {
		t.Fatalf("fixed shell script failed: result=%+v payload=%+v", res, payload)
	}
	if payload.Script == "" || len(payload.Argv) != 0 || payload.VerificationKind != "" {
		t.Fatalf("script result must preserve script without direct-command verification: %+v", payload)
	}
}

func TestCommandRunRequiresExactlyOneCommandInput(t *testing.T) {
	root := t.TempDir()
	for _, args := range []map[string]any{
		{"scope": "project"},
		{"scope": "project", "argv": commandHelperArgs("report"), "script": "printf duplicate"},
	} {
		res := commandTestCall(context.Background(), root, args)
		if res.Ok || !strings.Contains(res.Content, "exactly one of argv or script is required") {
			t.Fatalf("invalid command input must be rejected: args=%+v result=%+v", args, res)
		}
	}
}

func TestCommandRunStreamsStdoutAndStderr(t *testing.T) {
	var mu sync.Mutex
	progress := make([]Progress, 0, 2)
	ctx := WithProgressSink(context.Background(), func(item Progress) {
		mu.Lock()
		progress = append(progress, item)
		mu.Unlock()
	})
	res := commandTestCall(ctx, t.TempDir(), map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("report"),
	})
	if !res.Ok {
		t.Fatalf("command failed: %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	var stdout, stderr string
	for _, item := range progress {
		switch item.Stream {
		case ProgressStdout:
			stdout += item.Content
		case ProgressStderr:
			stderr += item.Content
		}
	}
	if stdout == "" || stderr != "helper stderr" {
		t.Fatalf("missing live command output: %+v", progress)
	}
}

func TestCommandRunRejectsCWDOutsideProject(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	res := commandTestCall(context.Background(), root, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("report"),
		"cwd":   outside,
	})
	if res.Ok || !strings.Contains(res.Content, `"reason":"path_not_authorized"`) {
		t.Fatalf("outside cwd must be rejected: %+v", res)
	}
}

func TestCommandRunReturnsExitCode(t *testing.T) {
	res := commandTestCall(context.Background(), t.TempDir(), map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("exit", "7"),
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || !payload.OK || payload.ExitCode != 7 || payload.Reason != "non_zero_exit" || !strings.Contains(payload.Stderr, "exit 7") {
		t.Fatalf("unexpected non-zero result: %+v", payload)
	}
}

func TestCommandRunUsesMinimalEnvironment(t *testing.T) {
	t.Setenv("PUDDING_COMMAND_SECRET", "do-not-inherit")
	res := commandTestCall(context.Background(), t.TempDir(), map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("report-env"),
		"env":   map[string]string{"PUDDING_VISIBLE": "visible"},
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || payload.Stdout != "visible|" {
		t.Fatalf("command environment leaked or custom env missing: %+v", payload)
	}
}

func TestCommandRunExecutesGoTest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/commandtest\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "command_test.go"), []byte("package commandtest\n\nimport \"testing\"\n\nfunc TestPass(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	res := commandTestCall(context.Background(), root, map[string]any{
		"scope":      "project",
		"argv":       []string{"go", "test", "./..."},
		"timeout_ms": 30000,
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || payload.ExitCode != 0 || !strings.Contains(payload.Stdout, "example.com/commandtest") {
		t.Fatalf("go test command failed: %+v", payload)
	}
	if payload.VerificationKind != "test" || payload.VerificationStatus != "passed" || payload.DiagnosticCount != 0 {
		t.Fatalf("go test verification metadata missing: %+v", payload)
	}
}

func TestCommandRunParsesFailedGoDiagnostics(t *testing.T) {
	root := t.TempDir()
	writeCommandTestFile(t, root, "go.mod", "module example.com/commanddiagnostic\n\ngo 1.24\n")
	writeCommandTestFile(t, root, "broken.go", "package commanddiagnostic\n\nfunc broken() { missingSymbol() }\n")
	res := commandTestCall(context.Background(), root, map[string]any{
		"scope":      "project",
		"argv":       []string{"go", "test", "./..."},
		"timeout_ms": 30000,
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || payload.ExitCode == 0 || payload.VerificationKind != "test" || payload.VerificationStatus != "failed" {
		t.Fatalf("failed go test metadata missing: %+v", payload)
	}
	if payload.DiagnosticCount == 0 || len(payload.Diagnostics) == 0 {
		t.Fatalf("go diagnostic not parsed: stdout=%q stderr=%q", payload.Stdout, payload.Stderr)
	}
	diagnostic := payload.Diagnostics[0]
	if diagnostic.RelativePath != "broken.go" || diagnostic.Line != 3 || diagnostic.Severity != "error" || !strings.Contains(diagnostic.Message, "missingSymbol") || !strings.Contains(diagnostic.Excerpt, "func broken") {
		t.Fatalf("unexpected go diagnostic: %+v", diagnostic)
	}
}

func TestParseCommandDiagnosticsSupportsTypeScriptAndESLint(t *testing.T) {
	root := t.TempDir()
	writeCommandTestFile(t, root, "src/app.ts", "const first = 1;\nconst value: string = 2;\nconsole.log(value);\n")
	typescript := parseCommandDiagnostics("src/app.ts(2,7): error TS2322: Type 'number' is not assignable to type 'string'.", "", root, []string{root}, true)
	if len(typescript) != 1 || typescript[0].RelativePath != "src/app.ts" || typescript[0].Line != 2 || typescript[0].Column != 7 || typescript[0].Code != "TS2322" || typescript[0].Source != "typescript" {
		t.Fatalf("unexpected TypeScript diagnostics: %+v", typescript)
	}
	eslintOutput := filepath.Join(root, "src", "app.ts") + "\n  2:7  warning  Unexpected any  @typescript-eslint/no-explicit-any\n"
	eslint := parseCommandDiagnostics(eslintOutput, "", root, []string{root}, false)
	if len(eslint) != 1 || eslint[0].Severity != "warning" || eslint[0].Code != "@typescript-eslint/no-explicit-any" || eslint[0].Source != "eslint" {
		t.Fatalf("unexpected ESLint diagnostics: %+v", eslint)
	}
}

func TestCommandRunTruncatesStdoutAndStderr(t *testing.T) {
	res := commandTestCall(context.Background(), t.TempDir(), map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("flood", strconv.Itoa(commandOutputLimitBytes*2)),
	})
	payload := decodeCommandPayload(t, res)
	if !res.Ok || !payload.StdoutTruncated || !payload.StderrTruncated {
		t.Fatalf("large output must be truncated: %+v", payload)
	}
	if !strings.Contains(payload.Stdout, "output truncated") || !strings.HasPrefix(payload.Stdout, "stdout-head") || !strings.HasSuffix(payload.Stdout, "stdout-tail") {
		t.Fatalf("stdout should preserve head and tail: %q", payload.Stdout)
	}
	if !strings.Contains(payload.Stderr, "output truncated") || !strings.HasPrefix(payload.Stderr, "stderr-head") || !strings.HasSuffix(payload.Stderr, "stderr-tail") {
		t.Fatalf("stderr should preserve head and tail: %q", payload.Stderr)
	}
}

func TestCommandRunTimeoutKillsProcessTree(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "child-finished")
	res := commandTestCall(context.Background(), root, map[string]any{
		"scope":      "project",
		"argv":       commandHelperArgs("spawn-child", marker),
		"timeout_ms": 150,
	})
	payload := decodeCommandPayload(t, res)
	if res.Ok || !payload.TimedOut || payload.Reason != "timed_out" {
		t.Fatalf("command should time out: %+v", payload)
	}
	time.Sleep(900 * time.Millisecond)
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("child process survived group termination: %v", err)
	}
}

func TestCommandRunStopsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan Result, 1)
	root := t.TempDir()
	go func() {
		done <- commandTestCall(ctx, root, map[string]any{
			"scope": "project",
			"argv":  commandHelperArgs("sleep", "5000"),
		})
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case res := <-done:
		payload := decodeCommandPayload(t, res)
		if res.Ok || !payload.Cancelled || payload.Reason != "cancelled" {
			t.Fatalf("command should be cancelled: %+v", payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled command did not stop")
	}
}

func TestCommandHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	args := os.Args[separator+1:]
	switch args[0] {
	case "report":
		cwd, _ := os.Getwd()
		fmt.Fprint(os.Stdout, cwd)
		fmt.Fprint(os.Stderr, "helper stderr")
	case "report-env":
		fmt.Fprintf(os.Stdout, "%s|%s", os.Getenv("PUDDING_VISIBLE"), os.Getenv("PUDDING_COMMAND_SECRET"))
	case "exit":
		code, _ := strconv.Atoi(args[1])
		fmt.Fprintf(os.Stderr, "exit %d", code)
		os.Exit(code)
	case "flood":
		size, _ := strconv.Atoi(args[1])
		fmt.Fprint(os.Stdout, "stdout-head"+strings.Repeat("o", size)+"stdout-tail")
		fmt.Fprint(os.Stderr, "stderr-head"+strings.Repeat("e", size)+"stderr-tail")
	case "sleep":
		ms, _ := strconv.Atoi(args[1])
		time.Sleep(time.Duration(ms) * time.Millisecond)
	case "background-stream":
		fmt.Fprintln(os.Stdout, "ready")
		fmt.Fprintln(os.Stderr, "warning")
		time.Sleep(500 * time.Millisecond)
		fmt.Fprintln(os.Stdout, "done")
	case "http-server":
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, "LISTEN "+listener.Addr().String())
		_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}))
	case "spawn-child":
		child := exec.Command(os.Args[0], "-test.run=^TestCommandHelperProcess$", "--", "write-after", args[1])
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		_ = child.Wait()
	case "write-after":
		time.Sleep(700 * time.Millisecond)
		_ = os.WriteFile(args[1], []byte("finished"), 0o600)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func commandTestCall(ctx context.Context, root string, args map[string]any) Result {
	raw, _ := json.Marshal(args)
	return NewBuiltinRunner().Call(ctx, Call{
		CallID:      "call_command",
		Name:        CommandRun,
		Args:        raw,
		ProjectDirs: []string{root},
	})
}

func writeCommandTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func commandHelperArgs(mode string, args ...string) []string {
	executable, _ := os.Executable()
	return append([]string{executable, "-test.run=^TestCommandHelperProcess$", "--", mode}, args...)
}

func decodeCommandPayload(t *testing.T, res Result) commandPayload {
	t.Helper()
	var payload commandPayload
	if err := json.Unmarshal([]byte(res.Content), &payload); err != nil {
		t.Fatalf("decode command payload: %v content=%q", err, res.Content)
	}
	return payload
}

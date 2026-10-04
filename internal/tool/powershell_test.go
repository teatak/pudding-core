package tool

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestPowerShellCancelledPreflightIsNotMissingRuntime(t *testing.T) {
	executable := testPowerShell(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := validatePowerShell(ctx, executable, "Write-Output ok", runtime.GOOS)
	result := commandShellFailure(Result{}, err)
	payload := decodeCommandPayload(t, result)
	if result.Ok || !payload.Cancelled || payload.Reason != "cancelled" {
		t.Fatalf("cancelled parser reported a runtime installation problem: %+v", result)
	}
}

func TestWindowsCommandInputDoesNotUsePOSIXGrammar(t *testing.T) {
	for _, command := range []string{
		`& 'C:\Program Files\Go\bin\go.exe' version`,
		`& .\build.ps1`,
		`if ($true) { Write-Output '你好' }`,
		`@(1, 2) | ForEach-Object { $_ + 1 }`,
		`$env:TEST_VALUE = 'hello'; Write-Output $env:TEST_VALUE`,
	} {
		args := commandRunArgs{Command: command, Execution: CommandExecutionHost, HostAccessReason: "Windows has no project sandbox"}
		if err := validateCommandInputForOS(args, "windows"); err != nil {
			t.Errorf("PowerShell input rejected by POSIX grammar: %s: %v", command, err)
		}
	}
}

func TestWindowsCommandRiskNeverUsesPOSIXAllowlisting(t *testing.T) {
	root := t.TempDir()
	for _, command := range []string{`git status`, `Remove-Item -Recurse .\data`, `& .\build.ps1`, `Write-Output $(Get-Content $env:USERPROFILE\secret)`} {
		args := commandRunArgs{Command: command, CWD: root, Execution: CommandExecutionHost, HostAccessReason: "Windows host command"}
		risk := windowsCommandRisk(args, []string{root})
		if risk.Class != RiskClassCommand || risk.LowRisk || !reflect.DeepEqual(risk.ApprovalReasons, []string{"host_execution"}) || !risk.hostAccessRequired || len(risk.requiredProjectPaths) != 0 {
			t.Fatalf("unsafe Windows risk: %+v", risk)
		}
	}
	// The pre-approval boundary rejects sandbox execution without invoking a shell.
	call := Call{CallID: "call", Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"git status"}`)}
	if res, failed := CommandBoundaryFailure(call, windowsCommandRisk(commandRunArgs{Command: "git status"}, []string{root})); !failed || !strings.Contains(res.Content, "host_access_required") {
		t.Fatalf("sandbox request did not return host guidance: %+v", res)
	}
}

func TestWindowsPowerShellInputLimits(t *testing.T) {
	for _, args := range []commandRunArgs{
		{Command: "Write-Output ok", Execution: CommandExecutionHost},
		{Command: "Write-Output ok", Execution: CommandExecutionHost, HostAccessReason: "test", Background: true, TTY: true},
		{Command: strings.Repeat("界", 20000), Execution: CommandExecutionHost, HostAccessReason: "test"},
	} {
		if err := validateCommandInputForOS(args, "windows"); err == nil {
			t.Fatal("invalid Windows input accepted")
		}
	}
}

func TestPowerShellDoesNotFallback(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := preparePowerShell(context.Background(), "Write-Output ok", "windows")
	if err == nil || !strings.Contains(err.Error(), "No fallback") {
		t.Fatalf("missing pwsh: %v", err)
	}
}

func testPowerShell(t *testing.T) string {
	t.Helper()
	name := "pwsh"
	if runtime.GOOS == "windows" {
		name = "pwsh.exe"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		if runtime.GOOS == "windows" {
			t.Fatal("Windows CI requires PowerShell 7 x64: ", err)
		}
		t.Skip("optional native PowerShell parser tests: pwsh is not installed")
	}
	return path
}

func psQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func TestPowerShellParserDoesNotExecuteInput(t *testing.T) {
	executable := testPowerShell(t)
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	for _, command := range []string{
		"Set-Content -LiteralPath " + psQuote(marker) + " -Value 'executed'",
		`& 'C:\Program Files\Go\bin\go.exe' version`,
		`if ($true) { @(1,2) | ForEach-Object { $_ } }`,
		`Write-Output '你好 && &'; Write-Output '😀'`,
		`Write-Output ok && Write-Output next || Write-Output failed`,
	} {
		if err := validatePowerShell(context.Background(), executable, command, runtime.GOOS); err != nil {
			t.Fatalf("%s: %v", command, err)
		}
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("syntax check executed input")
	}
	for _, command := range []string{`Write-Output 'unterminated`, `Write-Output ok &`, `if ($true) { Write-Output ok & }`, `Write-Output ok && Write-Output next &`} {
		if err := validatePowerShell(context.Background(), executable, command, runtime.GOOS); err == nil {
			t.Fatalf("invalid/unmanaged background input accepted: %s", command)
		}
	}
}

func TestPowerShellUTF8ArgumentsAndExitCodes(t *testing.T) {
	executable := testPowerShell(t)
	helper, _ := os.Executable()
	native := "& " + psQuote(helper) + " '-test.run=^TestCommandHelperProcess$' '--' 'exit' '7'"
	for _, tc := range []struct {
		command string
		code    int
		output  string
	}{
		{`Write-Output '你好 😀'`, 0, "你好 😀"},
		{`exit 7`, 7, ""},
		{native, 7, "exit 7"},
		{native + `; Write-Output 'recovered'`, 0, "recovered"},
		{`throw 'expected failure'`, 1, "expected failure"},
		{`Write-Error 'expected error'`, 1, "expected error"},
	} {
		cmd := exec.Command(executable, powerShellArgs(powerShellScript(tc.command))...)
		output, _ := cmd.CombinedOutput()
		if cmd.ProcessState.ExitCode() != tc.code || !strings.Contains(string(output), tc.output) {
			t.Errorf("%s: exit=%d output=%s", tc.command, cmd.ProcessState.ExitCode(), output)
		}
	}
	want := []string{"你好", `{"name":"a b"}`, `C:\path with space\`, "", "one'two", "$literal"}
	command := "& " + psQuote(helper) + " '-test.run=^TestPowerShellArgvHelper$' '--' '--ps-argv'"
	for _, arg := range want {
		command += " " + psQuote(arg)
	}
	cmd := exec.Command(executable, powerShellArgs(powerShellScript(command))...)
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(output, &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("native argv = %s, err=%v", output, err)
	}
}

func TestPowerShellArgvHelper(t *testing.T) {
	for i, arg := range os.Args {
		if arg == "--ps-argv" {
			_ = json.NewEncoder(os.Stdout).Encode(os.Args[i+1:])
			os.Exit(0)
		}
	}
}

package tool

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

//go:embed assets/powershell_validate.ps1
var powerShellValidator string

type commandShellError struct{ reason, detail string }

func (e *commandShellError) Error() string { return e.detail }

func powerShellArgs(script string) []string {
	units := utf16.Encode([]rune(script))
	bytes := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.LittleEndian.PutUint16(bytes[2*i:], unit)
	}
	return []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(bytes)}
}

func powerShellScript(command string) string {
	// Carry user text as data, not as interpolated bootstrap syntax. stdin stays
	// available for managed background processes. The original command is also
	// kept unchanged in the approval and result payloads.
	return `$ErrorActionPreference = 'Stop'
if ($PSVersionTable.PSEdition -ne 'Core' -or $PSVersionTable.PSVersion.Major -lt 7 -or ($IsWindows -and [System.Runtime.InteropServices.RuntimeInformation]::ProcessArchitecture -notin @('X64', 'Arm64'))) { throw 'PowerShell 7 (x64 or ARM64) is required on Windows' }
$OutputEncoding = [Console]::InputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$puddingSource = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString([]byte(command)) + `'))
$puddingScript = [scriptblock]::Create($puddingSource + '

if (-not $?) { if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; exit 1 }
exit 0
')
$global:LASTEXITCODE = 0
& $puddingScript
`
}

func powerShellInputLimit(command string) error {
	args := powerShellArgs(powerShellScript(command))
	if len(args[len(args)-1]) > 30000 {
		return fmt.Errorf("PowerShell command exceeds the Windows command-line limit; save a script in the project and invoke it instead")
	}
	return nil
}

// persistedPowerShellPATH is the PATH Windows gives new processes. Installing
// PowerShell 7 updates it, while this process keeps the PATH it started with.
var persistedPowerShellPATH = persistedWindowsPATH

func lookupPowerShell(goos string) (string, error) {
	if goos != "windows" {
		// Used by the optional portable PowerShell regression tests on macOS.
		return exec.LookPath("pwsh")
	}
	if executable, err := exec.LookPath("pwsh.exe"); err == nil {
		return executable, nil
	}
	for _, dir := range filepath.SplitList(persistedPowerShellPATH()) {
		candidate := filepath.Join(dir, "pwsh.exe")
		if info, err := os.Stat(candidate); err == nil && filepath.IsAbs(dir) && info.Mode().IsRegular() {
			return candidate, nil
		}
	}
	return "", exec.ErrNotFound
}

// CommandShellStatus reports whether the Windows command tool can find
// PowerShell 7. Version and architecture are still checked before each command.
type CommandShellStatus struct {
	State string `json:"state"` // installed, missing or not_required
	Path  string `json:"path,omitempty"`
}

func CurrentCommandShellStatus() CommandShellStatus {
	if runtime.GOOS != "windows" {
		return CommandShellStatus{State: "not_required"}
	}
	executable, err := lookupPowerShell(runtime.GOOS)
	if err != nil {
		return CommandShellStatus{State: "missing"}
	}
	return CommandShellStatus{State: "installed", Path: executable}
}

func preparePowerShell(ctx context.Context, command, goos string) (string, []string, error) {
	executable, err := lookupPowerShell(goos)
	if err != nil {
		return "", nil, &commandShellError{"shell_unavailable", "PowerShell 7 (x64 or ARM64) (pwsh.exe) is required on Windows; install it from Pudding's prompt or with `winget install --id Microsoft.PowerShell --exact --source winget`. No fallback to Windows PowerShell 5.1 or Git Bash."}
	}
	if err := powerShellInputLimit(command); err != nil {
		return "", nil, &commandShellError{"invalid_arguments", err.Error()}
	}
	if err := validatePowerShell(ctx, executable, command, goos); err != nil {
		return "", nil, err
	}
	return executable, powerShellArgs(powerShellScript(command)), nil
}

func validatePowerShell(ctx context.Context, executable, command, goos string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, powerShellArgs(powerShellValidator)...)
	cmd.Stdin = strings.NewReader(command)
	// Never use the model's env/cwd during pre-approval parsing. User input is
	// read by Parser.ParseInput only; profiles and scripts are not executed.
	cmd.Env, _ = commandEnvironment(nil)
	configureCommandProcess(cmd)
	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return &commandShellError{"shell_unavailable", fmt.Sprintf("PowerShell 7 preflight failed: %v", err)}
	}
	var parsed struct {
		Major      int      `json:"major"`
		Edition    string   `json:"edition"`
		Arch       string   `json:"arch"`
		Errors     []string `json:"errors"`
		Background bool     `json:"background"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		return &commandShellError{"shell_unavailable", "PowerShell returned an invalid preflight response"}
	}
	if !powerShellRuntimeSupported(parsed.Major, parsed.Edition, parsed.Arch, goos) {
		return &commandShellError{"shell_unavailable", "PowerShell 7 (x64 or ARM64) is required; Windows PowerShell 5.1 and x86 runtimes are not supported"}
	}
	if len(parsed.Errors) > 0 {
		return &commandShellError{"invalid_arguments", "invalid PowerShell command: " + strings.Join(parsed.Errors, "; ")}
	}
	if parsed.Background {
		return &commandShellError{"invalid_arguments", "PowerShell background operators are not supported; set background=true instead"}
	}
	return nil
}

func powerShellRuntimeSupported(major int, edition, arch, goos string) bool {
	return major >= 7 && edition == "Core" && (goos != "windows" || arch == "X64" || arch == "Arm64")
}

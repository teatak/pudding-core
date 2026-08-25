package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	commandDefaultTimeout   = 60 * time.Second
	commandMaxTimeout       = 10 * time.Minute
	commandMinTimeout       = 100 * time.Millisecond
	commandOutputLimitBytes = 64 << 10
	commandLiveOutputLimit  = 1 << 20
	commandLiveFlushBytes   = 8 << 10
	commandLiveFlushDelay   = 75 * time.Millisecond
	commandMaxBytes         = 64 << 10
)

type CommandExecutionMode string

const (
	CommandExecutionSandbox CommandExecutionMode = "sandbox"
	CommandExecutionHost    CommandExecutionMode = "host"
)

type commandRunArgs struct {
	Scope            string               `json:"scope"`
	Command          string               `json:"command"`
	Execution        CommandExecutionMode `json:"execution,omitempty"`
	HostAccessReason string               `json:"host_access_reason,omitempty"`
	CWD              string               `json:"cwd,omitempty"`
	Env              map[string]string    `json:"env,omitempty"`
	TimeoutMS        int                  `json:"timeout_ms,omitempty"`
	Background       bool                 `json:"background,omitempty"`
	TTY              bool                 `json:"tty,omitempty"`
}

func (r *BuiltinRunner) commandRun(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeCommandRunArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if args.Background {
		return r.commandStart(call, args)
	}

	cwd := strings.TrimSpace(args.CWD)
	if cwd == "" {
		cwd = "."
	}
	_, resolvedCWD, _, err := resolveProjectPath(call.ProjectDirs, cwd, true, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	info, err := os.Stat(resolvedCWD)
	if err != nil {
		return toolJSONError(out, "cwd_unavailable", err.Error())
	}
	if !info.IsDir() {
		return toolJSONError(out, "cwd_not_directory", "command cwd must be a directory")
	}

	timeout, err := commandTimeout(args.TimeoutMS)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	executable, commandArgs, shell := commandInvocation(args)
	env, err := commandEnvironment(args.Env)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	execution, err := r.commands.Prepare(commandSpec{
		Executable:  executable,
		Args:        commandArgs,
		CWD:         resolvedCWD,
		Env:         env,
		ProjectDirs: call.ProjectDirs,
		SandboxMode: call.CommandSandbox,
		StateKey:    call.CommandStateKey,
	})
	if err != nil {
		return toolJSONError(out, "command_prepare_failed", err.Error())
	}
	cmd := execution.Cmd
	stdout := newTruncatingBuffer(commandOutputLimitBytes)
	stderr := newTruncatingBuffer(commandOutputLimitBytes)
	stdoutWriter := newCommandProgressWriter(ctx, ProgressStdout, stdout)
	stderrWriter := newCommandProgressWriter(ctx, ProgressStderr, stderr)
	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter
	defer stdoutWriter.Close()
	defer stderrWriter.Close()

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		return commandResult(out, args, shell, resolvedCWD, call.ProjectDirs, execution, -1, stdout, stderr, false, false, time.Since(startedAt), "start_failed", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	var waitErr error
	timedOut := false
	cancelled := false
	select {
	case waitErr = <-waitCh:
	case <-runCtx.Done():
		timedOut = errors.Is(runCtx.Err(), context.DeadlineExceeded)
		cancelled = errors.Is(runCtx.Err(), context.Canceled)
		killErr := terminateCommandProcess(cmd)
		waitErr = <-waitCh
		if killErr != nil {
			waitErr = errors.Join(waitErr, killErr)
		}
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if waitErr != nil {
		exitCode = -1
	}
	reason := ""
	switch {
	case timedOut:
		reason = "timed_out"
	case cancelled:
		reason = "cancelled"
	case waitErr != nil:
		reason = "non_zero_exit"
	}
	stdoutWriter.Close()
	stderrWriter.Close()
	return commandResult(out, args, shell, resolvedCWD, call.ProjectDirs, execution, exitCode, stdout, stderr, timedOut, cancelled, time.Since(startedAt), reason, waitErr)
}

func decodeCommandRunArgs(raw json.RawMessage) (commandRunArgs, error) {
	var args commandRunArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, errors.New("command arguments must be a JSON object")
	}
	args.Scope = strings.TrimSpace(args.Scope)
	if args.Scope != managedScopeProject {
		return args, errors.New("command scope must be project")
	}
	args.Execution = CommandExecutionMode(strings.ToLower(strings.TrimSpace(string(args.Execution))))
	if args.Execution == "" {
		args.Execution = CommandExecutionSandbox
	}
	args.HostAccessReason = strings.TrimSpace(args.HostAccessReason)
	if err := validateCommandInput(args); err != nil {
		return args, err
	}
	return args, nil
}

func commandInvocation(args commandRunArgs) (string, []string, string) {
	if runtime.GOOS == "windows" {
		return "powershell.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", args.Command}, "powershell"
	}
	return "/bin/sh", []string{"-c", args.Command}, "sh"
}

func commandApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeCommandRunArgs(call.Args)
	if err != nil {
		return nil, err
	}
	details := map[string]any{
		"command":   args.Command,
		"execution": string(args.Execution),
	}
	if args.HostAccessReason != "" {
		details["hostAccessReason"] = args.HostAccessReason
	}
	if args.Background {
		details["background"] = true
	}
	if args.TTY {
		details["tty"] = true
	}
	if cwd := strings.TrimSpace(args.CWD); cwd != "" {
		details["cwd"] = cwd
	}
	if len(args.Env) > 0 {
		keys := make([]string, 0, len(args.Env))
		for key := range args.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		details["envKeys"] = keys
	}
	if args.TimeoutMS > 0 {
		details["timeoutMS"] = args.TimeoutMS
	}
	return details, nil
}

func allowedCommandEnvKey(key string) bool {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "COLORTERM", "NO_COLOR", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "DEVELOPER_DIR", "SDKROOT", "XDG_CACHE_HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT", "GOENV", "GOFLAGS", "JAVA_HOME", "VIRTUAL_ENV", "CARGO_HOME", "RUSTUP_HOME", "PNPM_HOME", "NVM_DIR", "PYENV_ROOT", "RBENV_ROOT", "MISE_DATA_DIR", "VOLTA_HOME", "BUN_INSTALL", "SSH_AUTH_SOCK", "SSL_CERT_FILE", "SSL_CERT_DIR", "REQUESTS_CA_BUNDLE", "CURL_CA_BUNDLE", "PIP_CERT":
		return true
	default:
		return false
	}
}

func validCommandEnvKey(key string) bool {
	if key == "" {
		return false
	}
	for i, r := range key {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func validateCommandInput(args commandRunArgs) error {
	if strings.TrimSpace(args.Command) == "" {
		return errors.New("command is required")
	}
	if strings.ContainsRune(args.Command, 0) {
		return errors.New("command must not contain NUL bytes")
	}
	if len(args.Command) > commandMaxBytes {
		return errors.New("command is too large")
	}
	if args.Execution != CommandExecutionSandbox && args.Execution != CommandExecutionHost {
		return errors.New("command execution must be sandbox or host")
	}
	if args.Execution == CommandExecutionHost && args.HostAccessReason == "" {
		return errors.New("host_access_reason is required when execution is host")
	}
	if args.Execution == CommandExecutionSandbox && args.HostAccessReason != "" {
		return errors.New("host_access_reason is available only when execution is host")
	}
	analysis, err := analyzeShellCommand(args.Command)
	if err != nil {
		return err
	}
	if analysis.Background {
		return errors.New("shell background operators are not supported; set background=true instead")
	}
	if args.TTY && !args.Background {
		return errors.New("tty requires background=true")
	}
	if args.Background && args.TimeoutMS != 0 {
		return errors.New("timeout_ms is unavailable for background commands; stop the command session explicitly")
	}
	return nil
}

func commandTimeout(timeoutMS int) (time.Duration, error) {
	if timeoutMS == 0 {
		return commandDefaultTimeout, nil
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond
	if timeout < commandMinTimeout || timeout > commandMaxTimeout {
		return 0, errors.New("timeout_ms must be between 100 and 600000")
	}
	return timeout, nil
}

func commandResult(out Result, args commandRunArgs, shell, cwd string, projectDirs []string, execution *commandExecution, exitCode int, stdout, stderr *truncatingBuffer, timedOut, cancelled bool, duration time.Duration, reason string, runErr error) Result {
	stdoutText := stdout.String()
	stderrText := stderr.String()
	sandboxDenied := execution != nil && execution.Sandboxed && commandSandboxDenied(stdoutText+"\n"+stderrText, runErr)
	if sandboxDenied {
		reason = "sandbox_denied"
	}
	// A normal non-zero process exit is a completed command, not a tool transport failure.
	ok := reason == "" || reason == "non_zero_exit"
	payload := map[string]any{
		"ok":              ok,
		"cwd":             cwd,
		"exitCode":        exitCode,
		"stdout":          stdoutText,
		"stderr":          stderrText,
		"stdoutTruncated": stdout.Truncated(),
		"stderrTruncated": stderr.Truncated(),
		"timedOut":        timedOut,
		"cancelled":       cancelled,
		"durationMs":      duration.Milliseconds(),
		"execution":       commandActualExecution(execution),
	}
	if execution != nil && execution.SandboxKind != "" {
		payload["sandboxKind"] = execution.SandboxKind
	}
	if execution != nil && execution.Sandboxed {
		payload["sandboxDenied"] = sandboxDenied
	}
	payload["command"] = args.Command
	payload["shell"] = shell
	if reason != "" {
		payload["reason"] = reason
	}
	if runErr != nil && reason != "non_zero_exit" {
		payload["error"] = runErr.Error()
	}
	verificationKind := commandVerificationKind(commandVerificationArgv(args.Command))
	if verificationKind != "" {
		verificationStatus := commandVerificationStatus(verificationKind, exitCode, timedOut, cancelled, reason)
		diagnostics := parseCommandDiagnostics(stdoutText, stderrText, cwd, projectDirs, verificationStatus != "passed")
		payload["verificationKind"] = verificationKind
		payload["verificationStatus"] = verificationStatus
		payload["diagnostics"] = diagnostics
		payload["diagnosticCount"] = len(diagnostics)
	}
	out = toolJSON(out, ok, payload)
	if ok {
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = len(payload)
	}
	return out
}

func commandActualExecution(execution *commandExecution) string {
	if execution != nil && execution.Sandboxed {
		return commandExecutionLabel(true)
	}
	return commandExecutionLabel(false)
}

func commandExecutionLabel(sandboxed bool) string {
	if sandboxed {
		return string(CommandExecutionSandbox)
	}
	return string(CommandExecutionHost)
}

func RequestedCommandExecution(raw json.RawMessage) (CommandExecutionMode, error) {
	args, err := decodeCommandRunArgs(raw)
	if err != nil {
		return "", err
	}
	return args.Execution, nil
}

func CommandBoundaryFailure(call Call, risk ToolRisk) (Result, bool) {
	requested, err := RequestedCommandExecution(call.Args)
	if err != nil || requested == CommandExecutionHost {
		return Result{}, false
	}
	out := Result{CallID: call.CallID, Name: call.Name}
	if len(risk.requiredProjectPaths) > 0 {
		payload := map[string]any{
			"ok":          false,
			"reason":      "additional_project_access_required",
			"detail":      "command references paths outside the authorized project directories; request those directories with request_capability and retry inside the sandbox",
			"execution":   string(CommandExecutionSandbox),
			"paths":       risk.requiredProjectPaths,
			"projectDirs": commandProjectDirSuggestions(risk.requiredProjectPaths),
		}
		return toolJSON(out, false, payload), true
	}
	if risk.hostAccessRequired {
		payload := map[string]any{
			"ok":        false,
			"reason":    "host_access_required",
			"detail":    "command requires host access; retry with execution=host and an explicit host_access_reason",
			"execution": string(CommandExecutionSandbox),
		}
		return toolJSON(out, false, payload), true
	}
	return Result{}, false
}

func commandProjectDirSuggestions(paths []string) []string {
	var dirs []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || !filepath.IsAbs(path) {
			continue
		}
		dir := path
		if info, err := os.Stat(path); err == nil {
			if !info.IsDir() {
				dir = filepath.Dir(path)
			}
		} else {
			dir = filepath.Dir(path)
		}
		dirs = append(dirs, dir)
	}
	return compactRiskPaths(dirs...)
}

type commandProgressWriter struct {
	ctx         context.Context
	stream      string
	destination *truncatingBuffer

	mu        sync.Mutex
	pending   []byte
	timer     *time.Timer
	accepted  int
	truncated bool
	closed    bool
}

func newCommandProgressWriter(ctx context.Context, stream string, destination *truncatingBuffer) *commandProgressWriter {
	return &commandProgressWriter{ctx: ctx, stream: stream, destination: destination}
}

func (w *commandProgressWriter) Write(p []byte) (int, error) {
	written, err := w.destination.Write(p)
	if written == 0 {
		return written, err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return written, err
	}
	remaining := commandLiveOutputLimit - w.accepted
	if remaining > 0 {
		count := min(written, remaining)
		w.pending = append(w.pending, p[:count]...)
		w.accepted += count
	}
	if written > remaining && !w.truncated {
		w.pending = append(w.pending, []byte("\n... live output truncated ...\n")...)
		w.truncated = true
	}
	if len(w.pending) >= commandLiveFlushBytes {
		w.flushLocked()
	} else if len(w.pending) > 0 && w.timer == nil {
		w.timer = time.AfterFunc(commandLiveFlushDelay, w.flush)
	}
	return written, err
}

func (w *commandProgressWriter) Close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.flushLocked()
}

func (w *commandProgressWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.timer = nil
	w.flushLocked()
}

func (w *commandProgressWriter) flushLocked() {
	if len(w.pending) == 0 {
		return
	}
	content := strings.ToValidUTF8(string(w.pending), "�")
	w.pending = nil
	EmitProgress(w.ctx, Progress{Stream: w.stream, Content: content})
}

type truncatingBuffer struct {
	head      []byte
	tail      []byte
	headLimit int
	tailLimit int
	total     int
}

func newTruncatingBuffer(limit int) *truncatingBuffer {
	if limit < 2 {
		limit = 2
	}
	return &truncatingBuffer{headLimit: limit / 2, tailLimit: limit - limit/2}
}

func (b *truncatingBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.total += written
	if len(b.head) < b.headLimit {
		count := min(len(p), b.headLimit-len(b.head))
		b.head = append(b.head, p[:count]...)
		p = p[count:]
	}
	if len(p) > 0 {
		b.tail = append(b.tail, p...)
		if len(b.tail) > b.tailLimit {
			b.tail = append(b.tail[:0], b.tail[len(b.tail)-b.tailLimit:]...)
		}
	}
	return written, nil
}

func (b *truncatingBuffer) String() string {
	const marker = "\n... output truncated ...\n"
	if !b.Truncated() {
		return strings.ToValidUTF8(string(append(append([]byte(nil), b.head...), b.tail...)), "�")
	}
	return strings.ToValidUTF8(string(b.head)+marker+string(b.tail), "�")
}

func (b *truncatingBuffer) Truncated() bool {
	return b.total > b.headLimit+b.tailLimit
}

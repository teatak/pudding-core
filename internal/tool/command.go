package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
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
	commandMaxArgs          = 128
	commandMaxScriptBytes   = 64 << 10
)

type commandRunArgs struct {
	Scope     string            `json:"scope"`
	Argv      []string          `json:"argv,omitempty"`
	Script    string            `json:"script,omitempty"`
	CWD       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMS int               `json:"timeout_ms,omitempty"`
}

func (r *BuiltinRunner) commandRun(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args commandRunArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "command arguments must be a JSON object")
	}
	args.Scope = strings.TrimSpace(args.Scope)
	if args.Scope != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "command scope must be project")
	}
	if err := validateCommandInput(args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
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

func commandInvocation(args commandRunArgs) (string, []string, string) {
	if args.Script == "" {
		return args.Argv[0], args.Argv[1:], ""
	}
	if runtime.GOOS == "windows" {
		return "powershell.exe", []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", args.Script}, "powershell"
	}
	return "/bin/sh", []string{"-lc", args.Script}, "sh"
}

func commandApprovalDetails(call Call) (map[string]any, error) {
	var args commandRunArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return nil, errors.New("command arguments must be a JSON object")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return nil, errors.New("command scope must be project")
	}
	if err := validateCommandInput(args); err != nil {
		return nil, err
	}
	details := map[string]any{}
	if len(args.Argv) > 0 {
		details["argv"] = args.Argv
	} else {
		details["script"] = args.Script
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

func commandEnvironment(custom map[string]string) ([]string, error) {
	type envValue struct {
		key   string
		value string
	}
	values := make(map[string]envValue)
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !allowedCommandEnvKey(key) {
			continue
		}
		values[strings.ToUpper(key)] = envValue{key: key, value: value}
	}
	for key, value := range custom {
		key = strings.TrimSpace(key)
		if !validCommandEnvKey(key) {
			return nil, errors.New("env keys must match [A-Za-z_][A-Za-z0-9_]*")
		}
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("env values must not contain NUL bytes")
		}
		values[strings.ToUpper(key)] = envValue{key: key, value: value}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		out = append(out, value.key+"="+value.value)
	}
	return out, nil
}

func allowedCommandEnvKey(key string) bool {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "PATH", "HOME", "USER", "LOGNAME", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL", "LC_CTYPE", "TERM", "COLORTERM", "NO_COLOR", "SYSTEMROOT", "WINDIR", "COMSPEC", "PATHEXT", "USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "PROGRAMFILES", "PROGRAMFILES(X86)", "DEVELOPER_DIR", "SDKROOT", "XDG_CACHE_HOME", "GOCACHE", "GOMODCACHE", "GOPATH", "GOROOT", "GOENV", "GOFLAGS", "JAVA_HOME", "VIRTUAL_ENV", "CARGO_HOME", "RUSTUP_HOME":
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

func validateCommandArgv(argv []string) error {
	if len(argv) == 0 {
		return errors.New("argv must contain an executable")
	}
	if len(argv) > commandMaxArgs {
		return errors.New("argv has too many entries")
	}
	for i, arg := range argv {
		if strings.ContainsRune(arg, 0) {
			return errors.New("argv must not contain NUL bytes")
		}
		if i == 0 && strings.TrimSpace(arg) == "" {
			return errors.New("argv executable must not be empty")
		}
	}
	return nil
}

func validateCommandInput(args commandRunArgs) error {
	hasArgv := len(args.Argv) > 0
	hasScript := strings.TrimSpace(args.Script) != ""
	if hasArgv == hasScript {
		return errors.New("exactly one of argv or script is required")
	}
	if hasArgv {
		return validateCommandArgv(args.Argv)
	}
	if strings.ContainsRune(args.Script, 0) {
		return errors.New("script must not contain NUL bytes")
	}
	if len(args.Script) > commandMaxScriptBytes {
		return errors.New("script is too large")
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
	// A non-zero process exit is a completed command, not a tool transport failure.
	ok := reason == "" || reason == "non_zero_exit"
	stdoutText := stdout.String()
	stderrText := stderr.String()
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
		"sandboxed":       execution != nil && execution.Sandboxed,
	}
	if execution != nil && execution.SandboxKind != "" {
		payload["sandboxKind"] = execution.SandboxKind
	}
	if execution != nil && execution.Sandboxed {
		payload["sandboxDenied"] = commandSandboxDenied(stdoutText+"\n"+stderrText, runErr)
	}
	if len(args.Argv) > 0 {
		payload["argv"] = args.Argv
	} else {
		payload["script"] = args.Script
		payload["shell"] = shell
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if runErr != nil && reason != "non_zero_exit" {
		payload["error"] = runErr.Error()
	}
	verificationKind := ""
	if len(args.Argv) > 0 {
		verificationKind = commandVerificationKind(args.Argv)
	}
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

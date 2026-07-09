package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const (
	commandDefaultTimeout   = 60 * time.Second
	commandMaxTimeout       = 10 * time.Minute
	commandMinTimeout       = 100 * time.Millisecond
	commandOutputLimitBytes = 64 << 10
	commandMaxArgs          = 128
)

type commandRunArgs struct {
	Scope     string            `json:"scope"`
	Argv      []string          `json:"argv"`
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
	if err := validateCommandArgv(args.Argv); err != nil {
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

	cmd := exec.Command(args.Argv[0], args.Argv[1:]...)
	cmd.Dir = resolvedCWD
	cmd.Env, err = commandEnvironment(args.Env)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	configureCommandProcess(cmd)
	stdout := newTruncatingBuffer(commandOutputLimitBytes)
	stderr := newTruncatingBuffer(commandOutputLimitBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	startedAt := time.Now()
	if err := cmd.Start(); err != nil {
		return commandResult(out, args, resolvedCWD, -1, stdout, stderr, false, false, time.Since(startedAt), "start_failed", err)
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
	return commandResult(out, args, resolvedCWD, exitCode, stdout, stderr, timedOut, cancelled, time.Since(startedAt), reason, waitErr)
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

func commandResult(out Result, args commandRunArgs, cwd string, exitCode int, stdout, stderr *truncatingBuffer, timedOut, cancelled bool, duration time.Duration, reason string, runErr error) Result {
	ok := reason == "" && exitCode == 0
	payload := map[string]any{
		"ok":              ok,
		"argv":            args.Argv,
		"cwd":             cwd,
		"exitCode":        exitCode,
		"stdout":          stdout.String(),
		"stderr":          stderr.String(),
		"stdoutTruncated": stdout.Truncated(),
		"stderrTruncated": stderr.Truncated(),
		"timedOut":        timedOut,
		"cancelled":       cancelled,
		"durationMs":      duration.Milliseconds(),
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if runErr != nil {
		payload["error"] = runErr.Error()
	}
	out = toolJSON(out, ok, payload)
	if ok {
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = len(payload)
	}
	return out
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

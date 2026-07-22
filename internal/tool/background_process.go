package tool

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	backgroundProcessPerSessionLimit = 4
	backgroundProcessGlobalLimit     = 32
	backgroundProcessOutputLimit     = 1 << 20
	backgroundProcessPollDefault     = 64 << 10
	backgroundProcessPollMin         = 1 << 10
	backgroundProcessPollMax         = 256 << 10
	backgroundProcessPollWaitMax     = 10 * time.Minute
	backgroundProcessRetentionTTL    = 30 * time.Minute
	backgroundProcessStopWait        = 2 * time.Second
	backgroundProcessInputMax        = 64 << 10
	backgroundProcessInputTimeout    = 5 * time.Second
	backgroundProcessTTYColumns      = 100
	backgroundProcessTTYRows         = 30
)

var ErrBackgroundProcessNotFound = errors.New("background process not found")

type BackgroundProcessSnapshot struct {
	ProcessID     string     `json:"processID"`
	TurnID        string     `json:"turnID,omitempty"`
	CallID        string     `json:"callID,omitempty"`
	Status        string     `json:"status"`
	Running       bool       `json:"running"`
	CWD           string     `json:"cwd"`
	Command       string     `json:"command"`
	Shell         string     `json:"shell,omitempty"`
	ExitCode      *int       `json:"exitCode,omitempty"`
	StartedAt     time.Time  `json:"startedAt"`
	FinishedAt    *time.Time `json:"finishedAt,omitempty"`
	Reason        string     `json:"reason,omitempty"`
	Error         string     `json:"error,omitempty"`
	Sandboxed     bool       `json:"sandboxed"`
	SandboxKind   string     `json:"sandboxKind,omitempty"`
	SandboxDenied bool       `json:"sandboxDenied,omitempty"`
	TTY           bool       `json:"tty,omitempty"`
}

type BackgroundProcessOutputChunk struct {
	Offset  int64  `json:"offset"`
	Stream  string `json:"stream"`
	Content string `json:"content"`
}

type BackgroundProcessLogSnapshot struct {
	Process      BackgroundProcessSnapshot      `json:"process"`
	Output       []BackgroundProcessOutputChunk `json:"output"`
	OldestOffset int64                          `json:"oldestOffset"`
	NextOffset   int64                          `json:"nextOffset"`
	TailOffset   int64                          `json:"tailOffset"`
	Truncated    bool                           `json:"truncated"`
	HasMore      bool                           `json:"hasMore"`
}

type BackgroundProcessEvent struct {
	SessionID string
	Phase     string
	Process   BackgroundProcessSnapshot
}

const (
	BackgroundProcessStarted  = "started"
	BackgroundProcessFinished = "finished"
	BackgroundProcessStopped  = "stopped"
	BackgroundProcessRemoved  = "removed"
)

type commandSessionArgs struct {
	Action    string `json:"action"`
	ProcessID string `json:"process_id"`
	Offset    int64  `json:"offset,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
	WaitMS    int    `json:"wait_ms,omitempty"`
	Data      string `json:"data,omitempty"`
}

type backgroundProcessManager struct {
	mu               sync.Mutex
	commands         commandRunner
	processes        map[string]*backgroundProcess
	runningBySession map[string]int
	runningTotal     int
	closed           bool
	retentionTTL     time.Duration
	events           func(BackgroundProcessEvent)
}

type backgroundProcess struct {
	manager     *backgroundProcessManager
	id          string
	sessionID   string
	turnID      string
	callID      string
	cwd         string
	command     string
	shell       string
	cmd         *exec.Cmd
	done        chan struct{}
	sandboxed   bool
	sandboxKind string
	tty         bool
	stdin       io.WriteCloser
	pty         io.ReadWriteCloser
	inputMu     sync.Mutex

	mu                   sync.Mutex
	running              bool
	exitCode             *int
	reason               string
	errorText            string
	sandboxDenied        bool
	sandboxDenialOutput  bool
	sandboxDetectionTail string
	startedAt            time.Time
	finishedAt           time.Time
	expiryTimer          *time.Timer
	requestedStopReason  string
	output               backgroundProcessOutputBuffer

	counted bool // guarded by manager.mu
}

type backgroundProcessOutputChunk = BackgroundProcessOutputChunk

type backgroundProcessOutputBuffer struct {
	chunks     []backgroundProcessOutputChunk
	baseOffset int64
	nextOffset int64
	bytes      int
}

type backgroundProcessWriter struct {
	process *backgroundProcess
	stream  string
}

func newBackgroundProcessManager(retentionTTL time.Duration, runners ...commandRunner) *backgroundProcessManager {
	if retentionTTL <= 0 {
		retentionTTL = backgroundProcessRetentionTTL
	}
	commands := newDirectCommandRunner()
	if len(runners) > 0 && runners[0] != nil {
		commands = runners[0]
	}
	return &backgroundProcessManager{
		commands:         commands,
		processes:        make(map[string]*backgroundProcess),
		runningBySession: make(map[string]int),
		retentionTTL:     retentionTTL,
	}
}

func (r *BuiltinRunner) commandStart(call Call, args commandRunArgs) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if strings.TrimSpace(call.SessionID) == "" {
		return toolJSONError(out, "session_required", "background processes require a session")
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
	env, err := commandEnvironment(args.Env)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	commandArgs := commandRunArgs{Scope: args.Scope, Command: args.Command, CWD: args.CWD, Env: args.Env}
	executable, invocationArgs, shell := commandInvocation(commandArgs)
	process, err := r.processes.Start(call.SessionID, call.TurnID, call.CallID, resolvedCWD, call.ProjectDirs, call.CommandSandbox, env, executable, invocationArgs, args.Command, shell, args.TTY)
	if err != nil {
		return backgroundProcessError(out, err)
	}
	payload := process.statePayload()
	out = toolJSON(out, true, payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func (r *BuiltinRunner) commandSession(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeCommandSessionArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	process := r.processes.Get(call.SessionID, args.ProcessID)
	if process == nil {
		return toolJSONError(out, "process_not_found", "background process was not found for this session")
	}
	var payload map[string]any
	switch args.Action {
	case "poll":
		process.waitForPoll(ctx, time.Duration(args.WaitMS)*time.Millisecond)
		payload = process.pollPayload(args.Offset, args.MaxBytes)
		out.SummaryKind = SummaryReturnedItems
		if chunks, ok := payload["output"].([]backgroundProcessOutputChunk); ok {
			out.SummaryCount = len(chunks)
		}
	case "write":
		written, writeErr := process.writeInput(ctx, args.Data)
		if writeErr != nil {
			return toolJSONError(out, "input_failed", writeErr.Error())
		}
		payload = process.statePayload()
		payload["bytesWritten"] = written
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = len(payload)
	case "stop":
		if stopErr := process.stop("stopped"); stopErr != nil {
			return toolJSONError(out, "stop_failed", stopErr.Error())
		}
		payload = process.statePayload()
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = len(payload)
	}
	out = toolJSON(out, true, payload)
	return out
}

func decodeCommandSessionArgs(raw json.RawMessage) (commandSessionArgs, error) {
	var args commandSessionArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, errors.New("command session arguments must be a JSON object")
	}
	args.Action = strings.TrimSpace(args.Action)
	args.ProcessID = strings.TrimSpace(args.ProcessID)
	if args.Action != "poll" && args.Action != "write" && args.Action != "stop" {
		return args, errors.New("action must be poll, write, or stop")
	}
	if args.ProcessID == "" {
		return args, errors.New("process_id is required")
	}
	if args.Action == "write" {
		if args.Data == "" {
			return args, errors.New("data is required for write")
		}
		if len(args.Data) > backgroundProcessInputMax {
			return args, errors.New("data must not exceed 65536 bytes")
		}
		if args.Offset != 0 || args.MaxBytes != 0 || args.WaitMS != 0 {
			return args, errors.New("offset, max_bytes, and wait_ms are available only for poll")
		}
		return args, nil
	}
	if args.Action == "stop" {
		if args.Data != "" || args.Offset != 0 || args.MaxBytes != 0 || args.WaitMS != 0 {
			return args, errors.New("stop accepts only action and process_id")
		}
		return args, nil
	}
	if args.Offset < 0 {
		return args, errors.New("offset must be non-negative")
	}
	if args.MaxBytes == 0 {
		args.MaxBytes = backgroundProcessPollDefault
	}
	if args.MaxBytes < backgroundProcessPollMin || args.MaxBytes > backgroundProcessPollMax {
		return args, errors.New("max_bytes must be between 1024 and 262144")
	}
	if args.WaitMS < 0 || args.WaitMS > int(backgroundProcessPollWaitMax/time.Millisecond) {
		return args, errors.New("wait_ms must be between 0 and 600000")
	}
	return args, nil
}

func (m *backgroundProcessManager) Start(sessionID, turnID, callID, cwd string, projectDirs []string, sandboxMode CommandSandboxMode, env []string, executable string, invocationArgs []string, command, shell string, tty bool) (*backgroundProcess, error) {
	if tty && runtime.GOOS == "windows" {
		return nil, errors.New("interactive command sessions are unavailable on Windows")
	}
	execution, err := m.commands.Prepare(commandSpec{
		Executable:  executable,
		Args:        invocationArgs,
		CWD:         cwd,
		Env:         env,
		ProjectDirs: projectDirs,
		SandboxMode: sandboxMode,
	})
	if err != nil {
		return nil, err
	}
	cmd := execution.Cmd
	process := &backgroundProcess{
		manager:     m,
		id:          store.NewID("proc"),
		sessionID:   sessionID,
		turnID:      strings.TrimSpace(turnID),
		callID:      strings.TrimSpace(callID),
		cwd:         cwd,
		command:     command,
		shell:       shell,
		cmd:         cmd,
		done:        make(chan struct{}),
		sandboxed:   execution.Sandboxed,
		sandboxKind: execution.SandboxKind,
		tty:         tty,
		running:     true,
		startedAt:   time.Now(),
	}
	if !tty {
		stdin, stdinErr := cmd.StdinPipe()
		if stdinErr != nil {
			return nil, stdinErr
		}
		process.stdin = stdin
		cmd.Stdout = backgroundProcessWriter{process: process, stream: ProgressStdout}
		cmd.Stderr = backgroundProcessWriter{process: process, stream: ProgressStderr}
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		process.closeInput()
		return nil, errors.New("background process manager is closed")
	}
	if m.runningBySession[sessionID] >= backgroundProcessPerSessionLimit {
		m.mu.Unlock()
		process.closeInput()
		return nil, errors.New("background process limit reached for this session")
	}
	if m.runningTotal >= backgroundProcessGlobalLimit {
		m.mu.Unlock()
		process.closeInput()
		return nil, errors.New("global background process limit reached")
	}
	evicted := m.evictFinishedLocked()
	process.counted = true
	m.processes[process.id] = process
	m.runningBySession[sessionID]++
	m.runningTotal++
	if tty {
		configureCommandPTY(cmd)
		ptmx, startErr := pty.StartWithSize(cmd, &pty.Winsize{Cols: backgroundProcessTTYColumns, Rows: backgroundProcessTTYRows})
		if startErr == nil {
			process.stdin = ptmx
			process.pty = ptmx
		}
		err = startErr
	} else {
		err = cmd.Start()
	}
	if err != nil {
		delete(m.processes, process.id)
		m.releaseCountLocked(process)
		m.mu.Unlock()
		process.closeInput()
		for _, finished := range evicted {
			finished.cancelExpiry()
			m.emit(finished.sessionID, BackgroundProcessRemoved, finished.snapshot())
		}
		return nil, err
	}
	m.mu.Unlock()
	for _, finished := range evicted {
		finished.cancelExpiry()
		m.emit(finished.sessionID, BackgroundProcessRemoved, finished.snapshot())
	}
	m.emit(sessionID, BackgroundProcessStarted, process.snapshot())
	if tty {
		go process.readPTYOutput()
	}
	go process.wait()
	return process, nil
}

func (m *backgroundProcessManager) Get(sessionID, processID string) *backgroundProcess {
	m.mu.Lock()
	defer m.mu.Unlock()
	process := m.processes[strings.TrimSpace(processID)]
	if process == nil || process.sessionID != strings.TrimSpace(sessionID) {
		return nil
	}
	return process
}

func (m *backgroundProcessManager) List(sessionID string) []BackgroundProcessSnapshot {
	sessionID = strings.TrimSpace(sessionID)
	m.mu.Lock()
	processes := make([]*backgroundProcess, 0)
	for _, process := range m.processes {
		if process.sessionID == sessionID {
			processes = append(processes, process)
		}
	}
	m.mu.Unlock()

	items := make([]BackgroundProcessSnapshot, 0, len(processes))
	for _, process := range processes {
		items = append(items, process.snapshot())
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Running != items[j].Running {
			return items[i].Running
		}
		return items[i].StartedAt.After(items[j].StartedAt)
	})
	return items
}

func (m *backgroundProcessManager) Count(sessionID string) int {
	sessionID = strings.TrimSpace(sessionID)
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.runningBySession[sessionID]
}

func (m *backgroundProcessManager) Read(sessionID, processID string, offset int64, maxBytes, tailBytes int) (BackgroundProcessLogSnapshot, error) {
	process := m.Get(sessionID, processID)
	if process == nil {
		return BackgroundProcessLogSnapshot{}, ErrBackgroundProcessNotFound
	}
	if offset < 0 {
		return BackgroundProcessLogSnapshot{}, errors.New("offset must be non-negative")
	}
	if maxBytes == 0 {
		maxBytes = backgroundProcessPollDefault
	}
	if maxBytes < backgroundProcessPollMin || maxBytes > backgroundProcessPollMax {
		return BackgroundProcessLogSnapshot{}, errors.New("max_bytes must be between 1024 and 262144")
	}
	if tailBytes < 0 || tailBytes > backgroundProcessPollMax || (tailBytes > 0 && tailBytes < backgroundProcessPollMin) {
		return BackgroundProcessLogSnapshot{}, errors.New("tail_bytes must be 0 or between 1024 and 262144")
	}
	return process.logSnapshot(offset, maxBytes, tailBytes), nil
}

func (m *backgroundProcessManager) Stop(sessionID, processID string) (BackgroundProcessSnapshot, error) {
	process := m.Get(sessionID, processID)
	if process == nil {
		return BackgroundProcessSnapshot{}, ErrBackgroundProcessNotFound
	}
	if err := process.stop("stopped"); err != nil {
		return BackgroundProcessSnapshot{}, err
	}
	return process.snapshot(), nil
}

func (m *backgroundProcessManager) CloseSession(sessionID string) {
	m.closeMatching(func(process *backgroundProcess) bool { return process.sessionID == sessionID }, "session_closed")
}

func (m *backgroundProcessManager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	m.closeMatching(func(*backgroundProcess) bool { return true }, "daemon_closed")
	return nil
}

func (m *backgroundProcessManager) closeMatching(match func(*backgroundProcess) bool, reason string) {
	m.mu.Lock()
	processes := make([]*backgroundProcess, 0)
	for id, process := range m.processes {
		if !match(process) {
			continue
		}
		processes = append(processes, process)
		delete(m.processes, id)
		m.releaseCountLocked(process)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, process := range processes {
		wg.Add(1)
		go func(process *backgroundProcess) {
			defer wg.Done()
			process.cancelExpiry()
			_ = process.stop(reason)
			process.cancelExpiry()
		}(process)
	}
	wg.Wait()
}

func (m *backgroundProcessManager) expireFinished(process *backgroundProcess) {
	process.mu.Lock()
	if process.running {
		process.expiryTimer = nil
		process.mu.Unlock()
		return
	}
	process.expiryTimer = nil
	process.mu.Unlock()
	if m.remove(process) {
		m.emit(process.sessionID, BackgroundProcessRemoved, process.snapshot())
	}
}

func (m *backgroundProcessManager) evictFinishedLocked() []*backgroundProcess {
	var evicted []*backgroundProcess
	for len(m.processes) >= backgroundProcessGlobalLimit {
		var oldest *backgroundProcess
		for _, candidate := range m.processes {
			if candidate.counted || (oldest != nil && !candidate.startedAt.Before(oldest.startedAt)) {
				continue
			}
			oldest = candidate
		}
		if oldest == nil {
			break
		}
		delete(m.processes, oldest.id)
		evicted = append(evicted, oldest)
	}
	return evicted
}

func (m *backgroundProcessManager) markFinished(process *backgroundProcess) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processes[process.id] == process {
		m.releaseCountLocked(process)
	}
}

func (m *backgroundProcessManager) remove(process *backgroundProcess) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.processes[process.id] != process {
		return false
	}
	delete(m.processes, process.id)
	m.releaseCountLocked(process)
	return true
}

func (m *backgroundProcessManager) releaseCountLocked(process *backgroundProcess) {
	if !process.counted {
		return
	}
	process.counted = false
	if m.runningTotal > 0 {
		m.runningTotal--
	}
	if m.runningBySession[process.sessionID] <= 1 {
		delete(m.runningBySession, process.sessionID)
	} else {
		m.runningBySession[process.sessionID]--
	}
}

func (m *backgroundProcessManager) emit(sessionID, phase string, process BackgroundProcessSnapshot) {
	if m.events == nil {
		return
	}
	m.events(BackgroundProcessEvent{SessionID: sessionID, Phase: phase, Process: process})
}

func (p *backgroundProcess) wait() {
	waitErr := p.cmd.Wait()
	p.closeInput()
	exitCode := -1
	if p.cmd.ProcessState != nil {
		exitCode = p.cmd.ProcessState.ExitCode()
	}
	now := time.Now()
	p.mu.Lock()
	p.running = false
	p.exitCode = &exitCode
	p.finishedAt = now
	switch {
	case p.requestedStopReason != "":
		p.reason = p.requestedStopReason
	case waitErr != nil:
		p.reason = "non_zero_exit"
	default:
		p.reason = "exited"
	}
	if waitErr != nil && p.cmd.ProcessState == nil {
		p.errorText = waitErr.Error()
	}
	if p.sandboxed && (commandSandboxDenied(p.sandboxDetectionTail+"\n"+p.errorText, waitErr) || (waitErr != nil && p.sandboxDenialOutput)) {
		p.sandboxDenied = true
	}
	p.scheduleRetentionLocked()
	stopped := p.requestedStopReason != ""
	p.mu.Unlock()
	p.manager.markFinished(p)
	close(p.done)
	phase := BackgroundProcessFinished
	if stopped {
		phase = BackgroundProcessStopped
	}
	p.manager.emit(p.sessionID, phase, p.snapshot())
}

func (p *backgroundProcess) readPTYOutput() {
	buffer := make([]byte, 32<<10)
	writer := backgroundProcessWriter{process: p, stream: ProgressStdout}
	for {
		p.inputMu.Lock()
		reader := p.pty
		p.inputMu.Unlock()
		if reader == nil {
			return
		}
		n, err := reader.Read(buffer)
		if n > 0 {
			_, _ = writer.Write(buffer[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *backgroundProcess) writeInput(ctx context.Context, data string) (int, error) {
	if len(data) > backgroundProcessInputMax {
		return 0, errors.New("data must not exceed 65536 bytes")
	}
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	p.mu.Lock()
	running := p.running
	p.mu.Unlock()
	if !running {
		return 0, errors.New("command session is not running")
	}
	if p.stdin == nil {
		return 0, errors.New("command session input is closed")
	}
	deadline := time.Now().Add(backgroundProcessInputTimeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if writer, ok := p.stdin.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = writer.SetWriteDeadline(deadline)
		defer writer.SetWriteDeadline(time.Time{})
	}
	return io.WriteString(p.stdin, data)
}

func (p *backgroundProcess) closeInput() {
	p.inputMu.Lock()
	stdin := p.stdin
	p.stdin = nil
	p.pty = nil
	p.inputMu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
}

func (p *backgroundProcess) stop(reason string) error {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return nil
	}
	if p.requestedStopReason == "" {
		p.requestedStopReason = reason
	}
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()

	_ = requestCommandProcessStop(cmd)
	select {
	case <-done:
		return nil
	case <-time.After(backgroundProcessStopWait):
	}
	_ = terminateCommandProcess(cmd)
	select {
	case <-done:
		return nil
	case <-time.After(backgroundProcessStopWait):
		return errors.New("background process did not stop in time")
	}
}

func (p *backgroundProcess) waitForPoll(ctx context.Context, wait time.Duration) {
	if wait <= 0 {
		return
	}
	p.mu.Lock()
	running := p.running
	done := p.done
	p.mu.Unlock()
	if !running {
		return
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-done:
	case <-ctx.Done():
	case <-timer.C:
	}
}

func (p *backgroundProcess) pollPayload(offset int64, maxBytes int) map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	chunks, nextOffset, truncated, hasMore := p.output.Read(offset, maxBytes)
	payload := p.statePayloadLocked()
	payload["output"] = chunks
	payload["oldestOffset"] = p.output.baseOffset
	payload["nextOffset"] = nextOffset
	payload["tailOffset"] = p.output.nextOffset
	payload["truncated"] = truncated
	payload["hasMore"] = hasMore
	return payload
}

func (p *backgroundProcess) logSnapshot(offset int64, maxBytes, tailBytes int) BackgroundProcessLogSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	if tailBytes > 0 {
		offset = p.output.nextOffset - int64(tailBytes)
		if offset < 0 {
			offset = 0
		}
		maxBytes = tailBytes
	}
	chunks, nextOffset, truncated, hasMore := p.output.Read(offset, maxBytes)
	return BackgroundProcessLogSnapshot{
		Process:      p.snapshotLocked(),
		Output:       chunks,
		OldestOffset: p.output.baseOffset,
		NextOffset:   nextOffset,
		TailOffset:   p.output.nextOffset,
		Truncated:    truncated,
		HasMore:      hasMore,
	}
}

func (p *backgroundProcess) statePayload() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.statePayloadLocked()
}

func (p *backgroundProcess) statePayloadLocked() map[string]any {
	status := p.statusLocked()
	payload := map[string]any{
		"ok":        true,
		"processID": p.id,
		"status":    status,
		"running":   p.running,
		"cwd":       p.cwd,
		"startedAt": p.startedAt.UTC().Format(time.RFC3339Nano),
		"sandboxed": p.sandboxed,
	}
	if p.sandboxKind != "" {
		payload["sandboxKind"] = p.sandboxKind
	}
	if p.sandboxDenied {
		payload["sandboxDenied"] = true
	}
	if p.turnID != "" {
		payload["turnID"] = p.turnID
	}
	if p.callID != "" {
		payload["callID"] = p.callID
	}
	payload["command"] = p.command
	payload["shell"] = p.shell
	payload["tty"] = p.tty
	if p.exitCode != nil {
		payload["exitCode"] = *p.exitCode
	}
	if !p.finishedAt.IsZero() {
		payload["finishedAt"] = p.finishedAt.UTC().Format(time.RFC3339Nano)
	}
	if p.reason != "" {
		payload["reason"] = p.reason
	}
	if p.errorText != "" {
		payload["error"] = p.errorText
	}
	return payload
}

func (p *backgroundProcess) snapshot() BackgroundProcessSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.snapshotLocked()
}

func (p *backgroundProcess) snapshotLocked() BackgroundProcessSnapshot {
	item := BackgroundProcessSnapshot{
		ProcessID:     p.id,
		TurnID:        p.turnID,
		CallID:        p.callID,
		Status:        p.statusLocked(),
		Running:       p.running,
		CWD:           p.cwd,
		Command:       p.command,
		Shell:         p.shell,
		StartedAt:     p.startedAt,
		Reason:        p.reason,
		Error:         p.errorText,
		Sandboxed:     p.sandboxed,
		SandboxKind:   p.sandboxKind,
		SandboxDenied: p.sandboxDenied,
		TTY:           p.tty,
	}
	if p.exitCode != nil {
		exitCode := *p.exitCode
		item.ExitCode = &exitCode
	}
	if !p.finishedAt.IsZero() {
		finishedAt := p.finishedAt
		item.FinishedAt = &finishedAt
	}
	return item
}

func (p *backgroundProcess) statusLocked() string {
	if p.running {
		return "running"
	}
	if p.reason != "" {
		return p.reason
	}
	return "exited"
}

func (p *backgroundProcess) scheduleRetentionLocked() {
	if p.expiryTimer != nil {
		p.expiryTimer.Stop()
	}
	p.expiryTimer = time.AfterFunc(p.manager.retentionTTL, func() { p.manager.expireFinished(p) })
}

func (p *backgroundProcess) cancelExpiry() {
	p.mu.Lock()
	if p.expiryTimer != nil {
		p.expiryTimer.Stop()
		p.expiryTimer = nil
	}
	p.mu.Unlock()
}

func (w backgroundProcessWriter) Write(data []byte) (int, error) {
	w.process.mu.Lock()
	w.process.output.Append(w.stream, data)
	if w.process.sandboxed {
		const detectionTailLimit = 256
		scan := w.process.sandboxDetectionTail + string(data)
		if commandSandboxDenialOutput(scan) {
			w.process.sandboxDenialOutput = true
		}
		if len(scan) > detectionTailLimit {
			scan = scan[len(scan)-detectionTailLimit:]
		}
		w.process.sandboxDetectionTail = scan
	}
	w.process.mu.Unlock()
	return len(data), nil
}

func (b *backgroundProcessOutputBuffer) Append(stream string, data []byte) {
	if len(data) == 0 {
		return
	}
	content := strings.ToValidUTF8(string(data), "�")
	if content == "" {
		return
	}
	chunk := backgroundProcessOutputChunk{Offset: b.nextOffset, Stream: stream, Content: content}
	size := len(content)
	b.nextOffset += int64(size)
	b.bytes += size
	b.chunks = append(b.chunks, chunk)
	b.trim()
}

func (b *backgroundProcessOutputBuffer) trim() {
	for b.bytes > backgroundProcessOutputLimit && len(b.chunks) > 0 {
		excess := b.bytes - backgroundProcessOutputLimit
		first := &b.chunks[0]
		if len(first.Content) <= excess {
			b.bytes -= len(first.Content)
			b.baseOffset = first.Offset + int64(len(first.Content))
			b.chunks = b.chunks[1:]
			continue
		}
		drop := utf8BoundaryAtOrAfter(first.Content, excess)
		first.Content = first.Content[drop:]
		first.Offset += int64(drop)
		b.bytes -= drop
		b.baseOffset = first.Offset
	}
	if len(b.chunks) == 0 {
		b.baseOffset = b.nextOffset
	}
}

func (b *backgroundProcessOutputBuffer) Read(offset int64, maxBytes int) ([]backgroundProcessOutputChunk, int64, bool, bool) {
	truncated := offset < b.baseOffset
	if offset < b.baseOffset {
		offset = b.baseOffset
	}
	if offset > b.nextOffset {
		offset = b.nextOffset
	}
	cursor := offset
	remaining := maxBytes
	out := make([]backgroundProcessOutputChunk, 0)
	for _, chunk := range b.chunks {
		chunkEnd := chunk.Offset + int64(len(chunk.Content))
		if chunkEnd <= cursor || remaining <= 0 {
			continue
		}
		start := 0
		if cursor > chunk.Offset {
			start = int(cursor - chunk.Offset)
			start = utf8BoundaryAtOrAfter(chunk.Content, start)
		}
		content := chunk.Content[start:]
		if len(content) > remaining {
			end := utf8BoundaryAtOrBefore(content, remaining)
			if end == 0 {
				break
			}
			content = content[:end]
		}
		chunkOffset := chunk.Offset + int64(start)
		out = append(out, backgroundProcessOutputChunk{Offset: chunkOffset, Stream: chunk.Stream, Content: content})
		cursor = chunkOffset + int64(len(content))
		remaining -= len(content)
	}
	return out, cursor, truncated, cursor < b.nextOffset
}

func utf8BoundaryAtOrAfter(value string, index int) int {
	if index <= 0 {
		return 0
	}
	if index >= len(value) {
		return len(value)
	}
	for index < len(value) && !utf8.RuneStart(value[index]) {
		index++
	}
	return index
}

func utf8BoundaryAtOrBefore(value string, index int) int {
	if index >= len(value) {
		return len(value)
	}
	for index > 0 && !utf8.RuneStart(value[index]) {
		index--
	}
	return index
}

func backgroundProcessError(out Result, err error) Result {
	reason := "start_failed"
	switch err.Error() {
	case "background process manager is closed":
		reason = "process_manager_closed"
	case "background process limit reached for this session":
		reason = "session_process_limit"
	case "global background process limit reached":
		reason = "global_process_limit"
	}
	return toolJSONError(out, reason, err.Error())
}

func (r *BuiltinRunner) CloseSession(sessionID string) {
	if r == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if r.processes != nil {
		r.processes.CloseSession(sessionID)
	}
	if sessionID != "" {
		r.patchMu.Lock()
		for key, prepared := range r.preparedPatches {
			if prepared.SessionID == sessionID {
				delete(r.preparedPatches, key)
			}
		}
		r.patchMu.Unlock()
	}
}

func (r *BuiltinRunner) ListBackgroundProcesses(sessionID string) []BackgroundProcessSnapshot {
	if r == nil || r.processes == nil {
		return []BackgroundProcessSnapshot{}
	}
	return r.processes.List(sessionID)
}

func (r *BuiltinRunner) BackgroundProcessCount(sessionID string) int {
	if r == nil || r.processes == nil {
		return 0
	}
	return r.processes.Count(sessionID)
}

func (r *BuiltinRunner) ReadBackgroundProcess(sessionID, processID string, offset int64, maxBytes, tailBytes int) (BackgroundProcessLogSnapshot, error) {
	if r == nil || r.processes == nil {
		return BackgroundProcessLogSnapshot{}, ErrBackgroundProcessNotFound
	}
	return r.processes.Read(sessionID, processID, offset, maxBytes, tailBytes)
}

func (r *BuiltinRunner) StopBackgroundProcess(sessionID, processID string) (BackgroundProcessSnapshot, error) {
	if r == nil || r.processes == nil {
		return BackgroundProcessSnapshot{}, ErrBackgroundProcessNotFound
	}
	return r.processes.Stop(sessionID, processID)
}

func (r *BuiltinRunner) Close() error {
	if r == nil || r.processes == nil {
		return nil
	}
	return r.processes.Close()
}

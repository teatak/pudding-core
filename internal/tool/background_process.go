package tool

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/store"
)

const (
	backgroundProcessPerSessionLimit = 4
	backgroundProcessGlobalLimit     = 32
	backgroundProcessOutputLimit     = 1 << 20
	backgroundProcessPollDefault     = 64 << 10
	backgroundProcessPollMin         = 1 << 10
	backgroundProcessPollMax         = 256 << 10
	backgroundProcessRetentionTTL    = 30 * time.Minute
	backgroundProcessStopWait        = 2 * time.Second
)

var ErrBackgroundProcessNotFound = errors.New("background process not found")

type BackgroundProcessSnapshot struct {
	ProcessID  string     `json:"processID"`
	TurnID     string     `json:"turnID,omitempty"`
	CallID     string     `json:"callID,omitempty"`
	Status     string     `json:"status"`
	Running    bool       `json:"running"`
	CWD        string     `json:"cwd"`
	Argv       []string   `json:"argv,omitempty"`
	Script     string     `json:"script,omitempty"`
	Shell      string     `json:"shell,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	StartedAt  time.Time  `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	Reason     string     `json:"reason,omitempty"`
	Error      string     `json:"error,omitempty"`
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

type commandStartArgs struct {
	Scope  string            `json:"scope"`
	Argv   []string          `json:"argv,omitempty"`
	Script string            `json:"script,omitempty"`
	CWD    string            `json:"cwd,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
}

type commandPollArgs struct {
	ProcessID string `json:"process_id"`
	Offset    int64  `json:"offset,omitempty"`
	MaxBytes  int    `json:"max_bytes,omitempty"`
}

type commandStopArgs struct {
	ProcessID string `json:"process_id"`
}

type backgroundProcessManager struct {
	mu               sync.Mutex
	processes        map[string]*backgroundProcess
	runningBySession map[string]int
	runningTotal     int
	closed           bool
	retentionTTL     time.Duration
	events           func(BackgroundProcessEvent)
}

type backgroundProcess struct {
	manager   *backgroundProcessManager
	id        string
	sessionID string
	turnID    string
	callID    string
	cwd       string
	argv      []string
	script    string
	shell     string
	cmd       *exec.Cmd
	done      chan struct{}

	mu                  sync.Mutex
	running             bool
	exitCode            *int
	reason              string
	errorText           string
	startedAt           time.Time
	finishedAt          time.Time
	expiryTimer         *time.Timer
	requestedStopReason string
	output              backgroundProcessOutputBuffer

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

func newBackgroundProcessManager(retentionTTL time.Duration) *backgroundProcessManager {
	if retentionTTL <= 0 {
		retentionTTL = backgroundProcessRetentionTTL
	}
	return &backgroundProcessManager{
		processes:        make(map[string]*backgroundProcess),
		runningBySession: make(map[string]int),
		retentionTTL:     retentionTTL,
	}
}

func (r *BuiltinRunner) commandStart(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeCommandStartArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
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
	commandArgs := commandRunArgs{Scope: args.Scope, Argv: args.Argv, Script: args.Script, CWD: args.CWD, Env: args.Env}
	executable, invocationArgs, shell := commandInvocation(commandArgs)
	process, err := r.processes.Start(call.SessionID, call.TurnID, call.CallID, resolvedCWD, env, executable, invocationArgs, args.Argv, args.Script, shell)
	if err != nil {
		return backgroundProcessError(out, err)
	}
	payload := process.statePayload()
	out = toolJSON(out, true, payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func (r *BuiltinRunner) commandPoll(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeCommandPollArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	process := r.processes.Get(call.SessionID, args.ProcessID)
	if process == nil {
		return toolJSONError(out, "process_not_found", "background process was not found for this session")
	}
	payload := process.pollPayload(args.Offset, args.MaxBytes)
	out = toolJSON(out, true, payload)
	out.SummaryKind = SummaryReturnedItems
	if chunks, ok := payload["output"].([]backgroundProcessOutputChunk); ok {
		out.SummaryCount = len(chunks)
	}
	return out
}

func (r *BuiltinRunner) commandStop(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeCommandStopArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	process := r.processes.Get(call.SessionID, args.ProcessID)
	if process == nil {
		return toolJSONError(out, "process_not_found", "background process was not found for this session")
	}
	if err := process.stop("stopped"); err != nil {
		return toolJSONError(out, "stop_failed", err.Error())
	}
	payload := process.statePayload()
	out = toolJSON(out, true, payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func decodeCommandStartArgs(raw json.RawMessage) (commandStartArgs, error) {
	var args commandStartArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, errors.New("background command arguments must be a JSON object")
	}
	args.Scope = strings.TrimSpace(args.Scope)
	if args.Scope != managedScopeProject {
		return args, errors.New("background command scope must be project")
	}
	if err := validateCommandInput(commandRunArgs{Argv: args.Argv, Script: args.Script}); err != nil {
		return args, err
	}
	return args, nil
}

func decodeCommandPollArgs(raw json.RawMessage) (commandPollArgs, error) {
	var args commandPollArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, errors.New("process poll arguments must be a JSON object")
	}
	args.ProcessID = strings.TrimSpace(args.ProcessID)
	if args.ProcessID == "" {
		return args, errors.New("process_id is required")
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
	return args, nil
}

func decodeCommandStopArgs(raw json.RawMessage) (commandStopArgs, error) {
	var args commandStopArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, errors.New("process stop arguments must be a JSON object")
	}
	args.ProcessID = strings.TrimSpace(args.ProcessID)
	if args.ProcessID == "" {
		return args, errors.New("process_id is required")
	}
	return args, nil
}

func (m *backgroundProcessManager) Start(sessionID, turnID, callID, cwd string, env []string, executable string, invocationArgs, argv []string, script, shell string) (*backgroundProcess, error) {
	cmd := exec.Command(executable, invocationArgs...)
	cmd.Dir = cwd
	cmd.Env = env
	configureCommandProcess(cmd)
	process := &backgroundProcess{
		manager:   m,
		id:        store.NewID("proc"),
		sessionID: sessionID,
		turnID:    strings.TrimSpace(turnID),
		callID:    strings.TrimSpace(callID),
		cwd:       cwd,
		argv:      append([]string(nil), argv...),
		script:    script,
		shell:     shell,
		cmd:       cmd,
		done:      make(chan struct{}),
		running:   true,
		startedAt: time.Now(),
	}
	cmd.Stdout = backgroundProcessWriter{process: process, stream: ProgressStdout}
	cmd.Stderr = backgroundProcessWriter{process: process, stream: ProgressStderr}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("background process manager is closed")
	}
	if m.runningBySession[sessionID] >= backgroundProcessPerSessionLimit {
		m.mu.Unlock()
		return nil, errors.New("background process limit reached for this session")
	}
	if m.runningTotal >= backgroundProcessGlobalLimit {
		m.mu.Unlock()
		return nil, errors.New("global background process limit reached")
	}
	evicted := m.evictFinishedLocked()
	process.counted = true
	m.processes[process.id] = process
	m.runningBySession[sessionID]++
	m.runningTotal++
	if err := cmd.Start(); err != nil {
		delete(m.processes, process.id)
		m.releaseCountLocked(process)
		m.mu.Unlock()
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
	}
	if p.turnID != "" {
		payload["turnID"] = p.turnID
	}
	if p.callID != "" {
		payload["callID"] = p.callID
	}
	if len(p.argv) > 0 {
		payload["argv"] = p.argv
	} else {
		payload["script"] = p.script
		payload["shell"] = p.shell
	}
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
		ProcessID: p.id,
		TurnID:    p.turnID,
		CallID:    p.callID,
		Status:    p.statusLocked(),
		Running:   p.running,
		CWD:       p.cwd,
		Argv:      append([]string(nil), p.argv...),
		Script:    p.script,
		Shell:     p.shell,
		StartedAt: p.startedAt,
		Reason:    p.reason,
		Error:     p.errorText,
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
	if r != nil && r.processes != nil {
		r.processes.CloseSession(strings.TrimSpace(sessionID))
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

func commandStartApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeCommandStartArgs(call.Args)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(commandRunArgs{Scope: args.Scope, Argv: args.Argv, Script: args.Script, CWD: args.CWD, Env: args.Env})
	if err != nil {
		return nil, err
	}
	return commandApprovalDetails(Call{Args: raw})
}

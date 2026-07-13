package tool

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type backgroundProcessPayload struct {
	OK            bool                           `json:"ok"`
	ProcessID     string                         `json:"processID"`
	Status        string                         `json:"status"`
	Running       bool                           `json:"running"`
	ExitCode      *int                           `json:"exitCode"`
	Output        []backgroundProcessOutputChunk `json:"output"`
	OldestOffset  int64                          `json:"oldestOffset"`
	NextOffset    int64                          `json:"nextOffset"`
	TailOffset    int64                          `json:"tailOffset"`
	Truncated     bool                           `json:"truncated"`
	HasMore       bool                           `json:"hasMore"`
	Sandboxed     bool                           `json:"sandboxed"`
	SandboxKind   string                         `json:"sandboxKind"`
	SandboxDenied bool                           `json:"sandboxDenied"`
}

func TestBackgroundProcessStartPollStop(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_background", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("background-stream"),
	})
	started := decodeBackgroundProcessPayload(t, start)
	if !start.Ok || !started.OK || started.ProcessID == "" || !started.Running || started.Status != "running" {
		t.Fatalf("background process did not start: result=%+v payload=%+v", start, started)
	}

	var polled backgroundProcessPayload
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result := backgroundToolCall(runner, "sess_background", root, CommandPoll, map[string]any{
			"process_id": started.ProcessID,
			"offset":     polled.NextOffset,
		})
		payload := decodeBackgroundProcessPayload(t, result)
		polled.NextOffset = payload.NextOffset
		polled.Output = append(polled.Output, payload.Output...)
		if strings.Contains(backgroundOutputText(polled.Output), "ready") && strings.Contains(backgroundOutputText(polled.Output), "warning") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if text := backgroundOutputText(polled.Output); !strings.Contains(text, "ready") || !strings.Contains(text, "warning") {
		t.Fatalf("missing background output: chunks=%+v", polled.Output)
	}

	isolation := backgroundToolCall(runner, "sess_other", root, CommandPoll, map[string]any{"process_id": started.ProcessID})
	if isolation.Ok || !strings.Contains(isolation.Content, `"reason":"process_not_found"`) {
		t.Fatalf("process must be isolated by session: %+v", isolation)
	}

	stoppedResult := backgroundToolCall(runner, "sess_background", root, CommandStop, map[string]any{"process_id": started.ProcessID})
	stopped := decodeBackgroundProcessPayload(t, stoppedResult)
	if !stoppedResult.Ok || stopped.Running || stopped.Status != "stopped" || stopped.ExitCode == nil {
		t.Fatalf("background process did not stop: %+v", stopped)
	}
	secondStop := backgroundToolCall(runner, "sess_background", root, CommandStop, map[string]any{"process_id": started.ProcessID})
	if !secondStop.Ok {
		t.Fatalf("stopping an exited process must be idempotent: %+v", secondStop)
	}
}

func TestBackgroundProcessKeepsLaunchAuthorizationSnapshot(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_snapshot", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("sleep", "5000"),
	})
	started := decodeBackgroundProcessPayload(t, start)
	raw, _ := json.Marshal(map[string]any{"process_id": started.ProcessID})
	poll := runner.Call(context.Background(), Call{
		SessionID: "sess_snapshot",
		CallID:    "call_poll_without_project",
		Name:      CommandPoll,
		Args:      raw,
	})
	if !poll.Ok {
		t.Fatalf("an approved process must remain accessible after project context changes: %+v", poll)
	}
	stop := runner.Call(context.Background(), Call{
		SessionID: "sess_snapshot",
		CallID:    "call_stop_without_project",
		Name:      CommandStop,
		Args:      raw,
	})
	if !stop.Ok {
		t.Fatalf("stop approved process without current project context: %+v", stop)
	}
}

func TestBackgroundProcessPollWaitsForExit(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_wait_exit", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("sleep", "80"),
	})
	started := decodeBackgroundProcessPayload(t, start)
	begin := time.Now()
	poll := backgroundToolCall(runner, "sess_wait_exit", root, CommandPoll, map[string]any{
		"process_id": started.ProcessID,
		"wait_ms":    1000,
	})
	elapsed := time.Since(begin)
	payload := decodeBackgroundProcessPayload(t, poll)
	if !poll.Ok || payload.Running || payload.Status != "exited" {
		t.Fatalf("long poll did not return the completed process: result=%+v payload=%+v", poll, payload)
	}
	if elapsed >= 750*time.Millisecond {
		t.Fatalf("long poll did not return early after process exit: %s", elapsed)
	}
}

func TestBackgroundProcessPollWaitTimeoutAndCancellation(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_wait_timeout", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("sleep", "1000"),
	})
	started := decodeBackgroundProcessPayload(t, start)
	begin := time.Now()
	timed := backgroundToolCall(runner, "sess_wait_timeout", root, CommandPoll, map[string]any{
		"process_id": started.ProcessID,
		"wait_ms":    50,
	})
	if elapsed := time.Since(begin); elapsed < 35*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("long poll timeout duration is unexpected: %s", elapsed)
	}
	if payload := decodeBackgroundProcessPayload(t, timed); !timed.Ok || !payload.Running {
		t.Fatalf("timed poll should return a running process: result=%+v payload=%+v", timed, payload)
	}

	raw, _ := json.Marshal(map[string]any{"process_id": started.ProcessID, "wait_ms": 1000})
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(40*time.Millisecond, cancel)
	begin = time.Now()
	cancelled := runner.Call(ctx, Call{
		SessionID: "sess_wait_timeout",
		CallID:    "call_poll_cancelled",
		Name:      CommandPoll,
		Args:      raw,
	})
	if elapsed := time.Since(begin); elapsed > 500*time.Millisecond {
		t.Fatalf("cancelled long poll did not return promptly: %s", elapsed)
	}
	if payload := decodeBackgroundProcessPayload(t, cancelled); !cancelled.Ok || !payload.Running {
		t.Fatalf("cancelled poll should return the current process state: result=%+v payload=%+v", cancelled, payload)
	}
}

func TestBackgroundProcessPollRejectsExcessiveWait(t *testing.T) {
	_, err := decodeCommandPollArgs(json.RawMessage(`{"process_id":"proc_test","wait_ms":600001}`))
	if err == nil || !strings.Contains(err.Error(), "wait_ms must be between 0 and 600000") {
		t.Fatalf("expected wait_ms validation error, got %v", err)
	}
}

func TestBackgroundProcessPublishesLifecycleEventsWithSource(t *testing.T) {
	events := make(chan BackgroundProcessEvent, 4)
	runner := NewBuiltinRunner(WithBackgroundProcessEvents(func(processEvent BackgroundProcessEvent) {
		events <- processEvent
	}))
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_events", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("sleep", "5000"),
	})
	started := decodeBackgroundProcessPayload(t, start)
	startEvent := awaitBackgroundProcessEvent(t, events)
	if startEvent.Phase != BackgroundProcessStarted || startEvent.SessionID != "sess_events" {
		t.Fatalf("unexpected start event: %+v", startEvent)
	}
	if startEvent.Process.ProcessID != started.ProcessID || startEvent.Process.TurnID != "turn_background" || startEvent.Process.CallID != "call_builtin_command_start" {
		t.Fatalf("background process source metadata is incomplete: %+v", startEvent.Process)
	}
	if stop := backgroundToolCall(runner, "sess_events", root, CommandStop, map[string]any{"process_id": started.ProcessID}); !stop.Ok {
		t.Fatalf("stop process: %+v", stop)
	}
	stopEvent := awaitBackgroundProcessEvent(t, events)
	if stopEvent.Phase != BackgroundProcessStopped || stopEvent.Process.Running || stopEvent.Process.Status != "stopped" {
		t.Fatalf("unexpected stop event: %+v", stopEvent)
	}
}

func TestBackgroundProcessEnforcesPerSessionLimit(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	for i := 0; i < backgroundProcessPerSessionLimit; i++ {
		result := backgroundToolCall(runner, "sess_limit", root, CommandStart, map[string]any{
			"scope": "project",
			"argv":  commandHelperArgs("sleep", "5000"),
		})
		if !result.Ok {
			t.Fatalf("start %d failed: %+v", i, result)
		}
	}
	overflow := backgroundToolCall(runner, "sess_limit", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("sleep", "5000"),
	})
	if overflow.Ok || !strings.Contains(overflow.Content, `"reason":"session_process_limit"`) {
		t.Fatalf("session process limit was not enforced: %+v", overflow)
	}
	runner.CloseSession("sess_limit")
	if runner.processes.runningTotal != 0 {
		t.Fatalf("session cleanup left running processes: %d", runner.processes.runningTotal)
	}
}

func TestBackgroundProcessRunsAndStopsLocalServer(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_server", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("http-server"),
	})
	started := decodeBackgroundProcessPayload(t, start)
	var nextOffset int64
	address := ""
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && address == "" {
		poll := decodeBackgroundProcessPayload(t, backgroundToolCall(runner, "sess_server", root, CommandPoll, map[string]any{
			"process_id": started.ProcessID,
			"offset":     nextOffset,
		}))
		nextOffset = poll.NextOffset
		for _, line := range strings.Split(backgroundOutputText(poll.Output), "\n") {
			if value, ok := strings.CutPrefix(line, "LISTEN "); ok {
				address = strings.TrimSpace(value)
			}
		}
		if address == "" {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if address == "" {
		t.Fatal("background server did not report its address")
	}
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	response, err := client.Get("http://" + address + "/health")
	if err != nil {
		t.Fatalf("background server is unreachable: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("unexpected background server response: status=%d body=%q", response.StatusCode, body)
	}
	stop := backgroundToolCall(runner, "sess_server", root, CommandStop, map[string]any{"process_id": started.ProcessID})
	if !stop.Ok {
		t.Fatalf("stop server: %+v", stop)
	}
	if _, err := client.Get("http://" + address + "/health"); err == nil {
		t.Fatal("background server remained reachable after stop")
	}
}

func TestBackgroundProcessRunningSurvivesRetentionTTL(t *testing.T) {
	runner := NewBuiltinRunner()
	_ = runner.processes.Close()
	runner.processes = newBackgroundProcessManager(100 * time.Millisecond)
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_ttl", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("sleep", "5000"),
	})
	payload := decodeBackgroundProcessPayload(t, start)
	time.Sleep(250 * time.Millisecond)
	process := runner.processes.Get("sess_ttl", payload.ProcessID)
	if process == nil {
		t.Fatal("running process was removed by finished-result retention")
	}
	process.mu.Lock()
	running := process.running
	process.mu.Unlock()
	if !running {
		t.Fatal("running process was stopped by finished-result retention")
	}
	if stop := backgroundToolCall(runner, "sess_ttl", root, CommandStop, map[string]any{"process_id": payload.ProcessID}); !stop.Ok {
		t.Fatalf("stop process: %+v", stop)
	}
}

func TestBackgroundProcessFinishedResultExpiresAfterRetention(t *testing.T) {
	runner := NewBuiltinRunner()
	_ = runner.processes.Close()
	runner.processes = newBackgroundProcessManager(100 * time.Millisecond)
	events := make(chan BackgroundProcessEvent, 4)
	runner.processes.events = func(processEvent BackgroundProcessEvent) { events <- processEvent }
	t.Cleanup(func() { _ = runner.Close() })
	root := t.TempDir()
	start := backgroundToolCall(runner, "sess_retention", root, CommandStart, map[string]any{
		"scope": "project",
		"argv":  commandHelperArgs("report"),
	})
	payload := decodeBackgroundProcessPayload(t, start)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && runner.processes.Get("sess_retention", payload.ProcessID) != nil {
		time.Sleep(20 * time.Millisecond)
	}
	if runner.processes.Get("sess_retention", payload.ProcessID) != nil {
		t.Fatal("finished process result was not removed after retention")
	}
	foundRemoved := false
	for !foundRemoved {
		processEvent := awaitBackgroundProcessEvent(t, events)
		foundRemoved = processEvent.Phase == BackgroundProcessRemoved
	}
}

func TestBackgroundProcessOutputRingUsesOffsetsAndTruncates(t *testing.T) {
	var buffer backgroundProcessOutputBuffer
	buffer.Append(ProgressStdout, []byte("head-"+strings.Repeat("x", backgroundProcessOutputLimit)+"-tail"))
	if buffer.bytes > backgroundProcessOutputLimit || buffer.baseOffset == 0 {
		t.Fatalf("output ring did not truncate: bytes=%d base=%d", buffer.bytes, buffer.baseOffset)
	}
	chunks, next, truncated, hasMore := buffer.Read(0, backgroundProcessPollMin)
	if !truncated || len(chunks) == 0 || next <= buffer.baseOffset || !hasMore {
		t.Fatalf("unexpected first ring read: chunks=%d next=%d truncated=%v more=%v", len(chunks), next, truncated, hasMore)
	}
	chunks, next, truncated, hasMore = buffer.Read(next, backgroundProcessPollMax)
	if truncated || len(chunks) == 0 || next <= buffer.baseOffset || !hasMore {
		t.Fatalf("unexpected continued ring read: chunks=%d next=%d truncated=%v more=%v", len(chunks), next, truncated, hasMore)
	}
}

func TestBackgroundProcessDetectsSplitSandboxDenial(t *testing.T) {
	process := &backgroundProcess{sandboxed: true}
	writer := backgroundProcessWriter{process: process, stream: ProgressStderr}
	if _, err := writer.Write([]byte("operation not")); err != nil {
		t.Fatal(err)
	}
	if process.sandboxDenialOutput {
		t.Fatal("partial marker must not report a sandbox denial")
	}
	if _, err := writer.Write([]byte(" permitted")); err != nil {
		t.Fatal(err)
	}
	if !process.sandboxDenialOutput {
		t.Fatal("sandbox denial split across output chunks was not detected")
	}
}

func TestBackgroundProcessStartUsesForegroundRiskRules(t *testing.T) {
	risk, ok := ClassifyToolCall(CommandStart, json.RawMessage(`{"scope":"project","argv":["go","test","./..."]}`))
	if !ok || risk.Class != RiskClassCommand || risk.Operation != "process_start" || !risk.LowRisk {
		t.Fatalf("background start risk is wrong: %+v ok=%v", risk, ok)
	}
}

func TestBackgroundProcessApprovalShowsCommandWithoutEnvironmentValues(t *testing.T) {
	runner := NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	details, err := runner.ApprovalDetails(context.Background(), Call{
		Name: CommandStart,
		Args: json.RawMessage(`{"scope":"project","script":"npm run dev","cwd":"web","env":{"PORT":"5173"}}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if details["script"] != "npm run dev" || details["cwd"] != "web" {
		t.Fatalf("approval command details are incomplete: %+v", details)
	}
	keys, ok := details["envKeys"].([]string)
	if !ok || len(keys) != 1 || keys[0] != "PORT" {
		t.Fatalf("approval env keys are wrong: %+v", details)
	}
	if _, exists := details["env"]; exists {
		t.Fatalf("approval must not expose environment values: %+v", details)
	}
}

func backgroundToolCall(runner *BuiltinRunner, sessionID, root, name string, args map[string]any) Result {
	raw, _ := json.Marshal(args)
	return runner.Call(context.Background(), Call{
		SessionID:   sessionID,
		TurnID:      "turn_background",
		CallID:      "call_" + name,
		Name:        name,
		Args:        raw,
		ProjectDirs: []string{root},
	})
}

func awaitBackgroundProcessEvent(t *testing.T, events <-chan BackgroundProcessEvent) BackgroundProcessEvent {
	t.Helper()
	select {
	case processEvent := <-events:
		return processEvent
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for background process event")
		return BackgroundProcessEvent{}
	}
}

func decodeBackgroundProcessPayload(t *testing.T, result Result) backgroundProcessPayload {
	t.Helper()
	var payload backgroundProcessPayload
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode background process result: %v content=%q", err, result.Content)
	}
	return payload
}

func backgroundOutputText(chunks []backgroundProcessOutputChunk) string {
	var out strings.Builder
	for _, chunk := range chunks {
		out.WriteString(chunk.Content)
	}
	return out.String()
}

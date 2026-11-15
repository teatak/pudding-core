package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type collaborationPlugins struct{ enabled atomic.Bool }

func (a *collaborationPlugins) ListDefinitions(context.Context) ([]*plugin.Definition, error) {
	defs := plugin.BuiltinDefinitions()
	for _, d := range defs {
		if d.ID == plugin.BuiltinSubsessionID {
			d.Enabled = a.enabled.Load()
		}
	}
	return defs, nil
}
func (*collaborationPlugins) ReadSkill(_ context.Context, id, skill string) (*plugin.SkillDetail, error) {
	d, _ := plugin.ReadBuiltinSkill(id, skill)
	return d, nil
}

func newCollaborationEngine(t *testing.T, client provider.Client) (*Engine, *storetest.Store, *collaborationPlugins) {
	t.Helper()
	ctx := context.Background()
	st := storetest.New(t)
	plugins := &collaborationPlugins{}
	plugins.enabled.Store(true)
	if err := st.CreateSession(ctx, &store.Session{ID: "root", Title: "Root", Provider: client.Name(), Model: "model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession, LoadedPluginIDs: []string{plugin.BuiltinSubsessionID}}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProviderProfile(ctx, &store.ProviderProfile{ID: client.Name(), Protocol: "openai-compatible", BaseURL: "http://example.invalid", Models: []store.ProviderModel{{ID: "model"}}}); err != nil {
		t.Fatal(err)
	}
	eng := New(st, event.NewHub(), mapResolver{client.Name(): client}, st, WithPlugins(plugins))
	t.Cleanup(func() { eng.Stop(); eng.Wait() })
	return eng, st, plugins
}

func TestCollaborationAdmissionStopAndGate(t *testing.T) {
	ctx := context.Background()
	eng, st, plugins := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"waiting"}), mock.WithDelay(time.Minute)))
	root, err := eng.Submit(ctx, SubmitInput{SessionID: "root", ClientMessageID: "root_input", Text: "ROOT"})
	if err != nil {
		t.Fatal(err)
	}
	var children []*store.Session
	for _, id := range []string{"one", "two", "three"} {
		child, err := eng.dispatchChild(ctx, "root", root.TurnID, id, id, "CHILD", store.ModeWork)
		if err != nil {
			t.Fatal(err)
		}
		children = append(children, child)
	}
	replay, err := eng.dispatchChild(ctx, "root", root.TurnID, "one", "replay", "CHILD", store.ModeWork)
	if err != nil || replay.ID != children[0].ID {
		t.Fatalf("dispatch replay: %+v %v", replay, err)
	}
	running := 0
	for _, child := range children {
		if _, err := st.RunningTurn(ctx, child.ID); err == nil {
			running++
		}
	}
	if running != 3 {
		t.Fatalf("running children=%d", running)
	}
	if _, err := eng.dispatchChild(ctx, "root", root.TurnID, "four", "four", "CHILD", store.ModeWork); !errors.Is(err, store.ErrSubtaskLimit) {
		t.Fatalf("fourth dispatch: %v", err)
	}
	// Previously existing children can still receive work; the concurrency
	// cap remains independent of the new-child creation budget.
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "existing", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.sendChild(ctx, "root", root.TurnID, "existing", "reuse", "CHILD"); err != nil {
		t.Fatal(err)
	}
	if queued, _ := st.HasQueuedInputs(ctx, "existing"); !queued {
		t.Fatal("reused fourth child should queue")
	}
	// Children do not even receive the recursive tool definitions.
	ids := []string{plugin.BuiltinSubsessionID}
	if _, err := st.UpdateSession(ctx, children[0].ID, store.SessionUpdate{LoadedPluginIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, children[0].ID, store.ModeWork)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		if tool.IsSubtaskTool(def.Name) {
			t.Fatal("recursive tools exposed")
		}
	}
	plugins.enabled.Store(false)
	if _, err := eng.dispatchChild(ctx, "root", root.TurnID, "disabled", "disabled", "CHILD", store.ModeWork); err == nil {
		t.Fatal("disabled App accepted dispatch")
	}
	if busy, err := eng.childrenBusy(ctx, "root"); err != nil || !busy {
		t.Fatal("disabling App cancelled accepted work")
	}
	if err := eng.StopCollaboration(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := eng.waitForChildren(waitCtx, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RunningTurn(ctx, "root"); err != nil {
		t.Fatal("stop collaboration cancelled main")
	}
	plugins.enabled.Store(true)
	if _, err := eng.dispatchChild(ctx, "root", root.TurnID, "late", "late", "CHILD", store.ModeWork); err == nil {
		t.Fatal("stop did not block late dispatch")
	}
	if err := eng.Cancel("root"); err != nil {
		t.Fatal(err)
	}
}

func TestCollaborationWakeUsesLatestRetry(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"integrated"})))
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"root", "child"} {
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: id, TurnID: id + "_failed", ClientMessageID: id + "_input", UserMessageID: id + "_message", UserText: "work"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id + "_failed", Status: store.TurnFailed}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: id, TurnID: id + "_retry", RetryOfTurnID: id + "_failed", ClientMessageID: id + "_retry_input", SystemMessageID: id + "_retry_message", Text: "retry"}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id + "_retry", Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	eng.wakeForChildResults("root")
	eng.Wait()
	page, err := st.ListTurnsPage(ctx, "root", "", 1)
	if err != nil || len(page.Turns) != 1 || page.Turns[0].ClientMessageID != "collaboration_wake_child_retry_" || page.Turns[0].Status != store.TurnCompleted {
		t.Fatalf("latest retry did not wake main: %+v %v", page, err)
	}
}

type collaborationClient struct {
	plugins  *collaborationPlugins
	mu       sync.Mutex
	requests []provider.Request
}

func (*collaborationClient) Name() string { return "collaboration-test" }
func (c *collaborationClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	raw, _ := json.Marshal(req.Messages)
	body := string(raw)
	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()
	if !strings.Contains(body, "ROOT") {
		c.plugins.enabled.Store(false)
		return mock.New(mock.WithScript([]string{"child result"}), mock.WithDelay(20*time.Millisecond)).Stream(ctx, req)
	}
	out := make(chan provider.Chunk, 2)
	if !strings.Contains(body, tool.SubtaskDispatch) {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "dispatch", Name: tool.SubtaskDispatch, ArgsDelta: `{"title":"Research","prompt":"CHILD"}`}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		text := "awaiting child"
		if strings.Contains(body, "child result") {
			text = "integrated child result"
		}
		out <- provider.Chunk{Delta: text}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func TestCollaborationCollectsWithoutEnabledPluginAndAcceptsChildFollowup(t *testing.T) {
	client := &collaborationClient{}
	eng, st, plugins := newCollaborationEngine(t, client)
	client.plugins = plugins
	ctx := context.Background()
	main, err := eng.Submit(ctx, SubmitInput{SessionID: "root", ClientMessageID: "input", Text: "ROOT"})
	if err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	turn, err := st.GetConversationTurn(ctx, "root", main.TurnID)
	if err != nil || turn.Status != store.TurnCompleted {
		t.Fatalf("main: %+v %v", turn, err)
	}
	children, err := st.ListChildSessions(ctx, "root")
	if err != nil || len(children) != 1 {
		t.Fatalf("children: %v %v", children, err)
	}
	messages, err := st.ListMessages(ctx, "root", 0)
	if err != nil {
		t.Fatal(err)
	}
	results := 0
	integrated := false
	for _, msg := range messages {
		if msg.Kind == "collaboration_result" {
			results++
		}
		if msg.Role == store.RoleAssistant && strings.Contains(msg.Text, "integrated child result") {
			integrated = true
		}
	}
	if results != 1 || !integrated {
		t.Fatalf("results=%d integrated=%v", results, integrated)
	}
	if plugins.enabled.Load() {
		t.Fatal("test did not disable App")
	}
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: children[0].ID, ClientMessageID: "followup", Text: "revise CHILD"}); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	messages, _ = st.ListMessages(ctx, "root", 0)
	results = 0
	for _, msg := range messages {
		if msg.Kind == "collaboration_result" {
			results++
		}
	}
	if results != 2 {
		t.Fatalf("updated result not delivered exactly once: %d", results)
	}
}

func TestCollaborationResourceWaitIsCancellable(t *testing.T) {
	eng, _, _ := newCollaborationEngine(t, mock.New())
	call := tool.Call{SessionID: "root", Name: tool.CommandRun, Args: json.RawMessage(`{"scope":"project","command":"touch file"}`), ProjectDirs: []string{t.TempDir()}}
	release, err := eng.acquireToolResources(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	call.SessionID = "peer" // A different session must still serialize writes to the same project.
	if second, err := eng.acquireToolResources(ctx, call); err == nil {
		second()
		t.Fatal("overlapping mutation acquired resource")
	}
	release()
	second, err := eng.acquireToolResources(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	second()
}

func TestCollaborationBackgroundProcessAllowsProjectWork(t *testing.T) {
	eng, _, _ := newCollaborationEngine(t, mock.New())
	root := t.TempDir()
	eng.attachmentHome = t.TempDir()
	runner := tool.NewBuiltinRunner(tool.WithHomeDir(eng.attachmentHome))
	eng.tools = runner
	t.Cleanup(func() { _ = runner.Close() })
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	call := tool.Call{SessionID: "root", TurnID: "background_turn", CallID: "background_call", Name: tool.CommandRun, Mode: store.ModeCode, ProjectDirs: []string{root}, CommandSandbox: tool.CommandSandboxBypass}
	call.Args, _ = json.Marshal(map[string]any{"scope": "project", "command": collaborationCommandHelper(t, "background"), "background": true})
	started := collaborationResourceCall(t, eng, call)
	var process struct {
		ProcessID string `json:"processID"`
		Running   bool   `json:"running"`
	}
	if err := json.Unmarshal([]byte(started.Content), &process); err != nil || !started.Ok || !process.Running || process.ProcessID == "" {
		t.Fatalf("background start: %s (%v)", started.Content, err)
	}
	poll := call
	poll.Name, poll.CallID = tool.CommandSession, "background_ready"
	poll.Args, _ = json.Marshal(map[string]any{"action": "poll", "process_id": process.ProcessID, "wait_ms": 5000})
	if result := collaborationResourceCall(t, eng, poll); !result.Ok || !strings.Contains(result.Content, "ready") {
		t.Fatalf("background helper was not ready: %s", result.Content)
	}
	for _, sessionID := range []string{"root", "peer"} {
		patch := call
		patch.SessionID, patch.TurnID, patch.CallID, patch.Name = sessionID, sessionID+"_edit", sessionID+"_patch", tool.FilePatch
		old, next := "before", "root edit"
		if sessionID == "peer" {
			old, next = "root edit", "peer edit"
		}
		patch.Args, _ = json.Marshal(map[string]any{"scope": "project", "files": []map[string]any{{"path": "notes.txt", "action": "edit", "hunks": []map[string]any{{"old_lines": []string{old}, "new_lines": []string{next}}}}}})
		if result := collaborationResourceCall(t, eng, patch); !result.Ok {
			t.Fatalf("%s edit while background process is running: %s", sessionID, result.Content)
		}
		if content, err := os.ReadFile(filepath.Join(root, "notes.txt")); err != nil || string(content) != next+"\n" {
			t.Fatalf("%s edit was not applied: %q (%v)", sessionID, content, err)
		}
	}
	check := call
	check.CallID = "background_check"
	check.Args = json.RawMessage(`{"scope":"project","command":"echo checked","yield_ms":5000}`)
	result := collaborationResourceCall(t, eng, check)
	var checked struct {
		ExitCode *int   `json:"exitCode"`
		Stdout   string `json:"stdout"`
	}
	if err := json.Unmarshal([]byte(result.Content), &checked); err != nil || !result.Ok || checked.ExitCode == nil || *checked.ExitCode != 0 || strings.TrimSpace(checked.Stdout) != "checked" {
		t.Fatalf("command did not complete while background process is running: %s (%v)", result.Content, err)
	}
	workspace := call
	workspace.Name, workspace.CallID = tool.TempWorkspace, "background_workspace"
	workspace.Args = json.RawMessage(`{"action":"open"}`)
	opened := collaborationResourceCall(t, eng, workspace)
	var scratch struct {
		Path         string `json:"path"`
		AbsolutePath string `json:"absolutePath"`
	}
	if err := json.Unmarshal([]byte(opened.Content), &scratch); err != nil || !opened.Ok || scratch.AbsolutePath == "" {
		t.Fatalf("open workspace while background process is running: %s (%v)", opened.Content, err)
	}
	previewPath := filepath.Join(scratch.AbsolutePath, "preview.txt")
	if err := os.WriteFile(previewPath, []byte("preview ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := call
	read.Name, read.CallID = tool.FileRead, "background_preview"
	read.Args, _ = json.Marshal(map[string]any{"scope": "temp", "path": filepath.Join(scratch.Path, "preview.txt")})
	if result := collaborationResourceCall(t, eng, read); !result.Ok || !strings.Contains(result.Content, "preview ready") {
		t.Fatalf("read preview while background process is running: %s", result.Content)
	}
	processes := eng.BackgroundProcesses(call.SessionID)
	if len(processes) != 1 || processes[0].ProcessID != process.ProcessID || !processes[0].Running {
		t.Fatalf("follow-up work stopped the background process: %+v", processes)
	}
	workspace.Args = json.RawMessage(`{"action":"clear"}`)
	if result := collaborationResourceCall(t, eng, workspace); result.Ok || !strings.Contains(result.Content, "workspace_in_use") {
		t.Fatalf("cleanup must reject the active background process: %s", result.Content)
	}
	if content, err := os.ReadFile(previewPath); err != nil || string(content) != "preview ready" {
		t.Fatalf("rejected cleanup removed the preview: %q (%v)", content, err)
	}
	stop := poll
	stop.CallID = "background_stop"
	stop.Args, _ = json.Marshal(map[string]any{"action": "stop", "process_id": process.ProcessID})
	if result := collaborationResourceCall(t, eng, stop); !result.Ok {
		t.Fatalf("stop background process: %s", result.Content)
	}
	if result := collaborationResourceCall(t, eng, workspace); !result.Ok {
		t.Fatalf("cleanup after background process stopped: %s", result.Content)
	}
	if _, err := os.Stat(previewPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("scratch file survived cleanup: %v", err)
	}
}

func TestCollaborationYieldedForegroundRetainsProjectLease(t *testing.T) {
	eng, _, _ := newCollaborationEngine(t, mock.New())
	root := t.TempDir()
	runner := tool.NewBuiltinRunner()
	eng.tools = runner
	t.Cleanup(func() { _ = runner.Close() })
	path, gate := filepath.Join(root, "notes.txt"), filepath.Join(root, "continue")
	if err := os.WriteFile(path, []byte("before\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	call := tool.Call{SessionID: "root", TurnID: "foreground_turn", CallID: "foreground_call", Name: tool.CommandRun, Mode: store.ModeCode, ProjectDirs: []string{root}, CommandSandbox: tool.CommandSandboxBypass}
	call.Args, _ = json.Marshal(map[string]any{"scope": "project", "command": collaborationCommandHelper(t, "foreground", gate, path), "yield_ms": 0, "mutation_paths": []string{path}})
	started := eng.callTrackedTool(ctx, call.SessionID, call.TurnID, store.ModeCode, call)
	var process struct {
		ProcessID string `json:"processID"`
		Yielded   bool   `json:"yielded"`
	}
	if err := json.Unmarshal([]byte(started.Content), &process); err != nil || !started.Ok || !process.Yielded || process.ProcessID == "" {
		t.Fatalf("foreground command did not yield: %s (%v)", started.Content, err)
	}
	patch := call
	patch.SessionID, patch.TurnID, patch.CallID, patch.Name = "peer", "peer_turn", "peer_patch", tool.FilePatch
	patch.Args = json.RawMessage(`{"scope":"project","files":[{"path":"notes.txt","action":"edit","hunks":[{"old_lines":["command"],"new_lines":["after"]}]}]}`)
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	blocked := eng.callTrackedTool(waitCtx, patch.SessionID, patch.TurnID, store.ModeCode, patch)
	waitCancel()
	if blocked.Ok || !strings.Contains(blocked.Content, context.DeadlineExceeded.Error()) {
		t.Fatalf("another session reached the file before foreground completion: %s", blocked.Content)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "before\n" {
		t.Fatalf("pending foreground mutation changed early: %q (%v)", content, err)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	poll := call
	poll.Name, poll.CallID = tool.CommandSession, "foreground_poll"
	var offset int64
	for {
		poll.Args, _ = json.Marshal(map[string]any{"action": "poll", "process_id": process.ProcessID, "offset": offset, "wait_ms": 5000})
		result := eng.callTrackedTool(ctx, poll.SessionID, poll.TurnID, store.ModeCode, poll)
		var state struct {
			Running    bool  `json:"running"`
			NextOffset int64 `json:"nextOffset"`
			ExitCode   *int  `json:"exitCode"`
		}
		if err := json.Unmarshal([]byte(result.Content), &state); err != nil || !result.Ok || ctx.Err() != nil {
			t.Fatalf("foreground completion: %s (%v, %v)", result.Content, err, ctx.Err())
		}
		offset = state.NextOffset
		if !state.Running {
			if state.ExitCode == nil || *state.ExitCode != 0 {
				t.Fatalf("foreground command failed: %s", result.Content)
			}
			break
		}
	}
	changes, err := eng.turnFiles.Finish(call.TurnID)
	if err != nil || len(changes) != 1 || changes[0].OldContent != "before\n" || changes[0].NewContent != "command\n" || changes[0].Origin != store.FileChangeOriginCommandObserved {
		t.Fatalf("yielded foreground mutation baseline was not completed: %+v (%v)", changes, err)
	}
	if result := collaborationResourceCall(t, eng, patch); !result.Ok {
		t.Fatalf("project lease was not released after foreground completion: %s", result.Content)
	}
	if content, err := os.ReadFile(path); err != nil || string(content) != "after\n" {
		t.Fatalf("following edit was not applied: %q (%v)", content, err)
	}
}

func collaborationResourceCall(t *testing.T, eng *Engine, call tool.Call) tool.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := eng.callTrackedTool(ctx, call.SessionID, call.TurnID, store.ModeCode, call)
	if ctx.Err() != nil {
		t.Fatalf("%s did not complete while the managed process was running: %v", call.Name, ctx.Err())
	}
	return result
}

func collaborationCommandHelper(t *testing.T, args ...string) string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	argv := append([]string{executable, "-test.run=^TestCollaborationCommandHelperProcess$", "--"}, args...)
	escapedQuote := "'\\''"
	if runtime.GOOS == "windows" {
		escapedQuote = "''"
	}
	for i, arg := range argv {
		argv[i] = "'" + strings.ReplaceAll(arg, "'", escapedQuote) + "'"
	}
	command := strings.Join(argv, " ")
	if runtime.GOOS == "windows" {
		command = "& " + command
	}
	return command
}

func TestCollaborationCommandHelperProcess(t *testing.T) {
	var args []string
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "background":
		_, _ = os.Stdout.WriteString("ready\n")
		time.Sleep(time.Minute)
	case "foreground":
		if len(args) != 3 {
			os.Exit(2)
		}
		deadline := time.Now().Add(20 * time.Second)
		for {
			if _, err := os.Stat(args[1]); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(3)
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err := os.WriteFile(args[2], []byte("command\n"), 0o600); err != nil {
			os.Exit(4)
		}
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

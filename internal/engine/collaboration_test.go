package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

type collaborationApps struct{ enabled atomic.Bool }

func (a *collaborationApps) ListDefinitions(context.Context) ([]*app.Definition, error) {
	defs := app.BuiltinDefinitions()
	for _, d := range defs {
		if d.ID == app.BuiltinCollaborationID {
			d.Enabled = a.enabled.Load()
		}
	}
	return defs, nil
}
func (*collaborationApps) ReadSkill(_ context.Context, id, skill string) (*app.SkillDetail, error) {
	d, _ := app.ReadBuiltinSkill(id, skill)
	return d, nil
}

func newCollaborationEngine(t *testing.T, client provider.Client) (*Engine, *memstore.Memstore, *collaborationApps) {
	t.Helper()
	ctx := context.Background()
	st := memstore.New()
	apps := &collaborationApps{}
	apps.enabled.Store(true)
	if err := st.CreateSession(ctx, &store.Session{ID: "root", Title: "Root", Provider: client.Name(), Model: "model", ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession, LoadedAppIDs: []string{app.BuiltinCollaborationID}}); err != nil {
		t.Fatal(err)
	}
	if err := st.PutProviderProfile(ctx, &store.ProviderProfile{ID: client.Name(), Protocol: "openai-compatible", BaseURL: "http://example.invalid", Models: []store.ProviderModel{{ID: "model"}}}); err != nil {
		t.Fatal(err)
	}
	eng := New(st, event.NewHub(), mapResolver{client.Name(): client}, st, WithApps(apps))
	t.Cleanup(func() { eng.Stop(); eng.Wait() })
	return eng, st, apps
}

func TestCollaborationAdmissionStopAndGate(t *testing.T) {
	ctx := context.Background()
	eng, st, apps := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"waiting"}), mock.WithDelay(time.Minute)))
	root, err := eng.Submit(ctx, SubmitInput{SessionID: "root", ClientMessageID: "root_input", Text: "ROOT"})
	if err != nil {
		t.Fatal(err)
	}
	var children []*store.Session
	for _, id := range []string{"one", "two", "three", "four"} {
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
	if queued, _ := st.HasQueuedInputs(ctx, children[3].ID); !queued {
		t.Fatal("fourth child not queued")
	}
	// Children do not even receive the recursive tool definitions.
	ids := []string{app.BuiltinCollaborationID}
	if _, err := st.UpdateSession(ctx, children[0].ID, store.SessionUpdate{LoadedAppIDs: &ids}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, children[0].ID, store.ModeWork)
	if err != nil {
		t.Fatal(err)
	}
	for _, def := range defs {
		if tool.IsCollaborationTool(def.Name) {
			t.Fatal("recursive tools exposed")
		}
	}
	apps.enabled.Store(false)
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
	apps.enabled.Store(true)
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
	apps     *collaborationApps
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
		c.apps.enabled.Store(false)
		return mock.New(mock.WithScript([]string{"child result"}), mock.WithDelay(20*time.Millisecond)).Stream(ctx, req)
	}
	out := make(chan provider.Chunk, 2)
	if !strings.Contains(body, tool.CollaborationDispatch) {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "dispatch", Name: tool.CollaborationDispatch, ArgsDelta: `{"title":"Research","prompt":"CHILD"}`}}
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

func TestCollaborationCollectsWithoutEnabledAppAndAcceptsChildFollowup(t *testing.T) {
	client := &collaborationClient{}
	eng, st, apps := newCollaborationEngine(t, client)
	client.apps = apps
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
	if apps.enabled.Load() {
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
	call := tool.Call{Name: tool.CommandRun, Args: json.RawMessage(`{"command":"touch file"}`), ProjectDirs: []string{t.TempDir()}}
	release, err := eng.acquireToolResources(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
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

func TestCollaborationBackgroundProcessRetainsProjectLease(t *testing.T) {
	eng, _, _ := newCollaborationEngine(t, mock.New())
	eng.tools = tool.NewBuiltinRunner()
	root := t.TempDir()
	call := tool.Call{SessionID: "root", TurnID: "background_turn", CallID: "background_call", Name: tool.CommandRun, Mode: store.ModeCode, ProjectDirs: []string{root}, CommandSandbox: tool.CommandSandboxBypass, Args: json.RawMessage(`{"scope":"project","command":"sleep 20","background":true}`)}
	result := eng.callTrackedTool(context.Background(), "root", call.TurnID, store.ModeCode, call)
	if !result.Ok {
		t.Fatalf("background start: %s", result.Content)
	}
	processes := eng.BackgroundProcesses("root")
	if len(processes) != 1 || !processes[0].Running {
		t.Fatalf("background processes: %+v", processes)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if release, err := eng.acquireToolResources(ctx, call); err == nil {
		release()
		t.Fatal("background handle released project lease before process exited")
	}
	if _, err := eng.StopBackgroundProcess("root", processes[0].ProcessID); err != nil {
		t.Fatal(err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	release, err := eng.acquireToolResources(waitCtx, call)
	if err != nil {
		t.Fatalf("project lease not released after process exit: %v", err)
	}
	release()
}

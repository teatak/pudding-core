package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type subagentResetExecutionProbe struct {
	mu       sync.Mutex
	requests []string
}

func (*subagentResetExecutionProbe) Name() string { return "reset-execution" }
func (p *subagentResetExecutionProbe) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	p.requests = append(p.requests, string(raw))
	p.mu.Unlock()
	return smokeTextStream("FRESH_RESULT"), nil
}
func (p *subagentResetExecutionProbe) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.requests...)
}

func TestSubagentResetToolsExecuteFreshWorkWithoutMaintenanceWake(t *testing.T) {
	ctx := context.Background()
	probe := &subagentResetExecutionProbe{}
	eng, st, _ := newCollaborationEngine(t, probe)
	if err := st.CreateChildSession(ctx, "root", &store.Session{ID: "child", Title: "Child", Provider: probe.Name(), Model: "model"}); err != nil {
		t.Fatal(err)
	}
	begin := func(session, turn, text string) {
		t.Helper()
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: session, TurnID: turn, ClientMessageID: turn, UserMessageID: turn + "-input", UserText: text}); err != nil {
			t.Fatal(err)
		}
	}
	finish := func(turn string) {
		t.Helper()
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: turn, Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	begin("child", "old-child", "PRE_RESET_PRIVATE_TASK")
	finish("old-child")
	if _, err := st.CollectChildResults(ctx, "root"); err != nil {
		t.Fatal(err)
	}
	before, err := st.ListMessages(ctx, "child", 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.SetSubtaskArchived(ctx, "root", "child", true); err != nil {
		t.Fatal(err)
	}
	run := func(parentTurn, callID, name string, args map[string]any) store.SubagentResetResult {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		result := eng.executeSubtask(ctx, "root", parentTurn, store.ModeWork, tool.Call{Name: name, CallID: callID, Args: raw})
		var receipt store.SubagentResetResult
		if !result.Ok || json.Unmarshal([]byte(result.Content), &receipt) != nil || !receipt.ContextReset {
			t.Fatalf("reset tool failed: %+v", result)
		}
		return receipt
	}
	begin("root", "reset-request", "reset the idle child")
	reset := run("reset-request", "reset", tool.SubtaskReset, map[string]any{"session_id": "child", "expected_turn_id": "old-child"})
	if reset.Queued {
		t.Fatal("standalone reset started a task")
	}
	finish("reset-request")
	eng.childTurnFinished("root", store.TurnCompleted)
	eng.Wait()
	if requests := probe.snapshot(); len(requests) != 0 {
		t.Fatalf("maintenance checkpoint woke a model: %v", requests)
	}
	views, err := eng.ListChildSessions(ctx, "root")
	if err != nil || len(views) != 1 || views[0].Status != "idle" || views[0].ArchivedAt == nil || !views[0].ResultCollected || !eng.subagentCanReset(views[0]) {
		t.Fatalf("standalone reset projection: %+v %v", views, err)
	}
	begin("root", "fresh-request", "send an independent task")
	args := map[string]any{"session_id": "child", "expected_turn_id": reset.ResetTurnID, "reset_context": true, "prompt": "FRESH_SELF_CONTAINED_TASK"}
	sent := run("fresh-request", "send", tool.SubtaskSend, args)
	if !sent.Queued || sent.Duplicate {
		t.Fatalf("reset-and-send did not enqueue fresh work: %+v", sent)
	}
	replay := run("fresh-request", "send", tool.SubtaskSend, args)
	if !replay.Duplicate || replay.ResetTurnID != sent.ResetTurnID || replay.ClientMessageID != sent.ClientMessageID {
		t.Fatalf("reset-and-send replay changed its receipt: %+v", replay)
	}
	eng.Wait()
	requests := probe.snapshot()
	if len(requests) != 1 || !strings.Contains(requests[0], "FRESH_SELF_CONTAINED_TASK") || strings.Contains(requests[0], "PRE_RESET_PRIVATE_TASK") {
		t.Fatalf("provider context was not fresh, or task ran twice: %v", requests)
	}
	if archives, err := st.ListSubtaskArchives(ctx, "root"); err != nil || len(archives) != 0 {
		t.Fatalf("reset-and-send did not restore the child: %v %v", archives, err)
	}
	after, err := st.ListMessages(ctx, "child", 0)
	if err != nil || len(after) < len(before) || !reflect.DeepEqual(before, after[:len(before)]) {
		t.Fatalf("fresh work rewrote canonical old history: %v", err)
	}
	views, err = eng.ListChildSessions(ctx, "root")
	if err != nil || len(views) != 1 || views[0].Status != "completed" || views[0].LatestTurnID == sent.ResetTurnID {
		t.Fatalf("replacement work did not complete: %+v %v", views, err)
	}
	resultTurnID := views[0].LatestTurnID
	finish("fresh-request")
	eng.childTurnFinished("root", store.TurnCompleted)
	eng.Wait()
	if _, err := st.GetMessage(ctx, "root", "collaboration_result_"+resultTurnID); err != nil {
		t.Fatalf("real replacement result did not wake/collect: %v", err)
	}
	for _, turnID := range []string{reset.ResetTurnID, sent.ResetTurnID} {
		if _, err := st.GetMessage(ctx, "root", "collaboration_result_"+turnID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("maintenance turn received a result receipt: %s %v", turnID, err)
		}
	}
	eng.childTurnFinished("child", store.TurnCompleted) // A repeated completion callback is harmless.
	eng.Wait()
	if requests := probe.snapshot(); len(requests) != 2 {
		t.Fatalf("real result did not wake exactly once: %v", requests)
	}
}

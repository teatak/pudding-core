package engine

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type revocationComputerRunner struct {
	*tool.BuiltinRunner
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (r *revocationComputerRunner) Call(ctx context.Context, call tool.Call) tool.Result {
	if r.calls.Add(1) == 1 {
		close(r.entered)
		select {
		case <-r.release:
		case <-ctx.Done():
			return tool.Result{CallID: call.CallID, Name: call.Name, Content: ctx.Err().Error()}
		}
	}
	return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: `{"ok":true}`}
}

func TestComputerAppRevocationRequiresFreshApprovalAfterInflightCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	st := storetest.New(t)
	if err := st.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ActiveMode: store.ModeWork}); err != nil {
		t.Fatal(err)
	}
	if err := st.GrantComputerApp(ctx, "session", "com.example.App"); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	events, unsub := hub.Subscribe("session")
	defer unsub()
	runner := &revocationComputerRunner{BuiltinRunner: tool.NewBuiltinRunner(tool.WithHomeDir(t.TempDir())), entered: make(chan struct{}), release: make(chan struct{})}
	eng := New(st, hub, registry.Static(mock.New()), st, WithTools(runner))
	t.Cleanup(eng.Stop)
	execute := func(id string) <-chan tool.Result {
		ch := make(chan tool.Result, 1)
		go func() {
			ch <- eng.executeAllowedTool(ctx, "session", "turn", store.ModeWork, tool.Call{SessionID: "session", TurnID: "turn", CallID: id, Name: tool.ComputerUseApp, Args: json.RawMessage(`{"appID":"com.example.App"}`)})
		}()
		return ch
	}
	result := func(ch <-chan tool.Result, wantOK bool) {
		t.Helper()
		select {
		case got := <-ch:
			if got.Ok != wantOK {
				t.Fatalf("tool result: %+v", got)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	approval := func() string {
		t.Helper()
		for {
			select {
			case ev := <-events:
				if ev.Kind == event.ApprovalRequested {
					return ev.ApprovalID
				}
			case <-ctx.Done():
				t.Fatal("next access did not request approval")
				return ""
			}
		}
	}
	first := execute("inflight")
	select {
	case <-runner.entered:
	case <-ctx.Done():
		t.Fatal("first call did not start")
	}
	if err := st.RevokeComputerAppGrant(ctx, "session", "com.example.App"); err != nil {
		t.Fatal(err)
	}
	close(runner.release)
	result(first, true) // Revocation does not undo an operation already admitted.
	next := execute("denied-after-revoke")
	id := approval()
	if runner.calls.Load() != 1 {
		t.Fatal("revoked app executed before fresh approval")
	}
	if err := eng.DenyApproval(ctx, "session", id, "no"); err != nil {
		t.Fatal(err)
	}
	result(next, false)
	if granted, _ := st.HasComputerAppGrant(ctx, "session", "com.example.App"); granted {
		t.Fatal("denial restored consent")
	}
	next = execute("approved-again")
	id = approval()
	if err := eng.ApproveApproval(ctx, "session", id, ApprovalScopeSession, nil); err != nil {
		t.Fatal(err)
	}
	result(next, true)
	if runner.calls.Load() != 2 {
		t.Fatalf("executed %d calls, want 2", runner.calls.Load())
	}
	if granted, err := st.HasComputerAppGrant(ctx, "session", "com.example.App"); err != nil || !granted {
		t.Fatalf("fresh approval was not saved: %v %v", granted, err)
	}
}

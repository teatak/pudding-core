package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestUnsupportedSessionApprovalRemainsPending(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ms := storetest.New(t)
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock", ActiveMode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	eng := New(ms, hub, registry.Static(mock.New()), ms)
	defer eng.Stop()
	sub, unsubscribe := hub.Subscribe("session")
	defer unsubscribe()
	done := make(chan bool, 1)
	go func() {
		_, approved := eng.requestToolCallApproval(ctx, "session", "turn", tool.Call{Name: tool.CommandRun, CallID: "call"}, tool.ToolRisk{}, nil, map[string]any{"execution": "host", "command": "echo one; echo two"}, "", nil, nil)
		done <- approved
	}()
	var approvalID string
	select {
	case ev := <-sub:
		if ev.Kind != event.ApprovalRequested {
			t.Fatalf("unexpected event: %s", ev.Kind)
		}
		approvalID = ev.ApprovalID
	case <-ctx.Done():
		t.Fatal("no approval request")
	}
	if err := eng.ApproveApproval(ctx, "session", approvalID, ApprovalScopeSession, nil); !errors.Is(err, ErrApprovalScopeUnsupported) {
		t.Fatalf("unsupported session scope was silently accepted: %v", err)
	}
	if pending := eng.PendingApprovals("session"); len(pending) != 1 || pending[0].ID != approvalID {
		t.Fatalf("approval no longer retryable: %+v", pending)
	}
	select {
	case <-done:
		t.Fatal("unsupported session approval executed command")
	default:
	}
	if err := eng.ApproveApproval(ctx, "session", approvalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	select {
	case approved := <-done:
		if !approved {
			t.Fatal("explicit one-time approval did not proceed")
		}
	case <-ctx.Done():
		t.Fatal("one-time retry did not finish")
	}
	if len(eng.PendingApprovals("session")) != 0 {
		t.Fatal("one-time approval left pending request")
	}
	if eng.CommandApprovals("session").GrantCount != 0 {
		t.Fatal("unsupported command created a reusable grant")
	}
}

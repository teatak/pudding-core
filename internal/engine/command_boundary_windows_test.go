package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestWindowsSandboxRejectionPrecedesApprovalAndDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	ms := storetest.New(t)
	root := t.TempDir()
	if err := ms.CreateProject(ctx, &store.Project{ID: "p", RootDirs: []string{root}, ApprovalMode: store.ApprovalAuto}); err != nil {
		t.Fatal(err)
	}
	if err := ms.CreateSession(ctx, &store.Session{ID: "s", ProjectID: "p", Provider: "mock", Model: "mock", ActiveMode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}
	hub := event.NewHub()
	events, unsub := hub.Subscribe("s")
	defer unsub()
	calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true}}
	eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
	t.Cleanup(eng.Stop)
	result := eng.executeAllowedTool(ctx, "s", "turn", store.ModeCode, tool.Call{SessionID: "s", CallID: "call", Name: tool.CommandRun, Args: json.RawMessage(`{"scope":"project","command":"& .\\build.ps1"}`)})
	if result.Ok || !strings.Contains(result.Content, "host_access_required") || len(calls.calls) != 0 {
		t.Fatalf("unexpected dispatch: %+v", result)
	}
	select {
	case ev := <-events:
		if ev.Kind == event.ApprovalRequested {
			t.Fatal("unsupported sandbox requested approval")
		}
	default:
	}
}

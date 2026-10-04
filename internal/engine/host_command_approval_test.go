package engine

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestHostCommandsRequireFreshApproval(t *testing.T) {
	for _, mode := range []store.ApprovalMode{store.ApprovalAsk, store.ApprovalAuto, store.ApprovalFull} {
		t.Run(string(mode), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			ms := storetest.New(t)
			root := t.TempDir()
			if err := ms.CreateProject(ctx, &store.Project{ID: "p", RootDirs: []string{root}, ApprovalMode: mode}); err != nil {
				t.Fatal(err)
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "s", ProjectID: "p", ActiveMode: store.ModeCode, Provider: "mock", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
			hub := event.NewHub()
			events, unsub := hub.Subscribe("s")
			defer unsub()
			calls := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: `{"ok":true}`}}
			eng := New(ms, hub, registry.Static(mock.New()), ms, WithTools(&approvalDetailsRecordingToolRunner{recordingToolRunner: calls}))
			t.Cleanup(eng.Stop)
			raw, _ := json.Marshal(map[string]any{"scope": "project", "command": "git status", "execution": "host", "host_access_reason": "Windows has no project sandbox", "cwd": root, "env": map[string]string{"TEST_VALUE": "unchanged"}})
			for i := 0; i < 3; i++ {
				deny := i == 2 && mode != store.ApprovalFull
				before := len(calls.calls)
				done := make(chan tool.Result, 1)
				go func() {
					done <- eng.executeAllowedTool(ctx, "s", fmt.Sprint("turn", i), store.ModeCode, tool.Call{SessionID: "s", CallID: fmt.Sprint("call", i), Name: tool.CommandRun, Args: raw})
				}()
				approvals := 0
			wait:
				for {
					select {
					case ev := <-events:
						if ev.Kind != event.ApprovalRequested {
							continue
						}
						approvals++
						if mode == store.ApprovalFull || approvals != 1 || strings.Contains(string(ev.Payload), "sessionGrant") || !strings.Contains(string(ev.Payload), "host_execution") {
							t.Fatalf("unexpected approval: %s", ev.Payload)
						}
						var err error
						if deny {
							err = eng.DenyApproval(ctx, "s", ev.ApprovalID, "test denial")
						} else {
							err = eng.ApproveApproval(ctx, "s", ev.ApprovalID, ApprovalScopeTurn, nil)
						}
						if err != nil {
							t.Fatal(err)
						}
					case result := <-done:
						if result.Ok == deny || (approvals == 1) != (mode != store.ApprovalFull) {
							t.Fatalf("approvals=%d result=%+v", approvals, result)
						}
						break wait
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				if deny {
					if len(calls.calls) != before {
						t.Fatal("denied command executed")
					}
				} else {
					call := calls.calls[len(calls.calls)-1]
					if string(call.Args) != string(raw) || call.CommandSandbox != tool.CommandSandboxBypass || call.CommandGrant != nil {
						t.Fatalf("approved call changed: %+v", call)
					}
				}
			}
		})
	}
}

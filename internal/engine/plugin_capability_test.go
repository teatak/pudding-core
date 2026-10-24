package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestPluginLoadResumesAfterCapabilityApproval(t *testing.T) {
	for _, tc := range []struct {
		scope ApprovalScope
		mode  store.AgentMode
	}{
		{ApprovalScopeTurn, store.ModeWork}, {ApprovalScopeSession, store.ModeWork},
		{ApprovalScopeTurn, store.ModeCode}, {ApprovalScopeSession, store.ModeCode},
	} {
		scope := tc.scope
		t.Run(string(tc.mode)+"/"+string(scope), func(t *testing.T) {
			ctx := context.Background()
			st := storetest.New(t)
			hub := event.NewHub()
			client := &pluginLoadClient{}
			runner := &recordingToolRunner{defs: tool.BuiltinDefinitions(), result: tool.Result{Ok: true, Content: `{"ok":true}`}}
			defs := plugin.BuiltinDefinitions()
			for _, def := range defs {
				if def.ID == plugin.BuiltinBrowserID {
					def.RequiredMode = string(tc.mode)
				}
			}
			eng := New(st, hub, mapResolver{"app-load": client}, st, WithTools(runner), WithAttachmentHome(t.TempDir()), WithPlugins(&mutablePluginSource{defs: defs}))
			t.Cleanup(eng.Stop)
			const sid = "auto-capability"
			if err := st.CreateSession(ctx, &store.Session{ID: sid, Title: "Plugin approval", Provider: "app-load", Model: "app-model"}); err != nil {
				t.Fatal(err)
			}
			if err := st.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "app-load", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "app-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 4}}}}); err != nil {
				t.Fatal(err)
			}
			sub, unsub := hub.Subscribe(sid)
			defer unsub()
			if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "first", Text: "Open the browser"}); err != nil {
				t.Fatal(err)
			}
			deadline := time.After(5 * time.Second)
			waiting := true
			for waiting {
				select {
				case ev := <-sub:
					if ev.Kind == event.TurnTool && ev.Phase == "error" {
						t.Fatalf("load must not fail before approval: %+v", ev)
					}
					if ev.Kind != event.ApprovalRequested {
						continue
					}
					if ev.CallID != "call_browser_load" || ev.ApprovalKind != ApprovalKindCapability || !strings.Contains(string(ev.Payload), `"targetMode":"`+string(tc.mode)+`"`) {
						t.Fatalf("wrong approval: %+v", ev)
					}
					sess, err := st.GetSession(ctx, sid)
					if err != nil {
						t.Fatal(err)
					}
					if len(sess.LoadedPluginIDs) != 0 {
						t.Fatal("loaded before approval")
					}
					if err := eng.ApproveApproval(ctx, sid, ev.ApprovalID, scope, nil); err != nil {
						t.Fatal(err)
					}
					waiting = false
				case <-deadline:
					t.Fatal("no capability approval")
				}
			}
			waitTurnDone(t, st, sid)
			if len(client.requests) != 3 || !hasToolDef(client.requests[1].Tools, tool.BrowserOpen) {
				t.Fatal("approved load did not expose tools on next model step")
			}
			if len(runner.calls) != 1 || runner.calls[0].Name != tool.BrowserOpen {
				t.Fatalf("plugin operation did not resume: %+v", runner.calls)
			}
			messages, err := st.ListMessages(ctx, sid, 0)
			if err != nil {
				t.Fatal(err)
			}
			loads := 0
			for _, message := range messages {
				for _, part := range message.Parts {
					if part.Type == store.ContentPartToolResult && part.Name == tool.PluginLoad {
						loads++
						if !part.Ok {
							t.Fatal(part.Content)
						}
					}
				}
			}
			if loads != 1 {
				t.Fatalf("load results=%d, want one successful call", loads)
			}
			sess, err := st.GetSession(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			wantMode := store.ModeChat
			if scope == ApprovalScopeSession {
				wantMode = tc.mode
			}
			if sess.ActiveMode != wantMode {
				t.Fatalf("persisted mode=%s, want %s", sess.ActiveMode, wantMode)
			}
		})
	}
}

func TestPluginLoadCapabilityDenialAndCancellation(t *testing.T) {
	for _, action := range []string{"deny", "cancel", "disable"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			st := storetest.New(t)
			hub := event.NewHub()
			plugins := plugin.NewService(t.TempDir(), st.Manager)
			eng := New(st, hub, nil, st, WithPlugins(plugins))
			t.Cleanup(eng.Stop)
			if err := st.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
			sub, unsub := hub.Subscribe("s")
			defer unsub()
			type response struct {
				result  tool.Result
				changed bool
				mode    store.AgentMode
			}
			done := make(chan response, 1)
			go func() {
				r, c, m := eng.loadPlugin(ctx, "s", tool.Call{Name: tool.PluginLoad, CallID: "load", TurnID: "turn", Args: json.RawMessage(`{"plugin_id":"browser"}`)}, store.ModeChat)
				done <- response{r, c, m}
			}()
			var ev event.Event
			select {
			case ev = <-sub:
			case <-time.After(time.Second):
				t.Fatal("no approval")
			}
			if ev.Kind != event.ApprovalRequested {
				t.Fatal(ev)
			}
			switch action {
			case "deny":
				if err := eng.DenyApproval(ctx, "s", ev.ApprovalID, "user_denied"); err != nil {
					t.Fatal(err)
				}
			case "cancel":
				cancel()
			case "disable":
				if _, err := plugins.SetEnabled(ctx, "browser", false); err != nil {
					t.Fatal(err)
				}
				if err := eng.ApproveApproval(ctx, "s", ev.ApprovalID, ApprovalScopeTurn, nil); err != nil {
					t.Fatal(err)
				}
			}
			select {
			case got := <-done:
				if got.result.Ok || got.changed || got.result.Name != tool.PluginLoad || got.result.CallID != "load" {
					t.Fatalf("unexpected load result: %+v", got)
				}
				want := "denied"
				wantMode := store.ModeChat
				if action == "cancel" {
					want = "cancelled"
				}
				if action == "disable" {
					want = "plugin_disabled"
					wantMode = store.ModeWork
				}
				if !strings.Contains(got.result.Content, want) || got.mode != wantMode {
					t.Fatal(got)
				}
			case <-time.After(time.Second):
				t.Fatal("load did not finish")
			}
			sess, err := st.GetSession(context.Background(), "s")
			if err != nil {
				t.Fatal(err)
			}
			if len(sess.LoadedPluginIDs) != 0 {
				t.Fatal("unsuccessful load changed session")
			}
			if len(eng.PendingApprovals("s")) != 0 {
				t.Fatal("approval leaked")
			}
		})
	}
}

package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/widget"
)

type widgetPatchClient struct {
	args    string
	streams int
}

func (c *widgetPatchClient) Name() string { return "widget-patch" }

func (c *widgetPatchClient) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	c.streams++
	out := make(chan provider.Chunk, 2)
	if c.streams == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "call_canvas_patch", Name: tool.FilePatch, ArgsDelta: c.args}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "画布源码已更新"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

func TestCodeSessionWidgetPatchRunsThroughEngineApproval(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ms := storetest.New(t)
	hub := event.NewHub()
	project := &store.Project{ID: "proj_canvas_patch", Name: "Widget patch", RootDirs: []string{t.TempDir()}, ApprovalMode: store.ApprovalAsk}
	if err := ms.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	const sessionID = "sess_canvas_patch"
	if err := ms.CreateSession(ctx, &store.Session{ID: sessionID, Title: "Widget patch", Provider: "widget-patch", Model: "widget-model", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession, ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	const widgetID = "canvas_engine_patch"
	if _, err := ms.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: widgetID, Name: "Widget", SourceSessionID: sessionID}); err != nil {
		t.Fatal(err)
	}
	draft, err := widget.StartDraft(home, widgetID, "")
	if err != nil {
		t.Fatal(err)
	}
	source := "export default function App(){return <p>Before</p>}\n"
	draft, err = widget.WriteDraftFile(home, widgetID, "src/App.tsx", &source, draft.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{
		"scope": "widget", "widget_id": widgetID, "expectedDraftHash": draft.DraftHash,
		"files": []any{map[string]any{"path": "src/App.tsx", "action": "edit", "hunks": []any{map[string]any{
			"start_line": 1,
			"old_lines":  []string{"export default function App(){return <p>Before</p>}"},
			"new_lines":  []string{"export default function App(){return <p>After</p>}"},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &widgetPatchClient{args: string(args)}
	eng := New(ms, hub, mapResolver{"widget-patch": client}, ms,
		WithAttachmentHome(home),
		WithTools(tool.NewBuiltinRunner(tool.WithHomeDir(home), tool.WithStudioItems(ms))),
		WithPlugins(&mutablePluginSource{defs: plugin.BuiltinDefinitions()}),
	)
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "widget-patch", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "widget-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 4}}}}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sessionID)
	defer unsub()
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sessionID, ClientMessageID: "widget-edit", Text: "编辑画布源码"}); err != nil {
		t.Fatal(err)
	}
	var approval event.Event
	deadline := time.After(2 * time.Second)
	for approval.Kind != event.ApprovalRequested {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approval = ev
			}
		case <-deadline:
			t.Fatal("widget patch approval request not emitted")
		}
	}
	if approval.CallID != "call_canvas_patch" || !strings.Contains(string(approval.Payload), `"scope":"widget"`) || !strings.Contains(string(approval.Payload), `-export default`) || strings.Contains(string(approval.Payload), `"projectRoot"`) {
		t.Fatalf("widget approval payload is incorrect: %+v", approval)
	}
	if current, err := widget.ReadDraft(home, widgetID); err != nil || current.DraftHash != draft.DraftHash {
		t.Fatalf("widget changed before approval: %+v %v", current, err)
	}
	if err := eng.ApproveApproval(ctx, sessionID, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sessionID)
	updated, err := widget.ReadDraft(home, widgetID)
	if err != nil || updated.Files["src/App.tsx"] != "export default function App(){return <p>After</p>}\n" || updated.DraftHash == draft.DraftHash || client.streams != 2 {
		t.Fatalf("engine did not apply the approved widget patch: draft=%+v streams=%d err=%v", updated, client.streams, err)
	}
}

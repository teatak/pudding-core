package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type canvasPatchClient struct {
	args    string
	streams int
}

func (c *canvasPatchClient) Name() string { return "canvas-patch" }

func (c *canvasPatchClient) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
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

func TestCodeSessionCanvasPatchRunsThroughEngineApproval(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	ms := storetest.New(t)
	hub := event.NewHub()
	project := &store.Project{ID: "proj_canvas_patch", Name: "Canvas patch", RootDirs: []string{t.TempDir()}, ApprovalMode: store.ApprovalAsk}
	if err := ms.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	const sessionID = "sess_canvas_patch"
	if err := ms.CreateSession(ctx, &store.Session{ID: sessionID, Title: "Canvas patch", Provider: "canvas-patch", Model: "canvas-model", ActiveMode: store.ModeCode, ModeLease: store.ModeLeaseSession, ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	const canvasID = "canvas_engine_patch"
	if _, err := ms.CreateCanvas(ctx, &store.Canvas{ID: canvasID, Name: "Canvas", SourceSessionID: sessionID}); err != nil {
		t.Fatal(err)
	}
	draft, err := canvas.StartDraft(home, canvasID, "")
	if err != nil {
		t.Fatal(err)
	}
	source := "export default function App(){return <p>Before</p>}\n"
	draft, err = canvas.WriteDraftFile(home, canvasID, "src/App.tsx", &source, draft.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	args, err := json.Marshal(map[string]any{
		"scope": "canvas", "canvas_id": canvasID, "expectedDraftHash": draft.DraftHash,
		"files": []any{map[string]any{"path": "src/App.tsx", "action": "edit", "hunks": []any{map[string]any{
			"start_line": 1,
			"old_lines":  []string{"export default function App(){return <p>Before</p>}"},
			"new_lines":  []string{"export default function App(){return <p>After</p>}"},
		}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &canvasPatchClient{args: string(args)}
	eng := New(ms, hub, mapResolver{"canvas-patch": client}, ms,
		WithAttachmentHome(home),
		WithTools(tool.NewBuiltinRunner(tool.WithHomeDir(home), tool.WithCanvasResources(ms))),
		WithApps(&mutableAppSource{defs: app.BuiltinDefinitions()}),
	)
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "canvas-patch", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "canvas-model", Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 4}}}}); err != nil {
		t.Fatal(err)
	}
	sub, unsub := hub.Subscribe(sessionID)
	defer unsub()
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sessionID, ClientMessageID: "canvas-edit", Text: "编辑画布源码"}); err != nil {
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
			t.Fatal("canvas patch approval request not emitted")
		}
	}
	if approval.CallID != "call_canvas_patch" || !strings.Contains(string(approval.Payload), `"scope":"canvas"`) || !strings.Contains(string(approval.Payload), `-export default`) || strings.Contains(string(approval.Payload), `"projectRoot"`) {
		t.Fatalf("canvas approval payload is incorrect: %+v", approval)
	}
	if current, err := canvas.ReadDraft(home, canvasID); err != nil || current.DraftHash != draft.DraftHash {
		t.Fatalf("canvas changed before approval: %+v %v", current, err)
	}
	if err := eng.ApproveApproval(ctx, sessionID, approval.ApprovalID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sessionID)
	updated, err := canvas.ReadDraft(home, canvasID)
	if err != nil || updated.Files["src/App.tsx"] != "export default function App(){return <p>After</p>}\n" || updated.DraftHash == draft.DraftHash || client.streams != 2 {
		t.Fatalf("engine did not apply the approved canvas patch: draft=%+v streams=%d err=%v", updated, client.streams, err)
	}
}

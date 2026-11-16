package engine

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func newDirectToolEngine(t *testing.T, opts ...Option) (*Engine, *storetest.Store) {
	t.Helper()
	db := storetest.New(t)
	if err := db.PutProviderProfile(context.Background(), &store.ProviderProfile{
		ID: "configured", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "configured-model"}},
	}); err != nil {
		t.Fatal(err)
	}
	// There is deliberately no model client: direct tools must not resolve one.
	eng := New(db, event.NewHub(), nil, db, append([]Option{WithAttachmentHome(t.TempDir())}, opts...)...)
	t.Cleanup(func() { eng.Stop(); eng.Wait() })
	return eng, db
}

type directToolRunner struct {
	defs []provider.ToolDef
	run  func(context.Context, tool.Call) tool.Result
}

func (r directToolRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return r.defs, nil
}
func (r directToolRunner) Call(ctx context.Context, call tool.Call) tool.Result {
	return r.run(ctx, call)
}

func TestDirectToolCallUsesCanonicalTurn(t *testing.T) {
	ctx := plugin.WithRuntimeID(context.Background(), "desktop-runtime")
	var called tool.Call
	eng, db := newDirectToolEngine(t)
	eng.tools = directToolRunner{
		defs: []provider.ToolDef{{Name: tool.TimeGetCurrent, Capability: store.ModeChat, InputSchema: json.RawMessage(`{"type":"object"}`)}},
		run: func(ctx context.Context, call tool.Call) tool.Result {
			called = call
			turn, err := db.GetConversationTurn(ctx, call.SessionID, call.TurnID)
			if err != nil || turn.Status != store.TurnRunning || plugin.RuntimeIDFromContext(ctx) != "desktop-runtime" {
				t.Fatalf("tool lost its canonical turn or runtime: turn=%+v, err=%v", turn, err)
			}
			return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: `{"time":"12:00"}`}
		},
	}
	session, err := eng.DebugSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	reused, err := eng.DebugSession(ctx)
	if err != nil || reused.ID != session.ID || reused.ActiveMode != store.ModeCode || reused.ModeLease != store.ModeLeaseSession || reused.Provider != "configured" {
		t.Fatalf("debug session not reused with configured Code capability: %+v, %v", reused, err)
	}
	defs, err := eng.ToolDefinitions(ctx, session.ID, store.ModeCode)
	if err != nil || !tool.HasDefinition(defs, tool.TimeGetCurrent) {
		t.Fatalf("core tool not listed: %+v, %v", defs, err)
	}
	result, err := eng.CallTool(ctx, session.ID, store.ModeCode, tool.TimeGetCurrent, json.RawMessage(`{}`))
	if err != nil || !result.Ok || called.Mode != store.ModeCode || called.CallID == "" {
		t.Fatalf("direct call failed: %+v, %v", result, err)
	}
	turn, err := db.GetConversationTurn(ctx, session.ID, called.TurnID)
	if err != nil || turn.Status != store.TurnCompleted {
		t.Fatalf("tool turn not completed: %+v, %v", turn, err)
	}
	var use, output bool
	for _, message := range turn.Messages {
		for _, part := range message.Parts {
			if part.CallID != called.CallID {
				continue
			}
			use = use || part.Type == store.ContentPartToolUse
			output = output || part.Type == store.ContentPartToolResult && part.Ok && part.Content == result.Content
		}
	}
	if !use || !output || eng.running[session.ID] != nil {
		t.Fatal("tool history or normal turn cleanup missing")
	}
}

type directPluginSource struct{}

func (directPluginSource) ListDefinitions(context.Context) ([]*plugin.Definition, error) {
	return []*plugin.Definition{{ID: "sample", Name: "Sample", Enabled: true, RequiredMode: "code"}}, nil
}
func (directPluginSource) ReadSkill(context.Context, string, string) (*plugin.SkillDetail, error) {
	return nil, errors.New("sample has no skills")
}

func TestDirectToolPluginLoadAndUnload(t *testing.T) {
	ctx := context.Background()
	eng, _ := newDirectToolEngine(t, WithPlugins(directPluginSource{}), WithTools(directToolRunner{
		defs: []provider.ToolDef{{Name: "sample_tool", PluginID: "sample", Capability: store.ModeCode, InputSchema: json.RawMessage(`{"type":"object"}`)}},
		run: func(_ context.Context, call tool.Call) tool.Result {
			return tool.Result{CallID: call.CallID, Name: call.Name, Ok: true, Content: "sample result"}
		},
	}))
	session, err := eng.DebugSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	prepared := map[string]bool{}
	if err := eng.PrepareDebugPlugins(ctx, session.ID, store.ModeCode, prepared); err != nil {
		t.Fatal(err)
	}
	result, err := eng.CallTool(ctx, session.ID, store.ModeCode, "sample_tool", json.RawMessage(`{}`))
	if err != nil || !result.Ok {
		t.Fatalf("prepared plugin unavailable: %+v, %v", result, err)
	}
	result, err = eng.CallTool(ctx, session.ID, store.ModeCode, tool.PluginUnload, json.RawMessage(`{"plugin_id":"sample"}`))
	if err != nil || !result.Ok {
		t.Fatalf("plugin unload failed: %+v, %v", result, err)
	}
	if err := eng.PrepareDebugPlugins(ctx, session.ID, store.ModeCode, prepared); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.ToolDefinitions(ctx, session.ID, store.ModeCode)
	if err != nil || tool.HasDefinition(defs, "sample_tool") {
		t.Fatalf("preparation undid explicit unload: %+v, %v", defs, err)
	}
	result, err = eng.CallTool(ctx, session.ID, store.ModeCode, tool.PluginLoad, json.RawMessage(`{"plugin_id":"sample"}`))
	if err != nil || !result.Ok {
		t.Fatalf("plugin reload failed: %+v, %v", result, err)
	}
	defs, err = eng.ToolDefinitions(ctx, session.ID, store.ModeCode)
	if err != nil || !tool.HasDefinition(defs, "sample_tool") {
		t.Fatalf("loaded plugin not listed: %+v, %v", defs, err)
	}
}

func TestDirectToolEngineDispatchAndSessionScope(t *testing.T) {
	ctx := context.Background()
	eng, db := newDirectToolEngine(t)
	session, err := eng.DebugSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range []struct{ name, args string }{
		{tool.RequestCapability, `{"targetMode":"code","reason":"debug"}`},
		{tool.ScheduledTask, `{"action":"list"}`},
	} {
		result, err := eng.CallTool(ctx, session.ID, store.ModeCode, call.name, json.RawMessage(call.args))
		if err != nil || !result.Ok {
			t.Fatalf("engine tool %s failed: %+v, %v", call.name, result, err)
		}
	}
	profile, err := db.OpenAssistant(ctx, "Assistant", "configured", "configured-model", store.AssistantAppearance{
		"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic",
		"headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush",
	})
	if err != nil {
		t.Fatal(err)
	}
	defs, err := eng.ToolDefinitions(ctx, session.ID, store.ModeCode)
	if err != nil || tool.HasDefinition(defs, tool.AssistantMemory) {
		t.Fatalf("ordinary session exposed assistant tools: %v", err)
	}
	assistantID := profile.Assistant.SessionID
	defs, err = eng.ToolDefinitions(ctx, assistantID, store.ModeChat)
	if err != nil || !tool.HasDefinition(defs, tool.AssistantMemory) {
		t.Fatalf("assistant session missing its tools: %v", err)
	}
	result, err := eng.CallTool(ctx, assistantID, store.ModeChat, tool.AssistantUserName, json.RawMessage(`{"name":"Developer"}`))
	if err != nil || !result.Ok || !strings.Contains(result.Content, "Developer") {
		t.Fatalf("assistant engine call failed: %+v, %v", result, err)
	}
}

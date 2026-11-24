package contextbuilder

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestCoolingNativeContinuationKeepsPairingAndUsesColdResultOnWire(t *testing.T) {
	for _, tc := range []struct {
		kind, native string
		wire         func(provider.Request) provider.WireInput
	}{
		{provider.ContinuationOpenAIChat, `{"role":"assistant","reasoning_content":"native-reasoning","tool_calls":[{"id":"call","type":"function","function":{"name":"builtin_command_run","arguments":"{}"}}]}`, openai.ChatRequestInput},
		{provider.ContinuationOpenAIResponses, `[{"type":"reasoning","id":"r","encrypted_content":"native-reasoning"},{"type":"function_call","id":"f","call_id":"call","name":"builtin_command_run","arguments":"{}"}]`, openai.ResponsesRequestInput},
		{provider.ContinuationAnthropic, `[{"type":"thinking","thinking":"native-reasoning","signature":"sig"},{"type":"tool_use","id":"call","name":"builtin_command_run","input":{}}]`, anthropic.RequestInput},
		{provider.ContinuationGoogle, `[{"text":"native-reasoning","thought":true},{"functionCall":{"id":"call","name":"builtin_command_run","args":{}},"thoughtSignature":"sig"}]`, google.RequestInput},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			ctx := context.Background()
			db := storetest.New(t)
			if err := db.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "m"}); err != nil {
				t.Fatal(err)
			}
			state := &store.ProviderState{Provider: "mock", Model: "m", Kind: tc.kind, Data: json.RawMessage(tc.native)}
			raw := strings.Repeat("ORIGINAL-EVIDENCE ", 300)
			messages := []*store.Message{
				{ID: "input", TurnID: "old", Role: store.RoleUser, Parts: store.TextPart("inspect")},
				{ID: "call", TurnID: "old", TurnIndex: 1, Role: store.RoleAssistant, ProviderState: state, Parts: []store.ContentPart{{Type: store.ContentPartToolUse, Name: tool.CommandRun, CallID: "call", Args: []byte(`{}`)}}},
				{ID: "result", TurnID: "old", TurnIndex: 2, Role: store.RoleTool, Parts: []store.ContentPart{{Type: store.ContentPartToolResult, Name: tool.CommandRun, CallID: "call", Ok: true, Content: raw}}},
				{ID: "current", TurnID: "current", Role: store.RoleUser, Parts: store.TextPart("continue")},
			}
			defs := []provider.ToolDef{{Name: tool.CommandRun}, {Name: tool.HistoryGetMessage}}
			build := func(defs []provider.ToolDef, cold store.ContextCooling) provider.Request {
				r, err := New(db, nil).BuildForProviderWithHistory(ctx, "s", "mock", "m", "code", defs, messages, provider.ModelConfig{}, cold)
				if err != nil {
					t.Fatal(err)
				}
				r.Tools = defs
				return r
			}
			cooled := build(defs, store.ContextCooling{ThroughTurnID: "old"})
			wire, err := json.Marshal(tc.wire(cooled).Messages)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(wire), "cold_only") || strings.Contains(string(wire), "ORIGINAL-EVIDENCE") || !strings.Contains(string(wire), "native-reasoning") {
				t.Fatalf("wrong wire projection: %s", wire)
			}
			if string(state.Data) != tc.native || messages[2].Parts[0].Content != raw {
				t.Fatal("mutated canonical history")
			}
			// Without the readback tool the original view must remain usable.
			withoutReader := build(defs[:1], store.ContextCooling{ThroughTurnID: "old"})
			wire, err = json.Marshal(tc.wire(withoutReader).Messages)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(wire), "cold_only") || !strings.Contains(string(wire), "ORIGINAL-EVIDENCE") {
				t.Fatal("cooled without readback")
			}
		})
	}
}

func TestExternalMCPTurnsCoolToolResultsAndImagesByDefault(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	home := t.TempDir()
	if err := db.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "m", ActiveMode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}
	service := attachment.NewService(home)
	extShot, err := service.StoreReader("s", "ext-shot.png", "image/png", bytes.NewReader([]byte("ext-png")))
	if err != nil {
		t.Fatal(err)
	}
	extShot.Origin = attachment.OriginTool
	// A one-pixel, 24-bit BMP.
	data, err := hex.DecodeString("424d3a0000000000000036000000280000000100000001000000010018000000000004000000000000000000000000000000000000000000ff00")
	if err != nil {
		t.Fatal(err)
	}
	bmp, err := service.StoreReader("s", "diagram.bmp", "image/bmp", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	bmp.Origin = attachment.OriginTool
	normShot, err := service.StoreReader("s", "norm-shot.png", "image/png", bytes.NewReader([]byte("norm-png")))
	if err != nil {
		t.Fatal(err)
	}
	normShot.Origin = attachment.OriginTool

	extLarge := strings.Repeat("EXTERNAL_LARGE_OUTPUT ", 120)
	normLarge := strings.Repeat("NORMAL_LARGE_OUTPUT ", 120)
	skillPayload := `{"ok":true,"reference":{"kind":"plugin_skill","pluginID":"browser","skillID":"browser"},"content":"SKILL_BODY"}`

	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s", TurnID: "ext1", UserMessageID: "u_ext",
		ClientMessageID: "external-mcp:agent_1:ext1", UserText: "Antigravity · Check UI",
		UserMetadata: json.RawMessage(`{"externalAgent":{"name":"Antigravity"}}`),
		Mode:         store.ModeCode,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{
		TurnID: "ext1",
		Parts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "c_big", Name: tool.FileRead, Args: json.RawMessage(`{}`)},
			{Type: store.ContentPartToolResult, CallID: "c_big", Name: tool.FileRead, Ok: true, Content: extLarge},
			{Type: store.ContentPartToolUse, CallID: "c_small", Name: tool.TimeGetCurrent, Args: json.RawMessage(`{}`)},
			{Type: store.ContentPartToolResult, CallID: "c_small", Name: tool.TimeGetCurrent, Ok: true, Content: `{"iso":"EXTERNAL_SMALL_OK"}`},
			{Type: store.ContentPartToolUse, CallID: "c_skill", Name: tool.PluginLoad, Args: json.RawMessage(`{"plugin_id":"browser"}`)},
			{Type: store.ContentPartToolResult, CallID: "c_skill", Name: tool.PluginLoad, Ok: true, Content: skillPayload},
			{Type: store.ContentPartToolUse, CallID: "c_shot", Name: tool.BrowserScreenshot, Args: json.RawMessage(`{}`)},
			{Type: store.ContentPartToolResult, CallID: "c_shot", Name: tool.BrowserScreenshot, Ok: true, Content: `{"ok":true}`},
			store.AttachmentPart(extShot),
			store.AttachmentPart(bmp),
			{Type: store.ContentPartText, Text: "EXTERNAL_FINAL_SUMMARY"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "ext1", Status: store.TurnCompleted, Mode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}

	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s", TurnID: "norm1", UserMessageID: "u_norm",
		ClientMessageID: "user-norm", UserText: "Regular follow-up",
		Mode: store.ModeCode,
	}); err != nil {
		t.Fatal(err)
	}
	normState := &store.ProviderState{
		Provider: "mock",
		Model:    "m",
		Kind:     provider.ContinuationAnthropic,
		Data:     json.RawMessage(`[{"type":"thinking","thinking":"KEEP_ANTHROPIC_THINKING","signature":"sig_keep"},{"type":"tool_use","id":"n_big","name":"builtin_file_read","input":{}},{"type":"tool_use","id":"n_shot","name":"builtin_browser_screenshot","input":{}}]`),
	}
	if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{
		TurnID:        "norm1",
		ProviderState: normState,
		Parts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "n_big", Name: tool.FileRead, Args: json.RawMessage(`{}`)},
			{Type: store.ContentPartToolUse, CallID: "n_shot", Name: tool.BrowserScreenshot, Args: json.RawMessage(`{}`)},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{
		TurnID: "norm1",
		Parts: []store.ContentPart{
			{Type: store.ContentPartToolResult, CallID: "n_big", Name: tool.FileRead, Ok: true, Content: normLarge},
			{Type: store.ContentPartToolResult, CallID: "n_shot", Name: tool.BrowserScreenshot, Ok: true, Content: `{"ok":true}`},
			store.AttachmentPart(normShot),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "norm1", Status: store.TurnCompleted, Mode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}

	b := New(db, nil, WithAttachmentHome(home))
	cfg := provider.ModelConfig{Capabilities: &provider.ModelCapabilities{Image: true, Tools: true}}
	defs := []provider.ToolDef{
		{Name: tool.FileRead, Capability: store.ModeCode},
		{Name: tool.TimeGetCurrent, Capability: store.ModeChat},
		{Name: tool.PluginLoad, Capability: store.ModeChat},
		{Name: tool.BrowserScreenshot, Capability: store.ModeWork},
		{Name: tool.HistoryGetMessage, Capability: store.ModeChat},
		{Name: tool.MediaRead, Capability: store.ModeChat},
	}
	req, err := b.BuildForProviderWithTools(ctx, "s", "mock", "m", string(store.ModeCode), defs, cfg)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(req.Messages)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(raw)
	if strings.Contains(wire, "EXTERNAL_LARGE_OUTPUT") {
		t.Fatalf("external large output leaked into default model projection: %s", wire)
	}
	var cBigCold bool
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Type == provider.PartToolResult && p.CallID == "c_big" {
				var stub struct {
					ColdOnly  bool `json:"cold_only"`
					ResultRef struct {
						TurnID string `json:"turn_id"`
						CallID string `json:"call_id"`
					} `json:"result_ref"`
				}
				if err := json.Unmarshal([]byte(p.Content), &stub); err != nil {
					t.Fatalf("unmarshal cold stub: %v (%s)", err, p.Content)
				}
				cBigCold = stub.ColdOnly && stub.ResultRef.TurnID == "ext1" && stub.ResultRef.CallID == "c_big"
			}
		}
	}
	if !cBigCold {
		t.Fatalf("expected cold_only reference for external tool result: %s", wire)
	}
	if !strings.Contains(wire, "EXTERNAL_SMALL_OK") || !strings.Contains(wire, "EXTERNAL_FINAL_SUMMARY") || !strings.Contains(wire, "plugin_skill") {
		t.Fatalf("expected external summary, small output, and skill ref preserved: %s", wire)
	}
	if !strings.Contains(wire, "NORMAL_LARGE_OUTPUT") {
		t.Fatalf("normal turn large result should remain hot: %s", wire)
	}

	var extImages, normImages, bmpImages int
	var placeholderFound bool
	for _, m := range req.Messages {
		for _, p := range m.Parts {
			if p.Type == provider.PartImage && p.ImageKey == extShot.AttachmentKey {
				extImages++
			}
			if p.Type == provider.PartImage && p.ImageKey == bmp.AttachmentKey {
				bmpImages++
			}
			if p.Type == provider.PartImage && p.ImageKey == normShot.AttachmentKey {
				normImages++
			}
			if p.Type == provider.PartText && strings.Contains(p.Text, extShot.AttachmentKey) && strings.Contains(p.Text, tool.MediaRead) {
				placeholderFound = true
			}
		}
	}
	if bmpImages != 1 {
		t.Fatal("BMP must retain its normal attachment projection because media_read cannot reload it")
	}
	if extImages != 0 || normImages != 1 || !placeholderFound {
		t.Fatalf("unexpected image cooling: extImages=%d normImages=%d placeholder=%v", extImages, normImages, placeholderFound)
	}
	anthWire, err := json.Marshal(anthropic.RequestInput(req).Messages)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(anthWire), "KEEP_ANTHROPIC_THINKING") {
		t.Fatalf("external cold image stripped subsequent Anthropic thinking: %s", anthWire)
	}

	stored, err := db.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	var canonicalFound bool
	for _, m := range stored {
		for _, p := range m.Parts {
			if p.CallID == "c_big" && p.Type == store.ContentPartToolResult && p.Content == extLarge {
				canonicalFound = true
			}
		}
	}
	if !canonicalFound {
		t.Fatal("canonical history was mutated")
	}

	// Without HistoryGetMessage, large external text results stay readable instead of turning cold_only.
	withoutReader := []provider.ToolDef{defs[0], defs[1], defs[2], defs[3], defs[5]}
	uncooledReq, err := b.BuildForProviderWithTools(ctx, "s", "mock", "m", string(store.ModeCode), withoutReader, cfg)
	if err != nil {
		t.Fatal(err)
	}
	uncooledWire, _ := json.Marshal(uncooledReq.Messages)
	if strings.Contains(string(uncooledWire), "cold_only") || !strings.Contains(string(uncooledWire), "EXTERNAL_LARGE_OUTPUT") {
		t.Fatal("external text result cooled without HistoryGetMessage")
	}

	// Without MediaRead, external images remain inline instead of being replaced with placeholders.
	withoutMediaRead := []provider.ToolDef{defs[0], defs[1], defs[2], defs[3], defs[4]}
	noMediaReq, err := b.BuildForProviderWithTools(ctx, "s", "mock", "m", string(store.ModeCode), withoutMediaRead, cfg)
	if err != nil {
		t.Fatal(err)
	}
	var noMediaExtImages int
	for _, m := range noMediaReq.Messages {
		for _, p := range m.Parts {
			if p.Type == provider.PartImage && p.ImageKey == extShot.AttachmentKey {
				noMediaExtImages++
			}
		}
	}
	if noMediaExtImages != 1 {
		t.Fatalf("external image cooled without MediaRead: %d", noMediaExtImages)
	}
}

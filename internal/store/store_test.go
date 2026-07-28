package store

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestToolResultPartMarshalsFalseOK(t *testing.T) {
	data, err := json.Marshal(ContentPart{
		Type:         ContentPartToolResult,
		CallID:       "call_1",
		Name:         "builtin_web_search",
		Ok:           false,
		Content:      `{"ok":false}`,
		SummaryKind:  "returned_items",
		SummaryCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"ok":false`) {
		t.Fatalf("tool_result must preserve ok=false, got %s", data)
	}
	if !strings.Contains(string(data), `"summaryKind":"returned_items"`) || !strings.Contains(string(data), `"summaryCount":0`) {
		t.Fatalf("tool_result must preserve protocol summary, got %s", data)
	}
}

func TestNonToolResultPartOmitsOK(t *testing.T) {
	data, err := json.Marshal(ContentPart{
		Type: ContentPartToolUse,
		Name: "builtin_web_search",
		Args: json.RawMessage(`{"query":"x"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `"ok":`) {
		t.Fatalf("non tool_result should omit ok, got %s", data)
	}
}

func TestUserInputPartsPreserveUIContext(t *testing.T) {
	parts := UserInputParts("检查这个", []ContentPart{
		{
			Type:         ContentPartUIContext,
			Surface:      "canvas",
			Resource:     "canvas_item",
			CallID:       "item_1",
			Name:         "2026 World Cup",
			ResourceKind: "grid",
		},
	})
	if len(parts) != 2 || parts[0].Type != ContentPartUIContext || parts[1].Type != ContentPartText {
		t.Fatalf("unexpected user parts: %+v", parts)
	}
	if parts[0].Surface != "canvas" || parts[0].CallID != "item_1" || parts[0].Name != "2026 World Cup" {
		t.Fatalf("ui context not preserved: %+v", parts[0])
	}
	data, err := json.Marshal(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"surface":"canvas"`) || !strings.Contains(string(data), `"kind":"grid"`) {
		t.Fatalf("ui context json missing fields: %s", data)
	}
}

func TestUserInputPartsPreserveProjectReference(t *testing.T) {
	parts := UserInputParts("检查这个文件", []ContentPart{
		ProjectReferencePart(ProjectReference{
			ID:          "ref_1",
			Name:        "README.md",
			Path:        "tutorials/README.md",
			SourcePath:  "/workspace/tutorials/README.md",
			RootID:      "root_1",
			Kind:        "file",
			StartLine:   3,
			StartColumn: 2,
			EndLine:     5,
			EndColumn:   8,
		}),
	})
	if len(parts) != 2 || parts[0].Type != ContentPartProjectRef || parts[1].Type != ContentPartText {
		t.Fatalf("unexpected user parts: %+v", parts)
	}
	references := ProjectReferencesFromParts(parts)
	if len(references) != 1 || references[0].RootID != "root_1" || references[0].Path != "tutorials/README.md" || references[0].Kind != "file" || references[0].StartLine != 3 || references[0].EndLine != 5 {
		t.Fatalf("project reference not preserved: %+v", references)
	}
}

func TestUserInputPartsPreserveFormResult(t *testing.T) {
	parts := UserInputParts("已填写数据库连接信息", []ContentPart{
		{
			Type:   ContentPartFormResult,
			Title:  "数据库连接信息",
			Schema: json.RawMessage(`{"type":"form","steps":[{"id":"host","type":"text_input","title":"主机"}]}`),
			Result: json.RawMessage(`{"host":"127.0.0.1"}`),
		},
	})
	if len(parts) != 2 || parts[0].Type != ContentPartFormResult || parts[1].Type != ContentPartText {
		t.Fatalf("unexpected user parts: %+v", parts)
	}
	data, err := json.Marshal(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"title":"数据库连接信息"`) || !strings.Contains(string(data), `"result":{"host":"127.0.0.1"}`) {
		t.Fatalf("form result json missing fields: %s", data)
	}
}

func TestNormalizeContentPartsRejectsInvalidFormResult(t *testing.T) {
	parts := NormalizeContentParts([]ContentPart{{
		Type:   ContentPartFormResult,
		Title:  "数据库连接信息",
		Schema: json.RawMessage(`{"type":"form"}`),
		Result: json.RawMessage(`null`),
	}})
	if len(parts) != 0 {
		t.Fatalf("invalid form result should be dropped: %+v", parts)
	}
}

func TestNormalizeContentPartsRejectsInvalidProjectReference(t *testing.T) {
	parts := NormalizeContentParts([]ContentPart{{
		Type:         ContentPartProjectRef,
		CallID:       "ref_1",
		Name:         "README.md",
		Path:         "README.md",
		SourcePath:   "/workspace/README.md",
		RootID:       "root_1",
		ResourceKind: "other",
	}})
	if len(parts) != 0 {
		t.Fatalf("invalid project reference should be dropped: %+v", parts)
	}
}

func TestNormalizeContentPartsRejectsUnknownUIContextSurface(t *testing.T) {
	parts := NormalizeContentParts([]ContentPart{{Type: ContentPartUIContext, Surface: "unknown"}})
	if len(parts) != 0 {
		t.Fatalf("unknown ui context surface should be dropped: %+v", parts)
	}
}

func TestNormalizeContentPartsCapsUIContextSelection(t *testing.T) {
	parts := NormalizeContentParts([]ContentPart{{
		Type:          ContentPartUIContext,
		Surface:       "browser",
		Resource:      "browser_tab",
		SelectionText: "  " + strings.Repeat("选", 16*1024+1) + "  ",
	}})
	if len(parts) != 1 || len([]rune(parts[0].SelectionText)) != 16*1024 {
		t.Fatalf("browser selection should be trimmed and capped: %+v", parts)
	}
}

func TestContentPartMarshalJSONPreservesUIContextSelection(t *testing.T) {
	part := ContentPart{
		Type:          ContentPartUIContext,
		Surface:       "browser",
		Resource:      "browser_tab",
		CallID:        "tab_1",
		Name:          "Example",
		URL:           "https://example.com/",
		SelectionText: "selected browser text",
		ResourceKind:  "webview",
	}
	data, err := json.Marshal(part)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded["selectionText"]; got != part.SelectionText {
		t.Fatalf("selectionText = %v, want %q; json = %s", got, part.SelectionText, data)
	}
}

func TestProviderStateMetadataRoundTripPreservesPublicShape(t *testing.T) {
	state := &ProviderState{
		Provider: "openai",
		Model:    "gpt-test",
		Kind:     "openai_responses",
		Data:     json.RawMessage(`[{"type":"reasoning","encrypted_content":"cipher"}]`),
	}
	tests := []struct {
		name     string
		metadata json.RawMessage
		want     string
		wantNil  bool
	}{
		{name: "empty", wantNil: true},
		{name: "object", metadata: json.RawMessage(`{"source":"compact"}`), want: `{"source":"compact"}`},
		{name: "array", metadata: json.RawMessage(`["one",2]`), want: `["one",2]`},
		{name: "scalar", metadata: json.RawMessage(`"opaque"`), want: `"opaque"`},
		{name: "null", metadata: json.RawMessage(`null`), want: `null`},
		{name: "reserved key", metadata: json.RawMessage(`{"_provider_state":"public"}`), want: `{"_provider_state":"public"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored := EncodeMessageMetadataForStorage(tt.metadata, state)
			if strings.Contains(string(stored), "cipher") && !strings.Contains(string(stored), providerStateMetadataKey) {
				t.Fatalf("provider state was not stored in the hidden field: %s", stored)
			}
			got, decodedState := DecodeMessageMetadataFromStorage(stored)
			if decodedState == nil || string(decodedState.Data) != string(state.Data) {
				t.Fatalf("provider state changed: %+v", decodedState)
			}
			if tt.wantNil {
				if len(got) != 0 {
					t.Fatalf("metadata = %s, want nil", got)
				}
				return
			}
			if string(got) != tt.want {
				t.Fatalf("metadata = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestDecodeMetadataWithoutProviderStateKeepsPublicMetadata(t *testing.T) {
	for _, metadata := range []json.RawMessage{
		json.RawMessage(`{"source":"user"}`),
		json.RawMessage(`["one",2]`),
		json.RawMessage(`"_provider_state"`),
	} {
		got, state := DecodeMessageMetadataFromStorage(metadata)
		if state != nil || string(got) != string(metadata) {
			t.Fatalf("metadata changed: got=%s state=%+v want=%s", got, state, metadata)
		}
	}
	if got, state := DecodeMessageMetadataFromStorage(json.RawMessage(`{}`)); state != nil || len(got) != 0 {
		t.Fatalf("empty object should remain omitted: got=%s state=%+v", got, state)
	}
}

func TestEnsureProviderStateAssistantSegmentAddsHiddenAnchorAfterToolResult(t *testing.T) {
	state := &ProviderState{
		Provider: "openai",
		Model:    "gpt-test",
		Kind:     "openai_responses",
		Data:     json.RawMessage(`[{"type":"reasoning","encrypted_content":"cipher"}]`),
	}
	segments := []AssistantOutputSegment{{
		Role: RoleTool,
		Kind: MessageKindToolResult,
	}}
	got := EnsureProviderStateAssistantSegment(segments, state)
	if len(got) != 2 || got[0].Role != RoleTool || got[1].Role != RoleAssistant {
		t.Fatalf("segments = %+v, want tool result followed by assistant anchor", got)
	}
	message := &Message{
		Role:          got[1].Role,
		Kind:          got[1].Kind,
		Text:          got[1].Text,
		Parts:         got[1].Parts,
		ProviderState: state,
	}
	if !IsProtocolOnlyMessage(message) {
		t.Fatalf("assistant anchor should be protocol-only: %+v", message)
	}
	if IsProtocolOnlyMessage(&Message{Role: RoleAssistant, Parts: TextPart("visible"), ProviderState: state}) {
		t.Fatal("visible assistant message must not be protocol-only")
	}
}

func TestNormalizeAgentModeRejectsLegacyWorkspace(t *testing.T) {
	if mode := NormalizeAgentMode(AgentMode("workspace")); mode != "" {
		t.Fatalf("legacy workspace mode must be rejected, got %q", mode)
	}
	if mode := NormalizeAgentMode(AgentMode("project")); mode != "" {
		t.Fatalf("legacy project mode must be rejected, got %q", mode)
	}
	if mode := NormalizeAgentMode(ModeWork); mode != ModeWork {
		t.Fatalf("work mode must remain valid, got %q", mode)
	}
	if mode := NormalizeAgentMode(ModeCode); mode != ModeCode {
		t.Fatalf("code mode must remain valid, got %q", mode)
	}
	if AgentModeRank(ModeChat) >= AgentModeRank(ModeWork) || AgentModeRank(ModeWork) >= AgentModeRank(ModeCode) {
		t.Fatalf("agent mode ordering must be chat < work < code")
	}
}

func TestSessionLoadedAppsAreNormalizedAndReadable(t *testing.T) {
	sess := &Session{
		Provider:     "mock",
		Model:        "mock",
		LoadedAppIDs: []string{" terminal ", "browser", "browser", ""},
	}
	if err := NormalizeSessionProviderModel(sess); err != nil {
		t.Fatal(err)
	}
	if len(sess.LoadedAppIDs) != 2 || sess.LoadedAppIDs[0] != "browser" || sess.LoadedAppIDs[1] != "terminal" {
		t.Fatalf("loaded app ids = %+v", sess.LoadedAppIDs)
	}
	data, err := json.Marshal(sess)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"loadedAppIDs":["browser","terminal"]`) {
		t.Fatalf("loaded app ids must be readable: %s", data)
	}
}

func TestNextUsageCalibrationRatioUsesBoundedEWMA(t *testing.T) {
	first := NextUsageCalibrationRatio(0, 0, 100, 130)
	if math.Abs(first-1.3) > 0.0001 {
		t.Fatalf("first ratio = %f want 1.3", first)
	}
	second := NextUsageCalibrationRatio(first, 1, 100, 110)
	if math.Abs(second-1.25) > 0.0001 {
		t.Fatalf("second ratio = %f want 1.25", second)
	}
	if got := NextUsageCalibrationRatio(second, 2, 100, 1000); got > 1.44 {
		t.Fatalf("outlier was not bounded: %f", got)
	}
}

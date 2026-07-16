package store

import (
	"encoding/json"
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

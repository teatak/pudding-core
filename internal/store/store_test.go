package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestNormalizeProjectAllowsNamedProjectWithoutDirectories(t *testing.T) {
	project := &Project{ID: "project_empty", Name: "  Empty project  "}
	if err := NormalizeProject(project); err != nil {
		t.Fatal(err)
	}
	if project.Name != "Empty project" || len(project.RootDirs) != 0 {
		t.Fatalf("unexpected normalized project: %+v", project)
	}
}

func TestBrowserHistoryMatchesVisibleURLInsteadOfScheme(t *testing.T) {
	entry := &BrowserHistoryEntry{URL: "https://www.google.com/", Title: "Google"}
	if BrowserHistoryMatches(entry, "s") {
		t.Fatal("scheme must not participate in browser history search")
	}
	if !BrowserHistoryMatches(entry, "google") {
		t.Fatal("visible host should participate in browser history search")
	}
	if !BrowserHistoryMatches(entry, "https://www.google.com/") {
		t.Fatal("a pasted full URL should match its visible URL")
	}
	entry.URL = "https://www.google.com/search?q=pudding"
	if !BrowserHistoryMatches(entry, "search") {
		t.Fatal("visible path should participate in browser history search")
	}
}

func TestNormalizeProjectRejectsUnnamedProjectWithoutDirectories(t *testing.T) {
	project := &Project{ID: "project_empty"}
	if err := NormalizeProject(project); !errors.Is(err, ErrInvalidProject) {
		t.Fatalf("expected ErrInvalidProject, got %v", err)
	}
}

func TestNormalizeProjectUpdateAllowsClearingDirectories(t *testing.T) {
	dirs := []string{}
	update := ProjectUpdate{RootDirs: &dirs}
	if err := NormalizeProjectUpdate(&update); err != nil {
		t.Fatal(err)
	}
	if update.RootDirs == nil || len(*update.RootDirs) != 0 {
		t.Fatalf("unexpected normalized update: %+v", update)
	}
}

func TestToolResultPartMarshalsFalseOK(t *testing.T) {
	displayAttachment := Attachment{
		ID:            "att_photo",
		Name:          "photo.jpg",
		AttachmentKey: "sessions/sess_1/blobs/photo.jpg",
		URL:           "/sessions/sess_1/attachments/blobs/photo.jpg",
		MIME:          "image/jpeg",
		Size:          4,
		Origin:        "tool",
	}
	data, err := json.Marshal(ContentPart{
		Type:         ContentPartToolResult,
		CallID:       "call_1",
		Name:         "builtin_web_search",
		Ok:           false,
		Content:      `{"ok":false}`,
		SummaryKind:  "returned_items",
		SummaryCount: 0,
		Attachments:  []Attachment{displayAttachment},
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
	if !strings.Contains(string(data), `"attachments":[`) || !strings.Contains(string(data), displayAttachment.AttachmentKey) {
		t.Fatalf("tool_result must preserve display attachments, got %s", data)
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

func TestAttachmentsFromPartsDeduplicatesToolDisplayAndContextAttachment(t *testing.T) {
	item := Attachment{
		ID:            "att_photo",
		Name:          "photo.jpg",
		AttachmentKey: "sessions/sess_1/blobs/photo.jpg",
		URL:           "/sessions/sess_1/attachments/blobs/photo.jpg",
		MIME:          "image/jpeg",
		Size:          4,
		Origin:        "tool",
	}
	parts := []ContentPart{
		{Type: ContentPartToolResult, CallID: "call_1", Attachments: []Attachment{item}},
		AttachmentPart(item),
	}
	got := AttachmentsFromParts(parts)
	if len(got) != 1 || got[0].AttachmentKey != item.AttachmentKey {
		t.Fatalf("unexpected attachments: %+v", got)
	}
}

func TestCloneHelpersRewriteAttachmentsAndCompactMessageIDs(t *testing.T) {
	source := Attachment{
		ID:            "att_1",
		Name:          "photo.jpg",
		AttachmentKey: "sessions/source/blobs/photo.jpg",
		URL:           "/sessions/source/attachments/blobs/photo.jpg",
		MIME:          "image/jpeg",
		Size:          4,
	}
	target := source
	target.AttachmentKey = "sessions/target/blobs/photo.jpg"
	target.URL = "/sessions/target/attachments/blobs/photo.jpg"
	parts := ReplaceContentPartAttachments([]ContentPart{
		AttachmentPart(source),
		{Type: ContentPartToolResult, CallID: "call_1", Content: `{"attachmentKey":"sessions/source/blobs/photo.jpg","url":"/sessions/source/attachments/blobs/photo.jpg"}`, Attachments: []Attachment{source}},
	}, map[string]Attachment{source.AttachmentKey: target})
	attachments := AttachmentsFromParts(parts)
	if len(attachments) != 1 || attachments[0].AttachmentKey != target.AttachmentKey || attachments[0].URL != target.URL {
		t.Fatalf("attachments were not rewritten: %+v", attachments)
	}
	if !strings.Contains(parts[1].Content, target.AttachmentKey) || !strings.Contains(parts[1].Content, target.URL) {
		t.Fatalf("tool result references were not rewritten: %s", parts[1].Content)
	}

	metadata := CompactMessageMetadataWithCounts([]string{"old_1"}, []string{"old_2"}, 1, 1)
	metadata = RemapCompactMessageMetadata(metadata, map[string]string{"old_1": "new_1", "old_2": "new_2"})
	compact, ok := CompactMetadataFromMessage(&Message{Metadata: metadata})
	if !ok || len(compact.SourceMessageIDs) != 1 || compact.SourceMessageIDs[0] != "new_1" || len(compact.TailMessageIDs) != 1 || compact.TailMessageIDs[0] != "new_2" {
		t.Fatalf("compact metadata was not remapped: %+v", compact)
	}
}

func TestMessagesThroughBoundaryIncludesOnlyTrailingProtocolState(t *testing.T) {
	state := &ProviderState{Provider: "mock", Model: "model", Kind: "native", Data: json.RawMessage(`{"state":1}`)}
	messages := []*Message{
		{ID: "visible", TurnID: "turn_1", Role: RoleTool, Parts: []ContentPart{{Type: ContentPartToolResult, CallID: "call_1"}}},
		{ID: "protocol", TurnID: "turn_1", Role: RoleAssistant, ProviderState: state},
		{ID: "later", TurnID: "turn_1", Role: RoleAssistant, Parts: TextPart("later")},
	}
	prefix, ok := MessagesThroughBoundary(messages, "visible")
	if !ok || len(prefix) != 2 || prefix[1].ID != "protocol" {
		t.Fatalf("unexpected clone boundary: %+v", prefix)
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

func TestMarkUnsafeTurnFileChangeLayouts(t *testing.T) {
	changes := []*TurnFileChange{
		{RootPath: "/project", Path: "entry", Reversible: true},
		{RootPath: "/project", Path: "entry/child.txt", Reversible: true},
	}
	MarkUnsafeTurnFileChangeLayouts(changes)
	if changes[0].Reversible || changes[1].Reversible {
		t.Fatalf("ancestor file layout remained reversible: %+v", changes)
	}

	independent := []*TurnFileChange{
		{RootPath: "/project", Path: "one.txt", Reversible: true},
		{RootPath: "/project", Path: "folder/two.txt", Reversible: true},
	}
	MarkUnsafeTurnFileChangeLayouts(independent)
	if !independent[0].Reversible || !independent[1].Reversible {
		t.Fatalf("independent files became irreversible: %+v", independent)
	}
}

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

func TestSessionLoadedAppsAreNormalizedAndInternal(t *testing.T) {
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
	if strings.Contains(string(data), "loadedApp") {
		t.Fatalf("loaded app ids must remain internal: %s", data)
	}
}

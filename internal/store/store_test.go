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
	if mode := NormalizeAgentMode(ModeProject); mode != ModeProject {
		t.Fatalf("project mode must remain valid, got %q", mode)
	}
}

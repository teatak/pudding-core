package tool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TestToolDefAllowedForModeUsesCumulativeCapabilityLevels(t *testing.T) {
	defs := []provider.ToolDef{
		{Name: "chat_tool", Capability: store.ModeChat},
		{Name: "work_tool", Capability: store.ModeWork},
		{Name: "code_tool", Capability: store.ModeCode},
	}
	for modeIndex, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
		for definitionIndex, def := range defs {
			want := definitionIndex <= modeIndex
			if got := ToolDefAllowedForMode(mode, def); got != want {
				t.Fatalf("mode %q allows %q = %v, want %v", mode, def.Name, got, want)
			}
		}
	}
}

func TestRequestCapabilitySchemaOnlyExposesWorkAndCode(t *testing.T) {
	definition := RequestCapabilityDefinition()
	if !strings.Contains(definition.Description, "allowed as a no-op") || !strings.Contains(definition.Description, "already_available") {
		t.Fatalf("definition must explain Code-to-Work no-op behavior: %q", definition.Description)
	}
	var schema struct {
		Properties map[string]struct {
			Enum        []string `json:"enum"`
			Description string   `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	got := schema.Properties["targetMode"].Enum
	if len(got) != 2 || got[0] != "work" || got[1] != "code" {
		t.Fatalf("targetMode enum = %+v", got)
	}
	if !strings.Contains(schema.Properties["targetMode"].Description, "Code already includes Work") {
		t.Fatalf("targetMode description must explain cumulative capabilities: %q", schema.Properties["targetMode"].Description)
	}
	if !strings.Contains(schema.Properties["projectDirs"].Description, "session-isolated temporary workspace") || !strings.Contains(schema.Properties["projectDirs"].Description, "do not call with an empty list") {
		t.Fatalf("projectDirs description must explain scratch access and discourage redundant Code requests: %q", schema.Properties["projectDirs"].Description)
	}
}

func TestRequiredModeForDynamicMCPTools(t *testing.T) {
	tests := []struct {
		name string
		want store.AgentMode
	}{
		{name: "app_mcp__github__list_issues", want: store.ModeWork},
		{name: AppLoad, want: store.ModeChat},
		{name: AppUnload, want: store.ModeChat},
		{name: "canvas_create", want: store.ModeChat},
		{name: RequestUserInput, want: store.ModeChat},
		{name: "unknown_dynamic_tool", want: store.ModeCode},
	}
	for _, test := range tests {
		if got := RequiredModeForName(test.name); got != test.want {
			t.Fatalf("RequiredModeForName(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

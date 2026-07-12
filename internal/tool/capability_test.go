package tool

import (
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TestDefinitionsForTurnUsesCumulativeCapabilityLevels(t *testing.T) {
	defs := []provider.ToolDef{
		{Name: "chat_tool", Capability: store.ModeChat},
		{Name: "work_tool", Capability: store.ModeWork},
		{Name: "code_tool", Capability: store.ModeCode},
	}
	tests := []struct {
		mode store.AgentMode
		want []string
	}{
		{mode: store.ModeChat, want: []string{RequestCapability, ToolkitLoad, "chat_tool"}},
		{mode: store.ModeWork, want: []string{RequestCapability, ToolkitLoad, "chat_tool", "work_tool"}},
		{mode: store.ModeCode, want: []string{RequestCapability, ToolkitLoad, "chat_tool", "code_tool", "work_tool"}},
	}
	active := map[string]bool{"external.chat": true, "external.work": true, "external.code": true}
	for _, test := range tests {
		got := DefinitionsForTurn(test.mode, defs, active)
		if len(got) != len(test.want) {
			t.Fatalf("mode %q definitions = %+v, want %+v", test.mode, got, test.want)
		}
		for index, want := range test.want {
			if got[index].Name != want {
				t.Fatalf("mode %q definition %d = %q, want %q", test.mode, index, got[index].Name, want)
			}
		}
	}
}

func TestRequestCapabilitySchemaOnlyExposesWorkAndCode(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(RequestCapabilityDefinition().InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	got := schema.Properties["targetMode"].Enum
	if len(got) != 2 || got[0] != "work" || got[1] != "code" {
		t.Fatalf("targetMode enum = %+v", got)
	}
}

func TestRequiredModeForDynamicMCPTools(t *testing.T) {
	tests := []struct {
		name string
		want store.AgentMode
	}{
		{name: "app_mcp__github__list_issues", want: store.ModeWork},
		{name: "canvas_create", want: store.ModeChat},
		{name: "collect_user_input", want: store.ModeChat},
		{name: "unknown_dynamic_tool", want: store.ModeCode},
	}
	for _, test := range tests {
		if got := RequiredModeForName(test.name); got != test.want {
			t.Fatalf("RequiredModeForName(%q) = %q, want %q", test.name, got, test.want)
		}
	}
}

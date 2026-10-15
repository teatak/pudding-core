package tool

import (
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestSubagentToolsHaveOneCanonicalInventory(t *testing.T) {
	definitions := SubtaskDefinitions()
	if len(definitions) != 8 {
		t.Fatalf("unexpected subagent tool count: %d", len(definitions))
	}
	plug, ok := plugin.BuiltinDefinition(plugin.BuiltinCollaborationID)
	if !ok || len(plug.Tools) != len(definitions) {
		t.Fatal("plugin tool inventory differs from definitions")
	}
	for _, suffix := range []string{"list", "dispatch", "send", "wait", "stop", "archive", "restore", "reset"} {
		name := "builtin_subagent_" + suffix
		if !HasDefinition(definitions, name) || !IsSubtaskTool(name) || !NameAllowedForMode(store.ModeWork, name) || NameAllowedForMode(store.ModeChat, name) {
			t.Fatalf("subagent tool is unavailable or has the wrong mode: %s", name)
		}
		if id, ok := BuiltinPluginIDForTool(name); !ok || id != plugin.BuiltinCollaborationID {
			t.Fatalf("subagent tool has no plugin owner: %s", name)
		}
		found := false
		for _, ref := range plug.Tools {
			found = found || ref.Name == name
		}
		if !found {
			t.Errorf("plugin omitted %s", name)
		}
		for _, prefix := range []string{"builtin_collaboration_", "builtin_subtask_"} {
			retired := prefix + suffix
			if HasDefinition(BuiltinDefinitions(), retired) || IsSubtaskTool(retired) {
				t.Errorf("historical alias is executable: %s", retired)
			}
			if _, ok := BuiltinPluginIDForTool(retired); ok {
				t.Errorf("historical alias resolves to an executable plugin tool: %s", retired)
			}
		}
	}
}

// Retire this historical-family test together with the mapper six calendar
// months after the first stable release, as recorded in the collaboration plan.
func TestSubagentHistoricalNamesMapOnlyShippedAliases(t *testing.T) {
	for _, suffix := range []string{"list", "dispatch", "send", "wait", "stop", "archive", "restore"} {
		want := "builtin_subagent_" + suffix
		if got := CurrentToolName("builtin_subtask_" + suffix); got != want {
			t.Errorf("historical subtask %s: %s", suffix, got)
		}
		if suffix != "archive" && suffix != "restore" {
			if got := CurrentToolName("builtin_collaboration_" + suffix); got != want {
				t.Errorf("historical collaboration %s: %s", suffix, got)
			}
		}
	}
	for _, name := range []string{"builtin_subagent_reset", "builtin_subtask_reset", "builtin_collaboration_archive", "builtin_collaboration_reset", "builtin_subtask_delete", "unrelated_tool"} {
		if got := NormalizeCollaborationTool(name); got != name {
			t.Errorf("invented or nonhistorical alias %q became %q", name, got)
		}
	}
}

func TestSubagentLifecycleSchemasRequireExplicitResetAndArchive(t *testing.T) {
	for _, def := range SubtaskDefinitions() {
		var schema struct {
			Required []string `json:"required"`
			Properties map[string]map[string]any `json:"properties"`
			AdditionalProperties bool `json:"additionalProperties"`
		}
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if schema.AdditionalProperties {
			t.Fatalf("open schema for %s", def.Name)
		}
		required := func(key string) bool {
			for _, value := range schema.Required {
				if value == key { return true }
			}
			return false
		}
		if (def.Name == SubtaskArchive || def.Name == SubtaskRestore || def.Name == SubtaskReset) && !required("session_id") {
			t.Errorf("%s accepts an implicit all-child operation", def.Name)
		}
		if def.Name == SubtaskReset && !required("expected_turn_id") {
			t.Error("reset has no optimistic state boundary")
		}
		if def.Name == SubtaskSend {
			if schema.Properties["reset_context"]["default"] != false || schema.Properties["reset_context"]["type"] != "boolean" {
				t.Error("send must retain history unless explicitly reset")
			}
			if !required("session_id") || !required("prompt") || required("reset_context") {
				t.Error("normal send should need only target and prompt")
			}
		}
	}
}

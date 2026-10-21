package tool

import (
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestSubagentToolsExposeOnlyCanonicalNames(t *testing.T) {
	definitions := SubtaskDefinitions()
	for _, suffix := range []string{"list", "dispatch", "send", "wait", "stop", "archive", "restore", "reset"} {
		name := "builtin_subagent_" + suffix
		if !HasDefinition(definitions, name) || !IsSubtaskTool(name) || !NameAllowedForMode(store.ModeWork, name) || !NameAllowedForMode(store.ModeCode, name) || NameAllowedForMode(store.ModeChat, name) {
			t.Fatalf("subagent tool is unavailable or has the wrong mode: %s", name)
		}
		if id, ok := BuiltinPluginIDForTool(name); !ok || id != plugin.BuiltinSubagentID {
			t.Fatalf("subagent tool has no plugin owner: %s", name)
		}
		for _, prefix := range []string{"builtin_subtask_", "builtin_collaboration_"} {
			retired := prefix + suffix
			if HasDefinition(BuiltinDefinitions(), retired) || HasDefinition(definitions, retired) || IsSubtaskTool(retired) {
				t.Errorf("historical alias remains executable: %s", retired)
			}
			if _, ok := BuiltinPluginIDForTool(retired); ok {
				t.Errorf("historical alias resolves to an executable plugin tool: %s", retired)
			}
		}
	}
}

package tool

import (
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestSubtaskToolsRetireCollaborationNames(t *testing.T) {
	definitions := SubtaskDefinitions()
	for _, suffix := range []string{"list", "dispatch", "send", "wait", "stop"} {
		name := "builtin_subtask_" + suffix
		if !HasDefinition(definitions, name) || !IsSubtaskTool(name) || !NameAllowedForMode(store.ModeWork, name) || NameAllowedForMode(store.ModeChat, name) {
			t.Fatalf("subtask tool is unavailable or has the wrong mode: %s", name)
		}
		if id, ok := BuiltinPluginIDForTool(name); !ok || id != plugin.BuiltinCollaborationID {
			t.Fatalf("subtask tool has no plugin owner: %s", name)
		}
		retired := "builtin_collaboration_" + suffix
		if HasDefinition(BuiltinDefinitions(), retired) || IsSubtaskTool(retired) {
			t.Errorf("retired tool still recognized: %s", retired)
		}
		if _, ok := BuiltinPluginIDForTool(retired); ok {
			t.Errorf("retired tool still resolves to a plugin: %s", retired)
		}
	}
}

package tool

import (
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestLegacyPageAliasesShareOwnershipWithoutDuplicateDefinitions(t *testing.T) {
	for old, current := range map[string]string{
		"builtin_doc_create":  PageCreate,
		"builtin_doc_read":    PageRead,
		"builtin_doc_edit":    PageEdit,
		"builtin_doc_rename":  PageRename,
		"builtin_note_create": PageCreate,
		"builtin_note_read":   PageRead,
		"builtin_note_edit":   PageEdit,
		"builtin_note_rename": PageRename,
	} {
		if !HasDefinition(BuiltinDefinitions(), current) || HasDefinition(BuiltinDefinitions(), old) {
			t.Fatalf("catalog must advertise only %s", current)
		}
		for _, name := range []string{old, current} {
			if CurrentToolName(name) != current || NormalizeLegacyPageTool(name) != current || !IsStudioTool(name) {
				t.Fatalf("alias routing failed for %s", name)
			}
			if owner, ok := BuiltinPluginIDForTool(name); !ok || owner != plugin.BuiltinPageAuthoringID {
				t.Fatalf("wrong alias ownership: %s, %s", name, owner)
			}
			if RequiredModeForName(name) != store.ModeChat {
				t.Fatalf("wrong capability for %s", name)
			}
		}
	}
	for _, name := range []string{"builtin_doc_delete", "builtin_doc_create_extra", PageEdit} {
		if NormalizeLegacyPageTool(name) != name {
			t.Fatalf("unexpected alias: %s", name)
		}
	}

}

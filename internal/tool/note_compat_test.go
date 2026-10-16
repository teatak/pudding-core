package tool

import (
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestLegacyNoteAliasesShareOwnershipWithoutDuplicateDefinitions(t *testing.T) {
	for old, current := range map[string]string{
		"builtin_doc_create": NoteCreate,
		"builtin_doc_read":   NoteRead,
		"builtin_doc_edit":   NoteEdit,
	} {
		if !HasDefinition(BuiltinDefinitions(), current) || HasDefinition(BuiltinDefinitions(), old) {
			t.Fatalf("catalog must advertise only %s", current)
		}
		for _, name := range []string{old, current} {
			if CurrentToolName(name) != current || NormalizeLegacyNoteTool(name) != current || !IsStudioTool(name) {
				t.Fatalf("alias routing failed for %s", name)
			}
			if owner, ok := BuiltinPluginIDForTool(name); !ok || owner != plugin.BuiltinNoteAuthoringID {
				t.Fatalf("wrong alias ownership: %s, %s", name, owner)
			}
			if RequiredModeForName(name) != store.ModeChat {
				t.Fatalf("wrong capability for %s", name)
			}
		}
	}
	for _, name := range []string{"builtin_doc_delete", "builtin_doc_create_extra", DocumentCreate, DocumentRead, NoteEdit} {
		if NormalizeLegacyNoteTool(name) != name {
			t.Fatalf("unexpected alias: %s", name)
		}
	}
	for _, action := range []string{"create", "read", "edit"} {
		current := "builtin_document_" + action
		old := "builtin_rich_document_" + action
		if owner, ok := BuiltinPluginIDForTool(current); !ok || owner != plugin.BuiltinDocumentAuthoringID || !HasDefinition(BuiltinDefinitions(), current) {
			t.Fatalf("structured document ownership missing: %s", current)
		}
		if _, ok := BuiltinPluginIDForTool(old); ok || IsStudioTool(old) || HasDefinition(BuiltinDefinitions(), old) || CurrentToolName(old) != old {
			t.Fatalf("unreleased name must not acquire an alias: %s", old)
		}
	}

}

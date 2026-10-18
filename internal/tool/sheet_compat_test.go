package tool

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
)

func TestSheetAliasesShareCanonicalPermissionsAndCatalog(t *testing.T) {
	for old, current := range map[string]string{"builtin_table_create": SheetCreate, "builtin_table_read": SheetRead, "builtin_table_update": SheetEdit} {
		if !HasDefinition(BuiltinDefinitions(), current) || HasDefinition(BuiltinDefinitions(), old) {
			t.Fatalf("duplicate/missing definition: %s", current)
		}
		for _, name := range []string{old, current} {
			if CurrentToolName(name) != current || NormalizeLegacyContentTool(name) != current || !IsStudioTool(name) {
				t.Fatal("alias routing", name)
			}
			if owner, ok := BuiltinPluginIDForTool(name); !ok || owner != plugin.BuiltinSheetAuthoringID {
				t.Fatal("alias ownership", name, owner)
			}
			if RequiredModeForName(name) != store.ModeChat {
				t.Fatal("alias capability", name)
			}
		}
	}
	for _, name := range []string{"builtin_table_edit", "builtin_table_create_extra", "builtin_sheet_update"} {
		if NormalizeLegacyContentTool(name) != name {
			t.Fatal("invented alias", name)
		}
	}
	defs := SheetDefinitions()
	if len(defs) != 3 || !strings.Contains(string(defs[0].InputSchema), `"sheet"`) || strings.Contains(string(defs[0].InputSchema), `"table"`) {
		t.Fatal("noncanonical sheet schema", defs)
	}
}

func TestSheetCreateArgumentProjectionIsExactAndNonMutating(t *testing.T) {
	old := json.RawMessage(`{"name":"表格🙂","table":{"columns":[],"rows":[]}}`)
	before := string(old)
	projected := CurrentSheetToolArgs("builtin_table_create", old)
	if string(old) != before || strings.Contains(string(projected), `"table"`) || !strings.Contains(string(projected), `"sheet"`) {
		t.Fatal("bad projection", string(projected))
	}
	for _, raw := range []json.RawMessage{old, projected} {
		args, err := DecodeStudioArgs(raw)
		if err != nil || args.Sheet == nil || args.Name != "表格🙂" {
			t.Fatal("cannot decode", err, args)
		}
	}
	for _, raw := range []string{`{"sheet":null,"table":{"columns":[],"rows":[]}}`, `{"sheet":{"columns":[],"rows":[]},"table":null}`} {
		if _, err := DecodeStudioArgs(json.RawMessage(raw)); err == nil {
			t.Fatal("ambiguous fields accepted", raw)
		}
	}
	if string(CurrentSheetToolArgs(PageCreate, old)) != before {
		t.Fatal("note payload changed")
	}
}

package tool

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestPageCatalogRequiresStagedWriting(t *testing.T) {
	for _, def := range PageDefinitions() {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		}
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		switch def.Name {
		case PageEdit:
			if !slices.Contains(schema.Required, "action") || schema.Properties["body"] != nil || schema.Properties["edits"] != nil {
				t.Fatal("catalog permits invisible one-shot edits")
			}
		case PageCreate:
			var body struct {
				Enum []string `json:"enum"`
			}
			if err := json.Unmarshal(schema.Properties["body"], &body); err != nil || !reflect.DeepEqual(body.Enum, []string{""}) {
				t.Fatal("creation must start empty")
			}
		}
	}
}

func TestStudioCatalogAdvertisesPageAndSheet(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range StudioDefinitions() {
		if seen[def.Name] {
			t.Fatalf("duplicate tool %q", def.Name)
		}
		seen[def.Name] = true
		if strings.HasPrefix(def.Name, "builtin_note_") || strings.HasPrefix(def.Name, "builtin_doc_") || strings.HasPrefix(def.Name, "builtin_table_") {
			t.Fatalf("legacy tool advertised: %s", def.Name)
		}
		if def.Name != ArtifactList {
			continue
		}
		var schema struct {
			Properties map[string]struct {
				Enum []string `json:"enum"`
			} `json:"properties"`
		}
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if got := schema.Properties["kind"].Enum; !reflect.DeepEqual(got, []string{"page", "sheet", "widget"}) {
			t.Fatalf("artifact list advertises obsolete kinds: %v", got)
		}
	}
	for _, name := range []string{ArtifactList, ArtifactOpen, PageCreate, PageRead, PageEdit, PageRename, SheetCreate, SheetRead, SheetEdit} {
		if !seen[name] || !IsStudioTool(name) {
			t.Fatalf("missing canonical Studio tool %s", name)
		}
	}
}

func TestCanonicalCatalogRetainsLegacyContentInputs(t *testing.T) {
	for old, current := range map[string]string{
		"builtin_doc_create": PageCreate, "builtin_note_create": PageCreate,
		"builtin_doc_read": PageRead, "builtin_note_read": PageRead,
		"builtin_doc_edit": PageEdit, "builtin_note_edit": PageEdit,
		"builtin_doc_rename": PageRename, "builtin_note_rename": PageRename,
		"builtin_table_create": SheetCreate, "builtin_table_read": SheetRead,
		"builtin_table_update": SheetEdit,
	} {
		if got := NormalizeLegacyContentTool(old); got != current || !IsStudioTool(old) {
			t.Errorf("legacy %s resolved to %s, want %s", old, got, current)
		}
	}
	canonical, err := DecodeStudioArgs(json.RawMessage(`{"sheet":{"columns":[],"rows":[]}}`))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := DecodeStudioArgs(json.RawMessage(`{"table":{"columns":[],"rows":[]}}`))
	if err != nil || legacy.LegacyTable != nil || !reflect.DeepEqual(canonical.Sheet, legacy.Sheet) {
		t.Fatalf("legacy table payload changed: %+v, %v", legacy, err)
	}
	if _, err := DecodeStudioArgs(json.RawMessage(`{"sheet":{},"table":{}}`)); err == nil {
		t.Fatal("ambiguous canonical and legacy payload accepted")
	}
}

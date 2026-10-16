package tool

import (
	"encoding/json"
	"testing"
)

func TestRichDocumentSchemasHaveValidRequiredLists(t *testing.T) {
	var visit func(any)
	visit = func(value any) {
		switch v := value.(type) {
		case map[string]any:
			if required, ok := v["required"]; ok {
				if _, ok := required.([]any); !ok {
					t.Fatalf("required must be an array: %#v", required)
				}
			}
			for _, child := range v {
				visit(child)
			}
		case []any:
			for _, child := range v {
				visit(child)
			}
		}
	}
	for _, def := range RichDocumentDefinitions() {
		var schema map[string]any
		if err := json.Unmarshal(def.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		visit(schema)
		if schema["$defs"] == nil {
			t.Fatalf("%s: missing recursive node schema", def.Name)
		}
	}
}

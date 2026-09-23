package tool

import (
	"encoding/json"
	"slices"
	"testing"
)

// The model must be able to call discovery and temporary export without fields
// that belong only to the existing single-process/project-target variants.
func TestAnalysisToolSchemasAllowDiscoveryAndAllocatedExport(t *testing.T) {
	for _, tc := range []struct {
		name, selector, choice string
		required, optional     []string
	}{
		{CommandSession, "action", "list", []string{"action"}, []string{"process_id"}},
		{AttachmentExport, "scope", "temp", []string{"scope", "attachmentKey"}, []string{"path", "overwrite"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			definition := definitionByName(t, BuiltinDefinitions(), tc.name)
			var schema struct {
				Properties map[string]struct{ Enum []string }
				Required   []string
			}
			if err := json.Unmarshal(definition.InputSchema, &schema); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(schema.Properties[tc.selector].Enum, tc.choice) {
				t.Fatalf("schema does not expose %s=%s", tc.selector, tc.choice)
			}
			for _, field := range tc.required {
				if !slices.Contains(schema.Required, field) {
					t.Fatalf("missing required discriminator/input %s", field)
				}
			}
			for _, field := range tc.optional {
				if slices.Contains(schema.Required, field) {
					t.Fatalf("%s incorrectly requires %s for %s", tc.name, field, tc.choice)
				}
			}
		})
	}
}

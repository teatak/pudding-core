package config

import (
	"context"
	"os"
	"testing"
)

func TestContentPluginEnablementSplitsOnce(t *testing.T) {
	for _, tc := range []struct {
		name, yaml string
		want       map[string]bool
	}{
		{"default", "    browser: false\n", map[string]bool{}},
		{"disabled", "    artifacts: false\n", map[string]bool{"note-authoring": false, "document-authoring": false, "sheet-authoring": false}},
		{"legacy disabled", "    studio: false\n", map[string]bool{"note-authoring": false, "document-authoring": false, "sheet-authoring": false}},
		{"explicit wins", "    artifacts: true\n    docx: false\n", map[string]bool{"note-authoring": true, "document-authoring": false, "sheet-authoring": true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			home := t.TempDir()
			m := NewManager(home)
			if err := m.Prepare(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(m.path(settingsFile), []byte("version: 1\nplugins:\n  enabled:\n"+tc.yaml), 0600); err != nil {
				t.Fatal(err)
			}
			got, err := m.ListPluginEnablement(ctx)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"note-authoring", "document-authoring", "sheet-authoring"} {
				want, exists := tc.want[id]
				if value, ok := got[id]; ok != exists || value != want {
					t.Fatalf("%s = %v; want %v", id, got, tc.want)
				}
			}
			if err := m.SetPluginEnabled(ctx, "artifact", false); err != nil {
				t.Fatal(err)
			}
			if err := m.SetPluginEnabled(ctx, "note-authoring", true); err != nil {
				t.Fatal(err)
			}
			if err := m.DeletePluginEnablement(ctx, "sheet-authoring"); err != nil {
				t.Fatal(err)
			}
			got, err = NewManager(home).ListPluginEnablement(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !got["note-authoring"] || got["artifact"] {
				t.Fatal("switches not independent", got)
			}
			if _, exists := got["sheet-authoring"]; exists {
				t.Fatal("migration repeated after restart", got)
			}
			if got["document-authoring"] != tc.want["document-authoring"] {
				t.Fatal("sibling switch changed", got)
			}
		})
	}
}

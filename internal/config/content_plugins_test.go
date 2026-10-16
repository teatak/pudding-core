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
		{"disabled", "    artifacts: false\n", map[string]bool{"markdown": false, "docx": false, "tables": false}},
		{"legacy disabled", "    studio: false\n", map[string]bool{"markdown": false, "docx": false, "tables": false}},
		{"explicit wins", "    artifacts: true\n    docx: false\n", map[string]bool{"markdown": true, "docx": false, "tables": true}},
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
			for _, id := range []string{"markdown", "docx", "tables"} {
				want, exists := tc.want[id]
				if value, ok := got[id]; ok != exists || value != want {
					t.Fatalf("%s = %v; want %v", id, got, tc.want)
				}
			}
			if err := m.SetPluginEnabled(ctx, "artifacts", false); err != nil {
				t.Fatal(err)
			}
			if err := m.SetPluginEnabled(ctx, "markdown", true); err != nil {
				t.Fatal(err)
			}
			if err := m.DeletePluginEnablement(ctx, "tables"); err != nil {
				t.Fatal(err)
			}
			got, err = NewManager(home).ListPluginEnablement(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !got["markdown"] || got["artifacts"] {
				t.Fatal("switches not independent", got)
			}
			if _, exists := got["tables"]; exists {
				t.Fatal("migration repeated after restart", got)
			}
			if got["docx"] != tc.want["docx"] {
				t.Fatal("sibling switch changed", got)
			}
		})
	}
}

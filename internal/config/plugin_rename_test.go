package config

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestStudioEnablementMigratesAndCurrentValueWins(t *testing.T) {
	for _, tt := range []struct {
		name, enabled string
		want          bool
	}{
		{"disabled", "    studio: false\n", false},
		{"enabled", "    studio: true\n", true},
		{"current wins", "    studio: false\n    artifacts: true\n", true},
		{"current disabled", "    studio: true\n    artifacts: false\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManager(t.TempDir())
			if err := m.Prepare(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(m.path(settingsFile), []byte("version: 1\nplugins:\n  enabled:\n"+tt.enabled+"    browser: false\n"), 0600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				enabled, err := m.ListPluginEnablement(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if v, ok := enabled["artifacts"]; !ok || v != tt.want {
					t.Fatalf("enablement = %v", enabled)
				}
				if _, ok := enabled["studio"]; ok {
					t.Fatal("old ID retained")
				}
				if v, ok := enabled["browser"]; !ok || v {
					t.Fatal("unrelated state changed")
				}
			}
			data, err := os.ReadFile(m.path(settingsFile))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "studio:") {
				t.Fatal("migration not persisted")
			}
		})
	}
}

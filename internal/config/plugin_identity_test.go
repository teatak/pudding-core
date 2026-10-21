package config

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestPluginIDEnablementAliasesPreserveStateAndCanonicalPrecedence(t *testing.T) {
	for old, current := range map[string]string{"artifacts": "artifact", "markdown": "page-authoring", "note-authoring": "page-authoring", "tables": "sheet-authoring", "collaboration": "subagent", "web": "web-search-fetch"} {
		for _, explicit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/canonical=%v", old, explicit), func(t *testing.T) {
				ctx := context.Background()
				home := t.TempDir()
				m := NewManager(home)
				if err := m.Prepare(); err != nil {
					t.Fatal(err)
				}
				body := "version: 1\nplugins:\n  content_plugins_split: true\n  enabled:\n    browser: false\n    custom: true\n    " + old + ": false\n"
				if explicit {
					body += "    " + current + ": true\n"
				}
				if err := os.WriteFile(m.path(settingsFile), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
				for i := 0; i < 2; i++ {
					got, err := NewManager(home).ListPluginEnablement(ctx)
					if err != nil {
						t.Fatal(err)
					}
					if value, ok := got[current]; !ok || value != explicit {
						t.Fatal(got)
					}
					if _, exists := got[old]; exists || got["browser"] || !got["custom"] {
						t.Fatal("state lost", got)
					}
				}
				saved, err := os.ReadFile(m.path(settingsFile))
				if err != nil || strings.Contains(string(saved), "    "+old+":") {
					t.Fatal("alias persisted", err)
				}
				if err := m.SetPluginEnabled(ctx, old, false); err != nil {
					t.Fatal(err)
				}
				got, err := m.ListPluginEnablement(ctx)
				if err != nil || got[current] {
					t.Fatal("legacy setter did not disable canonical plugin", got, err)
				}
				if err := m.DeletePluginEnablement(ctx, old); err != nil {
					t.Fatal(err)
				}
				got, err = NewManager(home).ListPluginEnablement(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if _, exists := got[current]; exists {
					t.Fatal("legacy delete left canonical setting", got)
				}
			})
		}
	}
}

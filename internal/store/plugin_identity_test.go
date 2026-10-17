package store

import (
	"reflect"
	"testing"
)

func TestCanonicalPluginIDsAndOwnedSkillSelectors(t *testing.T) {
	for old, current := range map[string]string{
		"studio":         "artifact",
		"artifacts":      "artifact",
		"markdown":       "page-authoring",
		"note-authoring": "page-authoring",
		"tables":         "sheet-authoring",
		"collaboration":  "subagent",
	} {
		t.Run(old, func(t *testing.T) {
			if got := CurrentPluginID(" " + old + " "); got != current || CurrentPluginID(got) != current {
				t.Fatal(got)
			}
			if got := NormalizePluginIDs([]string{old, current, "browser", "", " " + old}); !reflect.DeepEqual(got, NormalizePluginIDs([]string{current, "browser"})) {
				t.Fatal(got)
			}
			for _, owner := range []string{old, current} {
				if got := CurrentPluginSkillID(owner, old); got != current {
					t.Fatal(owner, got)
				}
				if got := CurrentPluginSkillID(owner, "skills/"+old+"/SKILL.md"); got != "skills/"+current+"/SKILL.md" {
					t.Fatal(got)
				}
			}
			if got := CurrentPluginSkillID("custom", old); got != old {
				t.Fatal("unrelated skill renamed", got)
			}
		})
	}
	for _, id := range []string{"browser", "computer-use", "capture", "widget-authoring", "widget-interaction", "skill-authoring", "plugin-authoring", "custom", "constructor"} {
		if CurrentPluginID(id) != id {
			t.Fatal("unrelated identity changed", id)
		}
	}
	if CurrentPluginSkillID("page-authoring", "tables") != "tables" {
		t.Fatal("a different owner's skill was accepted")
	}
	if CurrentPluginSkillID("page-authoring", "skills/../markdown/SKILL.md") != "skills/../markdown/SKILL.md" {
		t.Fatal("unexpected path normalization")
	}
}

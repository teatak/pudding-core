package plugin

import (
	"context"
	"testing"
)

func TestRenamedBuiltinCatalogAndLegacySkillSelectors(t *testing.T) {
	aliases := map[string]string{"studio": "artifact", "artifacts": "artifact", "markdown": "page-authoring", "note-authoring": "page-authoring", "tables": "sheet-authoring", "collaboration": "subsession", "subagent": "subsession", "web": "web-search-fetch"}
	defs := BuiltinDefinitions()
	seen := map[string]bool{}
	for _, def := range defs {
		if _, legacy := aliases[def.ID]; legacy || seen[def.ID] {
			t.Fatal("legacy or duplicate advertised ID", def.ID)
		}
		seen[def.ID] = true
	}
	service := NewService(t.TempDir(), nil)
	for old, current := range aliases {
		t.Run(old, func(t *testing.T) {
			if !seen[current] || !IsReservedID(old) || !IsReservedID(current) {
				t.Fatal("missing/reservable built-in", current)
			}
			def, ok := BuiltinDefinition(old)
			if !ok || def.ID != current || def.DefaultSkillID != current {
				t.Fatal(def, ok)
			}
			for _, owner := range []string{old, current} {
				for _, selector := range []string{old, current, "skills/" + old + "/SKILL.md", "skills/" + current + "/SKILL.md"} {
					detail, err := service.ReadSkill(context.Background(), owner, selector)
					if err != nil || detail.ID != current || detail.Path != "skills/"+current+"/SKILL.md" || detail.Content == "" {
						t.Fatal(owner, selector, detail, err)
					}
				}
			}
		})
	}
	if _, ok := ReadBuiltinSkill("page-authoring", "tables"); ok {
		t.Fatal("wrong owner's skill resolved")
	}
}

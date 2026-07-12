package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/skill"
)

type fakeSkillSource struct {
	doc *skill.Document
	err error
	got string
}

func (f *fakeSkillSource) ReadSkill(_ context.Context, id string) (*skill.Document, error) {
	f.got = id
	return f.doc, f.err
}

func TestBuiltinSkillRead(t *testing.T) {
	source := &fakeSkillSource{doc: &skill.Document{
		Skill: skill.Skill{
			ID:          "skill-creator",
			Name:        "skill-creator",
			Description: "Create skills.",
			Scope:       skill.ScopeGlobal,
			Source:      skill.SourceBuiltin,
			Path:        "builtin/skill-creator/SKILL.md",
		},
		Content: "# Skill Creator\n",
	}}
	res := NewBuiltinRunner(WithSkills(source)).Call(context.Background(), Call{
		Name: SkillRead,
		Args: json.RawMessage(`{"skill_id":"skill-creator"}`),
	})
	if !res.Ok {
		t.Fatalf("skill read should succeed: %+v", res)
	}
	if source.got != "skill-creator" {
		t.Fatalf("unexpected skill id: %q", source.got)
	}
	if res.SummaryKind != SummaryReadChars || res.SummaryCount != len("# Skill Creator\n") {
		t.Fatalf("unexpected summary: %+v", res)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["id"] != "skill-creator" || payload["content"] != "# Skill Creator\n" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuiltinSkillReadRejectsAppScope(t *testing.T) {
	res := NewBuiltinRunner().Call(context.Background(), Call{
		Name: SkillRead,
		Args: json.RawMessage(`{"app_id":"github","skill_id":"github-issues"}`),
	})
	if res.Ok || !strings.Contains(res.Content, "unknown field") {
		t.Fatalf("app-scoped skill read should be rejected: %+v", res)
	}
}

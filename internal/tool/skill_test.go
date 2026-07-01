package tool

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
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

type fakeAppSkillSource struct {
	doc     *app.SkillDetail
	appID   string
	skillID string
	err     error
}

func (f *fakeAppSkillSource) ReadSkill(_ context.Context, appID, skillID string) (*app.SkillDetail, error) {
	f.appID = appID
	f.skillID = skillID
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

func TestBuiltinSkillReadAppSkill(t *testing.T) {
	source := &fakeAppSkillSource{doc: &app.SkillDetail{
		ID:          "github-issues",
		Name:        "github-issues",
		Description: "Read issues.",
		Path:        "skills/issues/SKILL.md",
		Content:     "# GitHub Issues\n",
	}}
	res := NewBuiltinRunner(WithAppSkills(source)).Call(context.Background(), Call{
		Name: SkillRead,
		Args: json.RawMessage(`{"app_id":"github","skill_id":"github-issues"}`),
	})
	if !res.Ok {
		t.Fatalf("app skill read should succeed: %+v", res)
	}
	if source.appID != "github" || source.skillID != "github-issues" {
		t.Fatalf("unexpected app skill target: app=%q id=%q", source.appID, source.skillID)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["appID"] != "github" || payload["scope"] != "app" || payload["content"] != "# GitHub Issues\n" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

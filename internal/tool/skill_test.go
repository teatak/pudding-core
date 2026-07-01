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
	doc       *app.SkillDetail
	appID     string
	skillPath string
	err       error
}

func (f *fakeAppSkillSource) ReadSkill(_ context.Context, appID, skillPath string) (*app.SkillDetail, error) {
	f.appID = appID
	f.skillPath = skillPath
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
		Args: json.RawMessage(`{"app_id":"github","skill_id":"skills/issues/SKILL.md"}`),
	})
	if !res.Ok {
		t.Fatalf("app skill read should succeed: %+v", res)
	}
	if source.appID != "github" || source.skillPath != "skills/issues/SKILL.md" {
		t.Fatalf("unexpected app skill target: app=%q path=%q", source.appID, source.skillPath)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["appID"] != "github" || payload["scope"] != "app" || payload["content"] != "# GitHub Issues\n" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

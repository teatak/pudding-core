package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/skill"
)

type fakeSkillSource struct {
	doc           *skill.Document
	err           error
	got           string
	validation    *skill.Validation
	validationErr error
	validateGot   string
}

func (f *fakeSkillSource) ReadSkill(_ context.Context, id string) (*skill.Document, error) {
	f.got = id
	return f.doc, f.err
}

func (f *fakeSkillSource) ValidateSkill(_ context.Context, id string) (*skill.Validation, error) {
	f.validateGot = id
	return f.validation, f.validationErr
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

func TestBuiltinSkillValidate(t *testing.T) {
	source := &fakeSkillSource{validation: &skill.Validation{OK: true}}
	res := NewBuiltinRunner(WithSkills(source)).Call(context.Background(), Call{
		Name: SkillValidate,
		Args: json.RawMessage(`{"skill_id":"demo-skill"}`),
	})
	if !res.Ok {
		t.Fatalf("skill validate should succeed: %+v", res)
	}
	if source.validateGot != "demo-skill" {
		t.Fatalf("unexpected skill id: %q", source.validateGot)
	}
	payload := decodeToolResult(t, res)
	if payload["ok"] != true || payload["skill_id"] != "demo-skill" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestBuiltinSkillDirectCreateFlow(t *testing.T) {
	home := t.TempDir()
	service := skill.NewService(home)
	runner := NewBuiltinRunner(WithHomeDir(home), WithSkills(service))
	body := "---\nname: demo-skill\ndescription: Demonstrate the direct Skill creation flow.\n---\n# Demo Skill\n\nFollow the requested demo workflow.\n"
	write := runner.Call(context.Background(), Call{
		Name: FileWrite,
		Args: json.RawMessage(`{"scope":"skill","path":"demo-skill/SKILL.md","content":` + mustJSONText(t, body) + `}`),
	})
	if !write.Ok {
		t.Fatalf("skill write should succeed: %+v", write)
	}
	validation := runner.Call(context.Background(), Call{
		Name: SkillValidate,
		Args: json.RawMessage(`{"skill_id":"demo-skill"}`),
	})
	if !validation.Ok {
		t.Fatalf("skill validation should succeed: %+v", validation)
	}
	items, err := service.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "demo-skill" && item.Source == skill.SourceUser {
			return
		}
	}
	t.Fatalf("directly created skill not listed: %+v", items)
}

func mustJSONText(t *testing.T, value string) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

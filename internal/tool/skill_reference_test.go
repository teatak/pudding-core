package tool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/skill"
)

func TestSkillReadReturnsReferenceWithoutBody(t *testing.T) {
	source := &fakeSkillSource{doc: &skill.Document{
		Skill: skill.Skill{ID: "demo", Name: "Demo"}, Content: "instruction body must not be persisted",
	}}
	result := NewBuiltinRunner(WithSkills(source)).Call(context.Background(), Call{
		Name: SkillRead, Args: json.RawMessage(`{"skill_id":"demo"}`),
	})
	if !result.Ok || strings.Contains(result.Content, source.doc.Content) || !strings.Contains(result.Content, `"reference"`) {
		t.Fatalf("expected a body-free reference: %+v", result)
	}
}

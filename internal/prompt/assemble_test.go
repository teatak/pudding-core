package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/skill"
)

func TestAssembleIncludesCoreAndUserInstruction(t *testing.T) {
	out := Assemble(Input{UserInstruction: "  请尽量简短  ", Mode: "research"})
	if !strings.Contains(out.SystemInstruction, "You are Pudding") {
		t.Fatalf("assembled prompt missing core:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Research Mode") {
		t.Fatalf("assembled prompt missing mode:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "请尽量简短") {
		t.Fatalf("assembled prompt missing user instruction:\n%s", out.SystemInstruction)
	}
	if len(out.Segments) != 3 || out.Segments[0].ID != "core_system" || out.Segments[1].ID != "mode_research" || out.Segments[2].ID != "user_system" {
		t.Fatalf("unexpected segments: %+v", out.Segments)
	}
}

func TestAssembleIncludesSkillsIndex(t *testing.T) {
	home := t.TempDir()
	out := Assemble(Input{
		Mode: "chat",
		Home: home,
		Skills: []skill.Skill{
			{
				ID:          "skill-creator",
				Description: "Create or update Pudding skills.",
				Source:      skill.SourceUser,
				Path:        "skill-creator/SKILL.md",
			},
		},
	})
	realPath := filepath.Join(home, "skills", "skill-creator", "SKILL.md")
	if !strings.Contains(out.SystemInstruction, "## Available Skills") {
		t.Fatalf("assembled prompt missing skills index:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "`skill-creator`") || !strings.Contains(out.SystemInstruction, "Create or update Pudding skills.") {
		t.Fatalf("assembled prompt missing skill metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, realPath) {
		t.Fatalf("assembled prompt missing real skill path %q:\n%s", realPath, out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "builtin_skill_read") {
		t.Fatalf("assembled prompt missing skill read instruction:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "# Skill Creator") {
		t.Fatalf("assembled prompt should not inline SKILL.md body:\n%s", out.SystemInstruction)
	}
}

func TestAssembleDoesNotShowPseudoPathForBuiltinSkill(t *testing.T) {
	out := Assemble(Input{
		Mode: "chat",
		Skills: []skill.Skill{
			{
				ID:          "skill-creator",
				Description: "Create or update Pudding skills.",
				Source:      skill.SourceBuiltin,
				Path:        "builtin/skill-creator/SKILL.md",
			},
		},
	})
	if strings.Contains(out.SystemInstruction, "builtin://") || strings.Contains(out.SystemInstruction, "path:") {
		t.Fatalf("assembled prompt should not show pseudo path for builtin skill:\n%s", out.SystemInstruction)
	}
}

func TestAssembleIncludesAppsIndex(t *testing.T) {
	appPath := filepath.Join(t.TempDir(), "apps", "github", app.AppFileName)
	realSkillPath := filepath.Join(filepath.Dir(appPath), "skills", "issues", "SKILL.md")
	out := Assemble(Input{
		Mode: "chat",
		Apps: []*app.Definition{
			{
				ID:          "github",
				Name:        "GitHub",
				Description: "Access repositories and issues.",
				Path:        appPath,
				Endpoints: map[string]app.Endpoint{
					"github_rest": {
						Kind:        app.EndpointKindREST,
						Description: "GitHub REST API.",
					},
				},
				Skills: []app.SkillRef{
					{
						ID:          "github-issues",
						Name:        "github-issues",
						Description: "Inspect GitHub issues.",
						Path:        "skills/issues/SKILL.md",
					},
				},
			},
		},
	})
	if !strings.Contains(out.SystemInstruction, "## Installed Apps") {
		t.Fatalf("assembled prompt missing apps index:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "App `github`") || !strings.Contains(out.SystemInstruction, "Endpoint `github_rest`") {
		t.Fatalf("assembled prompt missing app endpoint metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, `builtin_skill_read(app_id="<app id>", skill_id="<skill id>")`) {
		t.Fatalf("assembled prompt missing app skill read instruction:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Skill `github-issues`") || !strings.Contains(out.SystemInstruction, realSkillPath) {
		t.Fatalf("assembled prompt missing app skill id or real path %q:\n%s", realSkillPath, out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "# GitHub Issues") {
		t.Fatalf("assembled prompt should not inline app skill body:\n%s", out.SystemInstruction)
	}
}

func TestLoaderIncludesBuiltinSkillsIndex(t *testing.T) {
	home := t.TempDir()
	out, err := NewLoader(home).Prompt(context.Background(), "chat")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.SystemInstruction, "## Available Skills") || !strings.Contains(out.SystemInstruction, "`skill-creator`") {
		t.Fatalf("loader prompt missing builtin skill index:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "# Skill Creator") {
		t.Fatalf("loader prompt should not inline builtin skill body:\n%s", out.SystemInstruction)
	}
}

func TestLoadUserInstruction(t *testing.T) {
	home := t.TempDir()
	if got, err := LoadUserInstruction(home); err != nil || got != "" {
		t.Fatalf("missing prompt should be empty, got %q err=%v", got, err)
	}
	if err := os.WriteFile(filepath.Join(home, defaultUserPromptName), []byte("  custom prompt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadUserInstruction(home)
	if err != nil {
		t.Fatal(err)
	}
	if got != "custom prompt" {
		t.Fatalf("unexpected prompt: %q", got)
	}
}

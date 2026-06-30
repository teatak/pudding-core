package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	out := Assemble(Input{
		Mode: "chat",
		Skills: []skill.Skill{
			{
				ID:          "skill-creator",
				Description: "Create or update Pudding skills.",
				Source:      skill.SourceBuiltin,
				Path:        ".system/skill-creator/SKILL.md",
			},
		},
	})
	if !strings.Contains(out.SystemInstruction, "## Available Skills") {
		t.Fatalf("assembled prompt missing skills index:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "`skill-creator`") || !strings.Contains(out.SystemInstruction, "Create or update Pudding skills.") {
		t.Fatalf("assembled prompt missing skill metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "builtin_skill_read") {
		t.Fatalf("assembled prompt missing skill read instruction:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "# Skill Creator") {
		t.Fatalf("assembled prompt should not inline SKILL.md body:\n%s", out.SystemInstruction)
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

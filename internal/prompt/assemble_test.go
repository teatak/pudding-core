package prompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssembleIncludesCoreAndUserInstruction(t *testing.T) {
	out := Assemble(Input{UserInstruction: "  请尽量简短  "})
	if !strings.Contains(out.SystemInstruction, "You are Pudding") {
		t.Fatalf("assembled prompt missing core:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "请尽量简短") {
		t.Fatalf("assembled prompt missing user instruction:\n%s", out.SystemInstruction)
	}
	if len(out.Segments) != 2 || out.Segments[0].ID != "core_system" || out.Segments[1].ID != "user_system" {
		t.Fatalf("unexpected segments: %+v", out.Segments)
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

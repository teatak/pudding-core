package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestServiceValidateSkill(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "daily-review")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: daily-review\ndescription: Create a concise daily review skill.\n---\n# Daily Review\n"
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	validation, err := NewService(home).ValidateSkill(context.Background(), "daily-review")
	if err != nil {
		t.Fatal(err)
	}
	if !validation.OK || len(validation.Errors) != 0 {
		t.Fatalf("unexpected validation: %+v", validation)
	}
}

func TestServiceValidateSkillReportsErrors(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "bad-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte("# Missing frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	validation, err := NewService(home).ValidateSkill(context.Background(), "bad-skill")
	if err != nil {
		t.Fatal(err)
	}
	if validation.OK || len(validation.Errors) == 0 {
		t.Fatalf("expected validation errors: %+v", validation)
	}
}

func TestServiceValidateSkillReportsMissingDirectory(t *testing.T) {
	validation, err := NewService(t.TempDir()).ValidateSkill(context.Background(), "missing-skill")
	if err != nil {
		t.Fatal(err)
	}
	if validation.OK || len(validation.Errors) != 1 || validation.Errors[0] != "skill directory does not exist" {
		t.Fatalf("unexpected validation: %+v", validation)
	}
}

func TestServiceValidateSkillRejectsSymlinkedSkillFile(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "linked-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), SkillFileName)
	body := "---\nname: linked-skill\ndescription: This content lives outside the managed Skill directory.\n---\n# Linked\n"
	if err := os.WriteFile(outside, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, SkillFileName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	validation, err := NewService(home).ValidateSkill(context.Background(), "linked-skill")
	if err != nil {
		t.Fatal(err)
	}
	if validation.OK || len(validation.Errors) == 0 {
		t.Fatalf("symlinked Skill should be rejected: %+v", validation)
	}
}

func TestServiceValidateSkillRejectsBuiltinID(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "skills"), 0o700); err != nil {
		t.Fatal(err)
	}
	validation, err := NewService(home).ValidateSkill(context.Background(), "skill-creator")
	if err != nil {
		t.Fatal(err)
	}
	if validation.OK || len(validation.Errors) == 0 {
		t.Fatalf("builtin id should be rejected: %+v", validation)
	}
}

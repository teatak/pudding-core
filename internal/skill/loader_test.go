package skill

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBuiltinSkillsIncludesSkillCreator(t *testing.T) {
	skills, err := LoadBuiltinSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range skills {
		if item.ID == "skill-creator" {
			if item.Scope != ScopeGlobal || item.Source != SourceBuiltin || !item.System || item.Description == "" || item.IconPath != "builtin/skill-creator/assets/icon.svg" {
				t.Fatalf("unexpected skill-creator metadata: %+v", item)
			}
			return
		}
	}
	t.Fatalf("skill-creator not found: %+v", skills)
}

func TestServiceListsUserGlobalSkills(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "daily-review")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte("---\nname: daily-review\ndescription: Summarize daily notes.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "assets", "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	skills, err := NewService(home).ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, item := range skills {
		if item.ID == "daily-review" {
			found = true
			if item.Scope != ScopeGlobal || item.Source != SourceUser || item.System || item.IconPath != "daily-review/assets/icon.svg" {
				t.Fatalf("unexpected user skill metadata: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("user skill not found: %+v", skills)
	}
}

func TestServiceDeletesUserSkill(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "daily-review")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte("---\nname: daily-review\ndescription: Summarize daily notes.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewService(home).DeleteSkill(context.Background(), "daily-review"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("skill dir should be removed, stat err=%v", err)
	}
}

func TestServiceReadsSkillDocument(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "daily-review")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: daily-review\ndescription: Summarize daily notes.\n---\n# Daily Review\n"
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := NewService(home).ReadSkill(context.Background(), "daily-review")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "daily-review" || doc.Source != SourceUser || doc.Content != body {
		t.Fatalf("unexpected user skill doc: %+v", doc)
	}
	doc, err = NewService(home).ReadSkill(context.Background(), "skill-creator")
	if err != nil {
		t.Fatal(err)
	}
	if doc.ID != "skill-creator" || doc.Source != SourceBuiltin || doc.Content == "" {
		t.Fatalf("unexpected builtin skill doc: %+v", doc)
	}
}

func TestServiceReadsSkillAssets(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "daily-review", "assets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, contentType, err := NewService(home).ReadAsset(context.Background(), "daily-review/assets/icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<svg/>" || contentType != "image/svg+xml" {
		t.Fatalf("unexpected asset: %q %s", data, contentType)
	}
	data, contentType, err = NewService(home).ReadAsset(context.Background(), "builtin/skill-creator/assets/icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || contentType != "image/svg+xml" {
		t.Fatalf("unexpected builtin asset: len=%d type=%s", len(data), contentType)
	}
	draftDir := filepath.Join(home, DraftDirName, "draft-review", "assets")
	if err := os.MkdirAll(draftDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draftDir, "icon.svg"), []byte("<svg draft/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, contentType, err = NewService(home).ReadAsset(context.Background(), "skills-draft/draft-review/assets/icon.svg")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<svg draft/>" || contentType != "image/svg+xml" {
		t.Fatalf("unexpected draft asset: %q %s", data, contentType)
	}
}

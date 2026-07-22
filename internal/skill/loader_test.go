package skill

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAppOwnedBuiltinSkillsAreExcludedFromGlobalCatalog(t *testing.T) {
	skills, err := LoadBuiltinSkills()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range skills {
		if item.ID == "skill-creator" || item.ID == "app-creator" {
			t.Fatalf("App-owned Skill leaked into global catalog: %+v", item)
		}
	}
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

func TestServiceRejectsSymlinkedUserSkillsRoot(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	dir := filepath.Join(outside, "daily-review")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SkillFileName), []byte("---\nname: daily-review\ndescription: Summarize daily notes.\n---\nBody\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "skills")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	service := NewService(home)
	if _, err := service.ListSkills(context.Background()); err == nil {
		t.Fatal("symlinked Skill root should not be loaded")
	}
	if err := service.DeleteSkill(context.Background(), "daily-review"); err == nil {
		t.Fatal("delete through symlinked Skill root should fail")
	}
	if _, err := os.Stat(filepath.Join(dir, SkillFileName)); err != nil {
		t.Fatalf("outside Skill was removed: %v", err)
	}
}

func TestAppSkillIDDoesNotReserveGlobalSkillNamespace(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "skill-creator")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, SkillFileName)
	if err := os.WriteFile(marker, []byte("hidden duplicate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := NewService(home).DeleteSkill(context.Background(), "skill-creator"); err != nil {
		t.Fatalf("delete global Skill sharing an App Skill id: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("global Skill was not removed: %v", err)
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
	if _, err = NewService(home).ReadSkill(context.Background(), "skill-creator"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("App-owned Skill should not be globally readable: %v", err)
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
	if _, _, err = NewService(home).ReadAsset(context.Background(), "builtin/skill-creator/assets/icon.svg"); !errors.Is(err, ErrInvalidAsset) {
		t.Fatalf("App-owned Skill asset should not be globally readable: %v", err)
	}
}

func TestServiceRejectsUserSkillAssetSymlinkEscape(t *testing.T) {
	homeDir := t.TempDir()
	assetsDir := filepath.Join(homeDir, "skills", "daily-review", "assets")
	if err := os.MkdirAll(assetsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "secret.svg")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(assetsDir, "icon.svg")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewService(homeDir).ReadAsset(context.Background(), "daily-review/assets/icon.svg"); !errors.Is(err, ErrInvalidAsset) {
		t.Fatalf("asset symlink escape error = %v, want ErrInvalidAsset", err)
	}
}

func TestServiceRejectsSymlinkedUserSkillFile(t *testing.T) {
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

	service := NewService(home)
	items, err := service.ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "linked-skill" {
			t.Fatalf("symlinked Skill was loaded: %+v", item)
		}
	}
	if _, err := service.ReadSkill(context.Background(), "linked-skill"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read symlinked Skill error = %v, want ErrNotFound", err)
	}
}

func TestServiceRejectsUserSkillFileSymlinkInsideSkillDirectory(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "skills", "linked-skill")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: linked-skill\ndescription: This content is stored beside the link.\n---\n# Linked\n"
	if err := os.WriteFile(filepath.Join(dir, "REAL.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("REAL.md", filepath.Join(dir, SkillFileName)); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	items, err := NewService(home).ListSkills(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == "linked-skill" {
			t.Fatalf("symlinked Skill was loaded: %+v", item)
		}
	}
}

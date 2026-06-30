package skill

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServiceDraftLifecycle(t *testing.T) {
	home := t.TempDir()
	draftDir := filepath.Join(home, DraftDirName, "daily-review")
	if err := os.MkdirAll(filepath.Join(draftDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: daily-review\ndescription: Create a concise daily review skill.\n---\n# Daily Review\n"
	if err := os.WriteFile(filepath.Join(draftDir, SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draftDir, "assets", "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(home)
	drafts, err := svc.ListDrafts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].ID != "daily-review" || !drafts[0].Validation.OK {
		t.Fatalf("unexpected drafts: %+v", drafts)
	}
	if drafts[0].IconPath != "skills-draft/daily-review/assets/icon.svg" {
		t.Fatalf("unexpected draft icon path: %q", drafts[0].IconPath)
	}
	detail, err := svc.DraftDetail(context.Background(), "daily-review")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Draft.IconPath != "skills-draft/daily-review/assets/icon.svg" {
		t.Fatalf("unexpected detail icon path: %q", detail.Draft.IconPath)
	}
	if len(detail.Files) != 2 || !strings.Contains(detail.Files[0].UnifiedDiff+detail.Files[1].UnifiedDiff, "Daily Review") {
		t.Fatalf("unexpected draft diff: %+v", detail.Files)
	}
	if err := svc.ApplyDraft(context.Background(), "daily-review"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "skills", "daily-review", SkillFileName)); err != nil {
		t.Fatalf("published skill missing: %v", err)
	}
	if _, err := os.Stat(draftDir); !os.IsNotExist(err) {
		t.Fatalf("draft should be removed, stat err=%v", err)
	}
}

func TestServiceValidateDraftReportsErrors(t *testing.T) {
	home := t.TempDir()
	draftDir := filepath.Join(home, DraftDirName, "bad-skill")
	if err := os.MkdirAll(draftDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draftDir, SkillFileName), []byte("# Missing frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	validation, err := NewService(home).ValidateDraft(context.Background(), "bad-skill")
	if err != nil {
		t.Fatal(err)
	}
	if validation.OK || len(validation.Errors) == 0 {
		t.Fatalf("expected validation errors: %+v", validation)
	}
}

func TestServiceAppliesIncrementalExistingSkillDraft(t *testing.T) {
	home := t.TempDir()
	publishedDir := filepath.Join(home, "skills", "recipe-helper")
	if err := os.MkdirAll(filepath.Join(publishedDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	oldBody := "---\nname: recipe-helper\ndescription: Help with recipes and cooking steps.\n---\n# Recipe Helper\n\n## Rules\n\n- Prefer simple meals\n- Respect ingredients\n"
	if err := os.WriteFile(filepath.Join(publishedDir, SkillFileName), []byte(oldBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publishedDir, "assets", "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	draftDir := filepath.Join(home, DraftDirName, "recipe-helper")
	if err := os.MkdirAll(draftDir, 0o700); err != nil {
		t.Fatal(err)
	}
	newBody := strings.Replace(oldBody, "Prefer simple meals", "Prefer quick home meals", 1)
	if err := os.WriteFile(filepath.Join(draftDir, SkillFileName), []byte(newBody), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(home)
	detail, err := svc.DraftDetail(context.Background(), "recipe-helper")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Draft.IconPath != "recipe-helper/assets/icon.svg" {
		t.Fatalf("unexpected draft icon path: %q", detail.Draft.IconPath)
	}
	if _, err := os.Stat(filepath.Join(draftDir, "assets", "icon.svg")); !os.IsNotExist(err) {
		t.Fatalf("draft icon should not be materialized, stat err=%v", err)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != SkillFileName || detail.Files[0].Change != DraftChangeModified {
		t.Fatalf("unexpected files: %+v", detail.Files)
	}
	diff := detail.Files[0].UnifiedDiff
	if !strings.Contains(diff, "-- Prefer simple meals") || !strings.Contains(diff, "+- Prefer quick home meals") {
		t.Fatalf("expected changed lines in diff:\n%s", diff)
	}
	if strings.Contains(diff, "-# Recipe Helper") || strings.Contains(diff, "+# Recipe Helper") {
		t.Fatalf("unchanged title should not be shown as removed/added:\n%s", diff)
	}
	if err := svc.ApplyDraft(context.Background(), "recipe-helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(publishedDir, "assets", "icon.svg")); err != nil {
		t.Fatalf("published icon should be preserved: %v", err)
	}
}

func TestServiceDraftDeleteManifest(t *testing.T) {
	home := t.TempDir()
	publishedDir := filepath.Join(home, "skills", "icon-skill")
	if err := os.MkdirAll(filepath.Join(publishedDir, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: icon-skill\ndescription: Helps test explicit icon deletion.\n---\n# Icon Skill\n"
	if err := os.WriteFile(filepath.Join(publishedDir, SkillFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(publishedDir, "assets", "icon.svg"), []byte("<svg/>"), 0o600); err != nil {
		t.Fatal(err)
	}
	draftDir := filepath.Join(home, DraftDirName, "icon-skill")
	if err := os.MkdirAll(draftDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(draftDir, DraftDeleteFileName), []byte("assets/icon.svg\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	svc := NewService(home)
	detail, err := svc.DraftDetail(context.Background(), "icon-skill")
	if err != nil {
		t.Fatal(err)
	}
	if detail.Draft.IconPath != "" {
		t.Fatalf("deleted icon should not be shown: %q", detail.Draft.IconPath)
	}
	if len(detail.Files) != 1 || detail.Files[0].Path != "assets/icon.svg" || detail.Files[0].Change != "deleted" {
		t.Fatalf("unexpected files: %+v", detail.Files)
	}
	if err := svc.ApplyDraft(context.Background(), "icon-skill"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(publishedDir, "assets", "icon.svg")); !os.IsNotExist(err) {
		t.Fatalf("published icon should be deleted, stat err=%v", err)
	}
}

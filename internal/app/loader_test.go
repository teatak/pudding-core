package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserDefinitions(t *testing.T) {
	root := t.TempDir()
	appDir := filepath.Join(root, "github")
	if err := os.MkdirAll(filepath.Join(appDir, "skills", "issues"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, AppFileName), []byte(`
id: github
name: GitHub
description: GitHub API access
endpoints:
  github_rest:
    kind: rest
    url: https://api.github.com
skills:
  - skills/issues/SKILL.md
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "skills", "issues", "SKILL.md"), []byte(`---
name: github-issues
description: Read GitHub issues.
---

Use builtin_rest_request with github_rest.
`), 0o600); err != nil {
		t.Fatal(err)
	}

	defs, err := LoadUserDefinitions(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one app, got %+v", defs)
	}
	def := defs[0]
	if def.ID != "github" || def.Endpoints["github_rest"].Kind != EndpointKindREST {
		t.Fatalf("unexpected definition: %+v", def)
	}
	if len(def.Skills) != 1 || def.Skills[0].ID != "github-issues" || def.Skills[0].Description == "" {
		t.Fatalf("skill frontmatter not loaded: %+v", def.Skills)
	}
}

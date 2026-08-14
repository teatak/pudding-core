package prompt

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/skill"
)

func TestAssembleIncludesCoreAndUserInstruction(t *testing.T) {
	now := time.Date(2026, 7, 2, 15, 30, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	out := Assemble(Input{UserInstruction: "  请尽量简短  ", Mode: "research", RuntimeNow: now})
	if !strings.Contains(out.SystemInstruction, "You are Pudding") {
		t.Fatalf("assembled prompt missing core:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Chat Mode") {
		t.Fatalf("assembled prompt missing mode:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "请尽量简短") {
		t.Fatalf("assembled prompt missing user instruction:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Current date: 2026-07-02") || !strings.Contains(out.SystemInstruction, "UTC offset: +08:00") {
		t.Fatalf("assembled prompt missing runtime date:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "builtin_history_search") || !strings.Contains(out.SystemInstruction, "builtin_history_get_message") {
		t.Fatalf("assembled prompt missing history tool guidance:\n%s", out.SystemInstruction)
	}
	if len(out.Segments) != 4 || out.Segments[0].ID != "core_system" || out.Segments[1].ID != "mode_chat" || out.Segments[2].ID != "user_system" || out.Segments[3].ID != "runtime_context" {
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
				ID:          "daily-review",
				Description: "Review daily notes.",
				Source:      skill.SourceUser,
				Path:        "daily-review/SKILL.md",
			},
		},
	})
	realPath := filepath.Join(home, "skills", "daily-review", "SKILL.md")
	if !strings.Contains(out.SystemInstruction, "## Available Skills") {
		t.Fatalf("assembled prompt missing skills index:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "`daily-review`") || !strings.Contains(out.SystemInstruction, "Review daily notes.") {
		t.Fatalf("assembled prompt missing skill metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, realPath) {
		t.Fatalf("assembled prompt missing real skill path %q:\n%s", realPath, out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "builtin_skill_read") {
		t.Fatalf("assembled prompt missing skill read instruction:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "# Daily Review") {
		t.Fatalf("assembled prompt should not inline SKILL.md body:\n%s", out.SystemInstruction)
	}
}

func TestAssembleDoesNotShowPseudoPathForBuiltinSkill(t *testing.T) {
	out := Assemble(Input{
		Mode: "chat",
		Skills: []skill.Skill{
			{
				ID:          "system-guide",
				Description: "Follow a bundled system workflow.",
				Source:      skill.SourceBuiltin,
				Path:        "builtin/system-guide/SKILL.md",
			},
		},
	})
	if strings.Contains(out.SystemInstruction, "builtin://") || strings.Contains(out.SystemInstruction, "path:") {
		t.Fatalf("assembled prompt should not show pseudo path for builtin skill:\n%s", out.SystemInstruction)
	}
}

func TestAssembleIncludesAppsIndex(t *testing.T) {
	appPath := filepath.Join(t.TempDir(), "apps", "github", app.AppFileName)
	out := Assemble(Input{
		Mode: "work",
		Apps: []*app.Definition{
			{
				ID:             "github",
				Name:           "GitHub",
				Description:    "Access repositories and issues.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "github-issues",
				Path:           appPath,
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
	if !strings.Contains(out.SystemInstruction, "## Available Apps") {
		t.Fatalf("assembled prompt missing apps index:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "App `github`") || !strings.Contains(out.SystemInstruction, "requires Work") {
		t.Fatalf("assembled prompt missing compact app metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, `builtin_app_load(app_id="<app id>")`) {
		t.Fatalf("assembled prompt missing explicit app load instruction:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Never use `builtin_skill_read` to load an App") {
		t.Fatalf("assembled prompt missing App loading boundary:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Default skill `github-issues`") {
		t.Fatalf("assembled prompt missing default app skill:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "Endpoint `github_rest`") || strings.Contains(out.SystemInstruction, "skills/issues/SKILL.md") {
		t.Fatalf("compact app index must not expose endpoint or path details:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "# GitHub Issues") {
		t.Fatalf("assembled prompt should not inline app skill body:\n%s", out.SystemInstruction)
	}
}

func TestAssembleMarksLoadedApps(t *testing.T) {
	out := Assemble(Input{
		Mode:         "work",
		LoadedAppIDs: []string{"github"},
		Apps: []*app.Definition{{
			ID:             "github",
			Name:           "GitHub",
			Enabled:        true,
			RequiredMode:   "work",
			DefaultSkillID: "github-issues",
			Skills: []app.SkillRef{{
				ID:          "github-issues",
				Description: "Inspect GitHub issues.",
			}},
		}},
	})
	if !strings.Contains(out.SystemInstruction, "Status: loaded for this session") {
		t.Fatalf("assembled prompt missing loaded App status:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Do not call `builtin_app_load` again for their default skill") {
		t.Fatalf("assembled prompt missing duplicate load guidance:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, `builtin_app_unload(app_id="<loaded app id>")`) {
		t.Fatalf("assembled prompt missing App unload guidance:\n%s", out.SystemInstruction)
	}
}

func TestAssembleSummarizesUnconnectedApp(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Apps: []*app.Definition{
			{
				ID:             "github",
				Name:           "GitHub",
				Description:    "Access repositories and issues.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "github-issues",
				Auth:           &app.AuthConfig{Required: true},
				Endpoints: map[string]app.Endpoint{
					"github_rest": {Kind: app.EndpointKindREST, Description: "GitHub REST API."},
				},
				Skills: []app.SkillRef{{
					ID:          "github-issues",
					Description: "Inspect GitHub issues.",
					Path:        "skills/issues/SKILL.md",
				}},
			},
		},
	})
	if !strings.Contains(out.SystemInstruction, "App `github`") || !strings.Contains(out.SystemInstruction, "Status: not connected") {
		t.Fatalf("assembled prompt should summarize unconnected app:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "Endpoint `github_rest`") || strings.Contains(out.SystemInstruction, "Skill `github-issues`") {
		t.Fatalf("unconnected app should not expose endpoints or skills:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "Default skill `github-issues`") {
		t.Fatalf("unconnected app should not advertise its default skill:\n%s", out.SystemInstruction)
	}
}

func TestAssembleShowsConnectedAppFully(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Apps: []*app.Definition{
			{
				ID:             "github",
				Name:           "GitHub",
				Description:    "Access repositories and issues.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "github-issues",
				Auth:           &app.AuthConfig{Required: true},
				Endpoints: map[string]app.Endpoint{
					"github_rest": {Kind: app.EndpointKindREST, Description: "GitHub REST API."},
				},
				Skills: []app.SkillRef{{
					ID:          "github-issues",
					Description: "Inspect GitHub issues.",
					Path:        "skills/issues/SKILL.md",
				}},
			},
		},
		AppConnections: []*app.Connection{
			{
				ID: "github-main", Name: "GitHub · octocat", AppID: "github",
				Account: &app.ConnectionAccount{Login: "octocat"},
				Auth:    app.Auth{MethodID: app.GitHubAppAuthMethodID, Type: app.AuthTypeOAuth2, Variant: app.GitHubAppAuthVariant},
			},
			{
				ID: "github-pat", Name: "GitHub PAT", AppID: "github",
				Auth: app.Auth{MethodID: "github-pat", Type: app.AuthTypeBearer},
			},
		},
	})
	if strings.Contains(out.SystemInstruction, "Status: not connected") {
		t.Fatalf("connected app should not be marked unavailable:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Default skill `github-issues`") || strings.Contains(out.SystemInstruction, "Endpoint `github_rest`") {
		t.Fatalf("connected app should expose only compact loading metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Connection `github-main` (GitHub · octocat): auth method `github-app`, variant `github_app`, account `octocat`") {
		t.Fatalf("connected app should expose non-secret auth routing metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Connection `github-pat` (GitHub PAT): auth method `github-pat`") {
		t.Fatalf("connected app should distinguish PAT routing metadata:\n%s", out.SystemInstruction)
	}
}

func TestAssembleTreatsLegacyGitHubOAuthAsDisconnected(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Apps: []*app.Definition{{
			ID: "github", Name: "GitHub", Enabled: true, RequiredMode: "work",
			Auth: &app.AuthConfig{Required: true},
		}},
		AppConnections: []*app.Connection{{
			ID: "github-main", AppID: "github",
			Auth: app.Auth{MethodID: "github-oauth", Type: app.AuthTypeOAuth2, Variant: app.GitHubAppAuthVariant, AccessToken: "legacy-token"},
		}},
	})
	if !strings.Contains(out.SystemInstruction, "Status: not connected") {
		t.Fatalf("legacy GitHub OAuth must require reauthorization:\n%s", out.SystemInstruction)
	}
}

func TestAssembleShowsConnectionlessSkillsOnlyAppFully(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Apps: []*app.Definition{
			{
				ID:             "notebook-helper",
				Name:           "Notebook Helper",
				Description:    "Guide notebook workflows.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "notebook-review",
				Auth:           &app.AuthConfig{Required: false},
				Skills: []app.SkillRef{{
					ID:          "notebook-review",
					Description: "Review a notebook.",
					Path:        "skills/review/SKILL.md",
				}},
			},
		},
	})
	if strings.Contains(out.SystemInstruction, "Status: not connected") {
		t.Fatalf("connectionless skills-only app should be usable:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Default skill `notebook-review`") {
		t.Fatalf("connectionless skills-only app should expose skill metadata:\n%s", out.SystemInstruction)
	}
}

func TestAssembleOmitsAppsIndexWhenAllAppsAreDisabled(t *testing.T) {
	out := Assemble(Input{Mode: "chat", Apps: []*app.Definition{{
		ID:      "browser",
		Name:    "Browser",
		Enabled: false,
	}}})
	if strings.Contains(out.SystemInstruction, "## Available Apps") || hasSegment(out.Segments, "apps_index") {
		t.Fatalf("disabled Apps should not leave an empty prompt index:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, `builtin_app_load(app_id="browser")`) || strings.Contains(out.SystemInstruction, `builtin_app_load(app_id="capture")`) {
		t.Fatalf("disabled built-in App left an executable load instruction:\n%s", out.SystemInstruction)
	}
}

func TestAssembleModeLayersAndAllModesShowApps(t *testing.T) {
	apps := []*app.Definition{{ID: "github", Name: "GitHub", Enabled: true, RequiredMode: "work"}}
	chat := Assemble(Input{Mode: "chat", Apps: apps})
	if !strings.Contains(chat.SystemInstruction, "## Available Apps") || !strings.Contains(chat.SystemInstruction, "requires Work") {
		t.Fatalf("chat prompt must expose compact app capability metadata:\n%s", chat.SystemInstruction)
	}
	if !strings.Contains(chat.SystemInstruction, "Canvas is a runtime-provided App") {
		t.Fatalf("chat prompt missing conditional Canvas guidance:\n%s", chat.SystemInstruction)
	}
	work := Assemble(Input{Mode: "work", Apps: apps})
	if !strings.Contains(work.SystemInstruction, "## Work Mode") || !strings.Contains(work.SystemInstruction, "only when it is listed in Available Apps") || !hasSegment(work.Segments, "mode_work") || !hasSegment(work.Segments, "apps_index") {
		t.Fatalf("work prompt missing mode or apps segments: %+v", work.Segments)
	}
	code := Assemble(Input{Mode: "code", Apps: apps})
	if !strings.Contains(code.SystemInstruction, "## Code Mode") || !strings.Contains(code.SystemInstruction, "builtin_command_session") || !hasSegment(code.Segments, "mode_code") || !hasSegment(code.Segments, "apps_index") {
		t.Fatalf("code prompt missing mode or apps segments: %+v", code.Segments)
	}
	if !strings.Contains(code.SystemInstruction, "Code already includes Work") ||
		!strings.Contains(code.SystemInstruction, "load the App directly without requesting another capability") ||
		!strings.Contains(code.SystemInstruction, `request_capability(targetMode="work")`) {
		t.Fatalf("code prompt missing inherited Work capability guidance:\n%s", code.SystemInstruction)
	}
	if strings.Contains(code.SystemInstruction, `builtin_app_load(app_id="terminal")`) {
		t.Fatalf("code prompt still treats terminal as an App:\n%s", code.SystemInstruction)
	}
	if !strings.Contains(code.SystemInstruction, "Treat verification as part of the implementation") ||
		!strings.Contains(code.SystemInstruction, "go test ./...") ||
		!strings.Contains(code.SystemInstruction, "typecheck") ||
		!strings.Contains(code.SystemInstruction, "remaining risk") {
		t.Fatalf("code mode prompt missing compile and verification guidance")
	}
}

func hasSegment(segments []Segment, id string) bool {
	for _, segment := range segments {
		if segment.ID == id {
			return true
		}
	}
	return false
}

func TestLoaderListsAuthoringCapabilitiesAsApps(t *testing.T) {
	home := t.TempDir()
	out, err := NewLoader(home).Prompt(context.Background(), "chat")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.SystemInstruction, "## Available Skills") || strings.Contains(out.SystemInstruction, "`skill-creator`") && !strings.Contains(out.SystemInstruction, "Default skill `skill-creator`") {
		t.Fatalf("loader prompt exposed authoring Skills globally:\n%s", out.SystemInstruction)
	}
	for _, appID := range []string{"skill-authoring", "app-authoring"} {
		if !strings.Contains(out.SystemInstruction, "App `"+appID+"`") {
			t.Fatalf("loader prompt missing authoring App %s:\n%s", appID, out.SystemInstruction)
		}
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

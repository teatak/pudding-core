package prompt

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
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
	// The desktop composer inserts every mention as @<type>/<id>(<name>); each built-in type maps to one exact call.
	for _, want := range []string{"`@<type>/<id>(<name>)`", "`@plugin/<plugin id>`", "`@skill/<skill id>`", "`@skill/<plugin id>/<skill id>`", `builtin_plugin_load(plugin_id="<plugin id>", skill_id="<skill id>")`} {
		if !strings.Contains(out.SystemInstruction, want) {
			t.Fatalf("assembled prompt missing mention guidance %q:\n%s", want, out.SystemInstruction)
		}
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

func TestAssembleIncludesPluginsIndex(t *testing.T) {
	pluginPath := filepath.Join(t.TempDir(), "plugins", "github", plugin.PluginFileName)
	out := Assemble(Input{
		Mode: "work",
		Plugins: []*plugin.Definition{
			{
				ID:             "github",
				Name:           "GitHub",
				Description:    "Access repositories and issues.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "github-issues",
				Path:           pluginPath,
				Endpoints: map[string]plugin.Endpoint{
					"github_rest": {
						Kind:        plugin.EndpointKindREST,
						Description: "GitHub REST API.",
					},
				},
				Skills: []plugin.SkillRef{
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
	if !strings.Contains(out.SystemInstruction, "## Available Plugins") {
		t.Fatalf("assembled prompt missing plugins index:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Plugin `github`") || !strings.Contains(out.SystemInstruction, "requires Work") {
		t.Fatalf("assembled prompt missing compact app metadata:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, `builtin_plugin_load(plugin_id="<plugin id>")`) {
		t.Fatalf("assembled prompt missing explicit app load instruction:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Never use `builtin_skill_read` to load a plugin") {
		t.Fatalf("assembled prompt missing plugin loading boundary:\n%s", out.SystemInstruction)
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

func TestAssembleMarksLoadedPlugins(t *testing.T) {
	out := Assemble(Input{
		Mode:            "work",
		LoadedPluginIDs: []string{"github"},
		Plugins: []*plugin.Definition{{
			ID:             "github",
			Name:           "GitHub",
			Enabled:        true,
			RequiredMode:   "work",
			DefaultSkillID: "github-issues",
			Skills: []plugin.SkillRef{{
				ID:          "github-issues",
				Description: "Inspect GitHub issues.",
			}},
		}},
	})
	if !strings.Contains(out.SystemInstruction, "Status: loaded for this session") {
		t.Fatalf("assembled prompt missing loaded plugin status:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, "Do not call `builtin_plugin_load` again for their default skill") {
		t.Fatalf("assembled prompt missing duplicate load guidance:\n%s", out.SystemInstruction)
	}
	if !strings.Contains(out.SystemInstruction, `builtin_plugin_unload(plugin_id="<loaded plugin id>")`) {
		t.Fatalf("assembled prompt missing plugin unload guidance:\n%s", out.SystemInstruction)
	}
}

func TestAssembleSummarizesUnconnectedPlugin(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Plugins: []*plugin.Definition{
			{
				ID:             "github",
				Name:           "GitHub",
				Description:    "Access repositories and issues.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "github-issues",
				Auth:           &plugin.AuthConfig{Required: true},
				Endpoints: map[string]plugin.Endpoint{
					"github_rest": {Kind: plugin.EndpointKindREST, Description: "GitHub REST API."},
				},
				Skills: []plugin.SkillRef{{
					ID:          "github-issues",
					Description: "Inspect GitHub issues.",
					Path:        "skills/issues/SKILL.md",
				}},
			},
		},
	})
	if !strings.Contains(out.SystemInstruction, "Plugin `github`") || !strings.Contains(out.SystemInstruction, "Status: not connected") {
		t.Fatalf("assembled prompt should summarize unconnected plugin:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "Endpoint `github_rest`") || strings.Contains(out.SystemInstruction, "Skill `github-issues`") {
		t.Fatalf("unconnected app should not expose endpoints or skills:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, "Default skill `github-issues`") {
		t.Fatalf("unconnected app should not advertise its default skill:\n%s", out.SystemInstruction)
	}
}

func TestAssembleShowsConnectedPluginFully(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Plugins: []*plugin.Definition{
			{
				ID:             "github",
				Name:           "GitHub",
				Description:    "Access repositories and issues.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "github-issues",
				Auth:           &plugin.AuthConfig{Required: true},
				Endpoints: map[string]plugin.Endpoint{
					"github_rest": {Kind: plugin.EndpointKindREST, Description: "GitHub REST API."},
				},
				Skills: []plugin.SkillRef{{
					ID:          "github-issues",
					Description: "Inspect GitHub issues.",
					Path:        "skills/issues/SKILL.md",
				}},
			},
		},
		PluginConnections: []*plugin.Connection{
			{
				ID: "github-main", Name: "GitHub · octocat", PluginID: "github",
				Account: &plugin.ConnectionAccount{Login: "octocat"},
				Auth:    plugin.Auth{MethodID: plugin.GitHubAppAuthMethodID, Type: plugin.AuthTypeOAuth2, Variant: plugin.GitHubAppAuthVariant},
			},
			{
				ID: "github-pat", Name: "GitHub PAT", PluginID: "github",
				Auth: plugin.Auth{MethodID: "github-pat", Type: plugin.AuthTypeBearer},
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
		Plugins: []*plugin.Definition{{
			ID: "github", Name: "GitHub", Enabled: true, RequiredMode: "work",
			Auth: &plugin.AuthConfig{Required: true},
		}},
		PluginConnections: []*plugin.Connection{{
			ID: "github-main", PluginID: "github",
			Auth: plugin.Auth{MethodID: "github-oauth", Type: plugin.AuthTypeOAuth2, Variant: plugin.GitHubAppAuthVariant, AccessToken: "legacy-token"},
		}},
	})
	if !strings.Contains(out.SystemInstruction, "Status: not connected") {
		t.Fatalf("legacy GitHub OAuth must require reauthorization:\n%s", out.SystemInstruction)
	}
}

func TestAssembleShowsConnectionlessSkillsOnlyPluginFully(t *testing.T) {
	out := Assemble(Input{
		Mode: "work",
		Plugins: []*plugin.Definition{
			{
				ID:             "notebook-helper",
				Name:           "Notebook Helper",
				Description:    "Guide notebook workflows.",
				Enabled:        true,
				RequiredMode:   "work",
				DefaultSkillID: "notebook-review",
				Auth:           &plugin.AuthConfig{Required: false},
				Skills: []plugin.SkillRef{{
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

func TestAssembleOmitsPluginsIndexWhenAllPluginsAreDisabled(t *testing.T) {
	out := Assemble(Input{Mode: "chat", Plugins: []*plugin.Definition{{
		ID:      "browser",
		Name:    "Browser",
		Enabled: false,
	}}})
	if strings.Contains(out.SystemInstruction, "## Available Plugins") || hasSegment(out.Segments, "plugins_index") {
		t.Fatalf("disabled plugins should not leave an empty prompt index:\n%s", out.SystemInstruction)
	}
	if strings.Contains(out.SystemInstruction, `builtin_plugin_load(plugin_id="browser")`) || strings.Contains(out.SystemInstruction, `builtin_plugin_load(plugin_id="capture")`) {
		t.Fatalf("disabled built-in plugin left an executable load instruction:\n%s", out.SystemInstruction)
	}
}

func TestAssembleModeLayersAndAllModesShowPlugins(t *testing.T) {
	plugins := []*plugin.Definition{{ID: "github", Name: "GitHub", Enabled: true, RequiredMode: "work"}}
	chat := Assemble(Input{Mode: "chat", Plugins: plugins})
	if !strings.Contains(chat.SystemInstruction, "## Available Plugins") || !strings.Contains(chat.SystemInstruction, "requires Work") {
		t.Fatalf("chat prompt must expose compact plugin capability metadata:\n%s", chat.SystemInstruction)
	}
	if !strings.Contains(chat.SystemInstruction, "Widget Authoring is a runtime-provided plugin") {
		t.Fatalf("chat prompt missing conditional Widget guidance:\n%s", chat.SystemInstruction)
	}
	work := Assemble(Input{Mode: "work", Plugins: plugins})
	if !strings.Contains(work.SystemInstruction, "## Work Mode") || !strings.Contains(work.SystemInstruction, "only when it is listed in Available Plugins") || !hasSegment(work.Segments, "mode_work") || !hasSegment(work.Segments, "plugins_index") {
		t.Fatalf("work prompt missing mode or plugins segments: %+v", work.Segments)
	}
	code := Assemble(Input{Mode: "code", Plugins: plugins})
	if !strings.Contains(code.SystemInstruction, "## Code Mode") || !strings.Contains(code.SystemInstruction, "builtin_command_session") || !hasSegment(code.Segments, "mode_code") || !hasSegment(code.Segments, "plugins_index") {
		t.Fatalf("code prompt missing mode or plugins segments: %+v", code.Segments)
	}
	if !strings.Contains(code.SystemInstruction, "Code already includes Work") ||
		!strings.Contains(code.SystemInstruction, "load the plugin directly without requesting another capability") ||
		!strings.Contains(code.SystemInstruction, `request_capability(targetMode="work")`) {
		t.Fatalf("code prompt missing inherited Work capability guidance:\n%s", code.SystemInstruction)
	}
	if strings.Contains(code.SystemInstruction, `builtin_plugin_load(plugin_id="terminal")`) {
		t.Fatalf("code prompt still treats terminal as a plugin:\n%s", code.SystemInstruction)
	}
	if !strings.Contains(code.SystemInstruction, "builtin_file_slice.numberedContent") ||
		!strings.Contains(code.SystemInstruction, "direct line counting is acceptable") ||
		!strings.Contains(code.SystemInstruction, "read a fresh numbered slice") ||
		!strings.Contains(code.SystemInstruction, "recovery.candidateStartLines") ||
		!strings.Contains(code.SystemInstruction, "multiple matches require surrounding context") ||
		!strings.Contains(code.SystemInstruction, "copy truncated diagnostic text") {
		t.Fatalf("code prompt missing line-safe patch workflow:\n%s", code.SystemInstruction)
	}
	if !strings.Contains(code.SystemInstruction, "Treat verification as part of the implementation") ||
		!strings.Contains(code.SystemInstruction, "go test ./...") ||
		!strings.Contains(code.SystemInstruction, "typecheck") ||
		!strings.Contains(code.SystemInstruction, "remaining risk") {
		t.Fatalf("code mode prompt missing compile and verification guidance")
	}
	for _, guidance := range []string{
		"without asking for redundant conversational confirmation",
		"An analysis-only request does not authorize changes",
		"structured local Git operations need no separate approval prompt",
		"Only commit when the user requests a commit",
		"Ask mode still requires approval",
		"Ask mode allows low-risk project reads without a prompt",
		"Writes and command execution still require approval in Ask mode",
		"Never switch tools or execution modes to bypass a denial",
	} {
		if !strings.Contains(code.SystemInstruction, guidance) {
			t.Fatalf("code prompt missing approval guidance: %q", guidance)
		}
	}
	if runtime.GOOS != "windows" {
		for _, guidance := range []string{"Auto trusts code execution within the authorized project sandbox", "do not require approval solely because their behavior cannot be statically proved", "A failed compound command may have completed earlier steps"} {
			if !strings.Contains(code.SystemInstruction, guidance) {
				t.Fatalf("code prompt missing POSIX approval guidance: %q", guidance)
			}
		}
	} else if !strings.Contains(code.SystemInstruction, "Inspect partial effects before retrying") {
		t.Fatal("Windows code prompt missing partial-effect warning")
	}
	if strings.Contains(code.SystemInstruction, "explicit-path approval") || strings.Contains(code.SystemInstruction, "Git-write, outside-Project") {
		t.Fatal("code prompt still claims all local Git writes need approval")
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

func TestAssembleGuidesFocusedResultReuseWithoutSkippingVerification(t *testing.T) {
	out := Assemble(Input{Mode: "code"})
	for _, guidance := range []string{
		"Reuse relevant tool results already in context across turns",
		"Refresh when the source changed",
		"unit=lines",
		"unit=items",
		"not source-file line numbers",
		"do not mask the original exit code",
		"expand the search when completeness is required",
		"Treat verification as part of the implementation",
	} {
		if !strings.Contains(out.SystemInstruction, guidance) {
			t.Fatalf("focused context guidance missing: %s", guidance)
		}
	}
}

func TestLoaderListsAuthoringCapabilitiesAsPlugins(t *testing.T) {
	home := t.TempDir()
	out, err := NewLoader(home).Prompt(context.Background(), "chat")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.SystemInstruction, "## Available Skills") || strings.Contains(out.SystemInstruction, "`skill-creator`") && !strings.Contains(out.SystemInstruction, "Default skill `skill-creator`") {
		t.Fatalf("loader prompt exposed authoring Skills globally:\n%s", out.SystemInstruction)
	}
	for _, pluginID := range []string{"skill-authoring", "plugin-authoring"} {
		if !strings.Contains(out.SystemInstruction, "Plugin `"+pluginID+"`") {
			t.Fatalf("loader prompt missing authoring plugin %s:\n%s", pluginID, out.SystemInstruction)
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

// Package prompt assembles the system instruction from built-in prompt assets
// plus user-owned prompt files under the Pudding home.
package prompt

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/skill"
)

const defaultUserPromptName = "pudding.md"

//go:embed assets/core_system.md
var coreSystemPrompt string

//go:embed assets/mode_chat.md
var chatModePrompt string

//go:embed assets/mode_research.md
var researchModePrompt string

//go:embed assets/mode_workspace.md
var workspaceModePrompt string

type Segment struct {
	ID      string
	Layer   string
	Content string
}

type Input struct {
	UserInstruction string
	Mode            string
	Home            string
	Skills          []skill.Skill
	Apps            []*app.Definition
}

type Output struct {
	SystemInstruction string
	Segments          []Segment
}

type Loader struct {
	home   string
	skills SkillLister
	apps   AppLister
}

func NewLoader(home string) *Loader {
	return &Loader{home: home, skills: skill.NewService(home), apps: app.NewService(home, nil, nil)}
}

type SkillLister interface {
	ListSkills(ctx context.Context) ([]skill.Skill, error)
}

type AppLister interface {
	ListDefinitions(ctx context.Context) ([]*app.Definition, error)
}

func (l *Loader) Prompt(ctx context.Context, mode string) (Output, error) {
	user, err := LoadUserInstruction(l.home)
	if err != nil {
		return Output{}, err
	}
	var skills []skill.Skill
	if l.skills != nil {
		loaded, err := l.skills.ListSkills(ctx)
		if err != nil {
			slog.Warn("prompt: load skills failed", "error", err)
		} else {
			skills = loaded
		}
	}
	var apps []*app.Definition
	if l.apps != nil {
		loaded, err := l.apps.ListDefinitions(ctx)
		if err != nil {
			slog.Warn("prompt: load apps failed", "error", err)
		} else {
			apps = loaded
		}
	}
	return Assemble(Input{UserInstruction: user, Mode: mode, Home: l.home, Skills: skills, Apps: apps}), nil
}

func Assemble(input Input) Output {
	segments := make([]Segment, 0, 4)
	if core := strings.TrimSpace(coreSystemPrompt); core != "" {
		segments = append(segments, Segment{ID: "core_system", Layer: "core", Content: core})
	}
	if mode := strings.TrimSpace(modePrompt(input.Mode)); mode != "" {
		segments = append(segments, Segment{ID: "mode_" + normalizeMode(input.Mode), Layer: "mode", Content: mode})
	}
	if seg := skillsSegment(input.Skills, input.Home); seg != nil {
		segments = append(segments, *seg)
	}
	if seg := appsSegment(input.Apps); seg != nil {
		segments = append(segments, *seg)
	}
	if user := strings.TrimSpace(input.UserInstruction); user != "" {
		segments = append(segments, Segment{
			ID:    "user_system",
			Layer: "user",
			Content: "The following user preferences come from `<home>/pudding.md`. " +
				"They apply only when they do not conflict with higher-priority system rules or tool rules:\n\n" +
				user,
		})
	}

	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if content := strings.TrimSpace(seg.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return Output{SystemInstruction: strings.Join(parts, "\n\n"), Segments: segments}
}

func appsSegment(list []*app.Definition) *Segment {
	if len(list) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("## Installed Apps\n\n")
	b.WriteString("Installed apps provide configured endpoints and app-scoped skills.\n")
	b.WriteString("Endpoint calls use session-granted app connections. Use the listed endpoint names with `builtin_rest_request` or `builtin_graphql_request`; omit `connection` unless the tool reports multiple session-granted connections.\n")
	b.WriteString("Full app SKILL.md bodies are not loaded by default. When an app skill matches, call `builtin_skill_read(app_id=\"<app id>\", skill_id=\"<skill id>\")` once, then follow the returned instructions.\n")
	b.WriteString("Do not proactively load untriggered app skills.\n\n")
	for _, item := range list {
		if item == nil {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = id
		}
		desc := strings.TrimSpace(item.Description)
		if desc != "" {
			fmt.Fprintf(&b, "- App `%s` (%s) — %s\n", id, name, desc)
		} else {
			fmt.Fprintf(&b, "- App `%s` (%s)\n", id, name)
		}
		if len(item.Endpoints) > 0 {
			names := make([]string, 0, len(item.Endpoints))
			for name := range item.Endpoints {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				endpoint := item.Endpoints[name]
				kind := strings.TrimSpace(endpoint.Kind)
				if kind == "" {
					kind = "endpoint"
				}
				if endpoint.Description != "" {
					fmt.Fprintf(&b, "  - Endpoint `%s` (%s) — %s\n", name, kind, endpoint.Description)
				} else {
					fmt.Fprintf(&b, "  - Endpoint `%s` (%s)\n", name, kind)
				}
			}
		}
		for _, sk := range item.Skills {
			id := strings.TrimSpace(sk.ID)
			if id == "" {
				id = strings.TrimSpace(sk.Name)
			}
			path := appSkillRealPath(item, sk)
			desc := strings.TrimSpace(sk.Description)
			if id == "" || desc == "" {
				continue
			}
			if path != "" {
				fmt.Fprintf(&b, "  - Skill `%s` (path: `%s`) — %s\n", id, path, desc)
			} else {
				fmt.Fprintf(&b, "  - Skill `%s` — %s\n", id, desc)
			}
		}
	}
	content := strings.TrimSpace(b.String())
	if content == "" {
		return nil
	}
	return &Segment{ID: "apps_index", Layer: "app", Content: content}
}

func skillsSegment(list []skill.Skill, homeDir string) *Segment {
	if len(list) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("## Available Skills\n\n")
	b.WriteString("Registered global skills are listed below with their trigger descriptions.\n")
	b.WriteString("Full SKILL.md bodies are not loaded by default. Load a skill only when the user's intent clearly matches its description.\n")
	b.WriteString("When a skill matches, call `builtin_skill_read(skill_id=\"<id>\")` once, then follow the returned SKILL.md instructions.\n")
	b.WriteString("Do not proactively load untriggered skills.\n\n")
	for _, item := range list {
		id := strings.TrimSpace(item.ID)
		desc := strings.TrimSpace(item.Description)
		if id == "" || desc == "" {
			continue
		}
		source := strings.TrimSpace(item.Source)
		if source == "" {
			source = "unknown"
		}
		path := skillRealPath(item, homeDir)
		if path != "" {
			fmt.Fprintf(&b, "- `%s` (%s, path: `%s`) — %s\n", id, source, path, desc)
		} else {
			fmt.Fprintf(&b, "- `%s` (%s) — %s\n", id, source, desc)
		}
	}
	content := strings.TrimSpace(b.String())
	if content == "" {
		return nil
	}
	return &Segment{ID: "skills_index", Layer: "skill", Content: content}
}

func skillRealPath(item skill.Skill, homeDir string) string {
	raw := strings.TrimSpace(item.Path)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	switch item.Source {
	case skill.SourceUser:
		if strings.TrimSpace(homeDir) != "" {
			return filepath.Join(homeDir, "skills", filepath.FromSlash(raw))
		}
	}
	return ""
}

func appSkillRealPath(def *app.Definition, ref app.SkillRef) string {
	raw := strings.TrimSpace(ref.Path)
	if raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	appFile := strings.TrimSpace(def.Path)
	if filepath.IsAbs(appFile) {
		return filepath.Join(filepath.Dir(appFile), filepath.FromSlash(raw))
	}
	return ""
}

func modePrompt(mode string) string {
	switch normalizeMode(mode) {
	case "workspace":
		return workspaceModePrompt
	case "research":
		return researchModePrompt
	case "chat":
		fallthrough
	default:
		return chatModePrompt
	}
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "code", "operate", "local", "workspace":
		return "workspace"
	case "work", "research":
		return "research"
	case "chat":
		fallthrough
	default:
		return "chat"
	}
}

func UserInstructionPath(home string) string {
	return filepath.Join(home, defaultUserPromptName)
}

func LoadUserInstruction(home string) (string, error) {
	path := UserInstructionPath(home)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("prompt: read %s: %w", path, err)
	}
	return strings.TrimSpace(string(b)), nil
}

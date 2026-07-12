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
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/skill"
)

const defaultUserPromptName = "pudding.md"

//go:embed assets/core_system.md
var coreSystemPrompt string

//go:embed assets/mode_chat.md
var chatModePrompt string

//go:embed assets/mode_work.md
var workModePrompt string

//go:embed assets/mode_code.md
var codeModePrompt string

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
	AppConnections  []*app.Connection
	RuntimeNow      time.Time
}

type Output struct {
	SystemInstruction string
	Segments          []Segment
}

type Loader struct {
	home        string
	skills      SkillLister
	apps        AppLister
	connections app.ConnectionSource
}

func NewLoader(home string, connections ...app.ConnectionSource) *Loader {
	var source app.ConnectionSource
	if len(connections) > 0 {
		source = connections[0]
	}
	return &Loader{home: home, skills: skill.NewService(home), apps: app.NewService(home, source), connections: source}
}

func NewLoaderWithApps(home string, apps AppLister, connections app.ConnectionSource) *Loader {
	return &Loader{home: home, skills: skill.NewService(home), apps: apps, connections: connections}
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
	var connections []*app.Connection
	if l.connections != nil {
		loaded, err := l.connections.ListAppConnections(ctx)
		if err != nil {
			slog.Warn("prompt: load app connections failed", "error", err)
		} else {
			connections = loaded
		}
	}
	return Assemble(Input{UserInstruction: user, Mode: mode, Home: l.home, Skills: skills, Apps: apps, AppConnections: connections, RuntimeNow: time.Now()}), nil
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
	if seg := appsSegment(input.Apps, input.AppConnections); seg != nil {
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
	segments = append(segments, runtimeSegment(input.RuntimeNow))

	parts := make([]string, 0, len(segments))
	for _, seg := range segments {
		if content := strings.TrimSpace(seg.Content); content != "" {
			parts = append(parts, content)
		}
	}
	return Output{SystemInstruction: strings.Join(parts, "\n\n"), Segments: segments}
}

func runtimeSegment(now time.Time) Segment {
	if now.IsZero() {
		now = time.Now()
	}
	content := fmt.Sprintf("## Runtime Context\n\nCurrent date: %s\nUTC offset: %s\n\nUse this current date for relative dates. Dates from prior turns or prior tool results are historical unless the user explicitly refers to them.", now.Format("2006-01-02"), now.Format("-07:00"))
	return Segment{ID: "runtime_context", Layer: "runtime", Content: content}
}

func appsSegment(list []*app.Definition, connections []*app.Connection) *Segment {
	if len(list) == 0 {
		return nil
	}
	connectionCounts := appConnectionCounts(connections)
	var b strings.Builder
	b.WriteString("## Available Apps\n\n")
	b.WriteString("Enabled apps are listed here as a compact capability index. Their tools are not loaded by default.\n")
	b.WriteString("When an app matches the user's request, first request its required capability if needed, then call `builtin_app_load(app_id=\"<app id>\")`. The call returns the App's default skill instructions and explicitly loads its tools for the session; the tools become available on the next model step. Pass `skill_id` only when a listed non-default App skill clearly matches better.\n")
	b.WriteString("Apps, toolkits, and global skills use separate paths. Never use `builtin_toolkit_load` or `builtin_skill_read` to load an App, including Browser, Terminal, or Canvas.\n")
	b.WriteString("Do not load unrelated apps. Apps marked `not connected` cannot be loaded until a connection is added.\n\n")
	for _, item := range list {
		if item == nil || !item.Enabled {
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
		requiredMode := normalizeMode(item.RequiredMode)
		if strings.TrimSpace(item.RequiredMode) == "" {
			requiredMode = "work"
		}
		modeLabel := strings.ToUpper(requiredMode[:1]) + requiredMode[1:]
		if desc != "" {
			fmt.Fprintf(&b, "- App `%s` (%s), requires %s — %s\n", id, name, modeLabel, desc)
		} else {
			fmt.Fprintf(&b, "- App `%s` (%s), requires %s\n", id, name, modeLabel)
		}
		if !appPromptUsable(item, connectionCounts) {
			fmt.Fprintf(&b, "  - Status: not connected. Add a connection before using this app.\n")
			continue
		}
		skillID, skillDescription := defaultAppSkill(item)
		if skillID != "" {
			fmt.Fprintf(&b, "  - Default skill `%s`", skillID)
			if skillDescription != "" {
				fmt.Fprintf(&b, " — %s", skillDescription)
			}
			b.WriteByte('\n')
		}
	}
	content := strings.TrimSpace(b.String())
	if content == "" {
		return nil
	}
	return &Segment{ID: "apps_index", Layer: "app", Content: content}
}

func appPromptUsable(def *app.Definition, connectionCounts map[string]int) bool {
	if def == nil || !def.Enabled {
		return false
	}
	if !appRequiresConnection(def) {
		return true
	}
	return connectionCounts[strings.TrimSpace(def.ID)] > 0
}

func defaultAppSkill(def *app.Definition) (string, string) {
	if def == nil {
		return "", ""
	}
	id := strings.TrimSpace(def.DefaultSkillID)
	if id == "" && len(def.Skills) > 0 {
		id = strings.TrimSpace(def.Skills[0].ID)
		if id == "" {
			id = strings.TrimSpace(def.Skills[0].Name)
		}
		if id == "" {
			id = strings.TrimSpace(def.Skills[0].Path)
		}
	}
	for _, item := range def.Skills {
		if id == item.ID || id == item.Name || id == item.Path {
			return id, strings.TrimSpace(item.Description)
		}
	}
	return id, ""
}

func appRequiresConnection(def *app.Definition) bool {
	if def == nil {
		return false
	}
	if def.Auth != nil && def.Auth.Required {
		return true
	}
	if def.Connection == nil {
		return false
	}
	for _, field := range def.Connection.Fields {
		if field.Required {
			return true
		}
	}
	return false
}

func appConnectionCounts(connections []*app.Connection) map[string]int {
	out := map[string]int{}
	for _, conn := range connections {
		if conn == nil {
			continue
		}
		if appID := strings.TrimSpace(conn.AppID); appID != "" {
			out[appID]++
		}
	}
	return out
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

func modePrompt(mode string) string {
	switch normalizeMode(mode) {
	case "code":
		return codeModePrompt
	case "work":
		return workModePrompt
	case "chat":
		fallthrough
	default:
		return chatModePrompt
	}
}

func normalizeMode(mode string) string {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "code":
		return "code"
	case "work":
		return "work"
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

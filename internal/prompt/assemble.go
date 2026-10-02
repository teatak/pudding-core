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

	"github.com/teatak/pudding-core/internal/plugin"
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
	UserInstruction   string
	Mode              string
	Home              string
	Skills            []skill.Skill
	Plugins           []*plugin.Definition
	PluginConnections []*plugin.Connection
	LoadedPluginIDs   []string
	RuntimeNow        time.Time
}

type Output struct {
	SystemInstruction string
	Segments          []Segment
}

type Loader struct {
	home        string
	skills      SkillLister
	plugins     PluginLister
	connections plugin.ConnectionSource
}

func NewLoader(home string, connections ...plugin.ConnectionSource) *Loader {
	var source plugin.ConnectionSource
	if len(connections) > 0 {
		source = connections[0]
	}
	return &Loader{home: home, skills: skill.NewService(home), plugins: plugin.NewService(home, source), connections: source}
}

func NewLoaderWithPlugins(home string, plugins PluginLister, connections plugin.ConnectionSource) *Loader {
	return &Loader{home: home, skills: skill.NewService(home), plugins: plugins, connections: connections}
}

type SkillLister interface {
	ListSkills(ctx context.Context) ([]skill.Skill, error)
}

type PluginLister interface {
	ListDefinitions(ctx context.Context) ([]*plugin.Definition, error)
}

func (l *Loader) Prompt(ctx context.Context, mode string) (Output, error) {
	return l.prompt(ctx, mode, nil)
}

// PromptWithLoadedPlugins assembles the prompt with session-owned plugin load state.
func (l *Loader) PromptWithLoadedPlugins(ctx context.Context, mode string, loadedPluginIDs []string) (Output, error) {
	return l.prompt(ctx, mode, loadedPluginIDs)
}

func (l *Loader) prompt(ctx context.Context, mode string, loadedPluginIDs []string) (Output, error) {
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
	var plugins []*plugin.Definition
	if l.plugins != nil {
		loaded, err := l.plugins.ListDefinitions(ctx)
		if err != nil {
			slog.Warn("prompt: load plugins failed", "error", err)
		} else {
			plugins = loaded
		}
	}
	var connections []*plugin.Connection
	if l.connections != nil {
		loaded, err := l.connections.ListPluginConnections(ctx)
		if err != nil {
			slog.Warn("prompt: load plugin connections failed", "error", err)
		} else {
			connections = loaded
		}
	}
	return Assemble(Input{UserInstruction: user, Mode: mode, Home: l.home, Skills: skills, Plugins: plugins, PluginConnections: connections, LoadedPluginIDs: loadedPluginIDs, RuntimeNow: time.Now()}), nil
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
	if seg := pluginsSegment(input.Plugins, input.PluginConnections, input.LoadedPluginIDs); seg != nil {
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

func pluginsSegment(list []*plugin.Definition, connections []*plugin.Connection, loadedPluginIDs []string) *Segment {
	enabled := make([]*plugin.Definition, 0, len(list))
	for _, item := range list {
		if item != nil && item.Enabled && strings.TrimSpace(item.ID) != "" {
			enabled = append(enabled, item)
		}
	}
	if len(enabled) == 0 {
		return nil
	}
	connectionCounts := pluginConnectionCounts(connections)
	loaded := make(map[string]bool, len(loadedPluginIDs))
	for _, id := range loadedPluginIDs {
		if id = strings.TrimSpace(id); id != "" {
			loaded[id] = true
		}
	}
	var b strings.Builder
	b.WriteString("## Available Plugins\n\n")
	b.WriteString("Enabled plugins are listed here as a compact capability index. Their tools are not loaded by default.\n")
	b.WriteString("A plugin's `requires` label is its minimum capability, not an exact mode. Code includes Work, and Work includes Chat. When the current mode already satisfies the minimum, load the plugin directly without requesting another capability.\n")
	b.WriteString("When an unloaded plugin matches the user's request, first request its required capability if needed, then call `builtin_plugin_load(plugin_id=\"<plugin id>\")`. The call returns the plugin's default skill instructions when available and explicitly loads its tools for the session; the tools become available on the next model step. Pass `skill_id` only when a listed non-default plugin skill clearly matches better.\n")
	b.WriteString("Plugins marked `loaded for this session` are already active. Do not call `builtin_plugin_load` again for their default skill; if the current mode is below the plugin's required capability, request that capability instead. Reload only when intentionally selecting a different `skill_id`.\n")
	b.WriteString("Selected plugin skills are references resolved to their current registered instructions in tool results on every model request, including after compaction. Loading another skill of the same plugin supersedes its prior selection. Unloading removes its active instructions as well as its tools.\n")
	b.WriteString("After a plugin is no longer relevant to the current task, call `builtin_plugin_unload(plugin_id=\"<loaded plugin id>\")` to remove its tools from later model steps. This does not uninstall the plugin or delete its connections.\n")
	b.WriteString("Plugins and global skills use separate paths. Never use `builtin_skill_read` to load a plugin, including Browser or Widget Authoring.\n")
	b.WriteString("Do not load unrelated plugins. Plugins marked `not connected` cannot be loaded until a connection is added.\n\n")
	for _, item := range enabled {
		id := strings.TrimSpace(item.ID)
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
			fmt.Fprintf(&b, "- Plugin `%s` (%s), requires %s — %s\n", id, name, modeLabel, desc)
		} else {
			fmt.Fprintf(&b, "- Plugin `%s` (%s), requires %s\n", id, name, modeLabel)
		}
		if !pluginPromptUsable(item, connectionCounts) {
			fmt.Fprintf(&b, "  - Status: not connected. Add a connection before using this plugin.\n")
			continue
		}
		if loaded[id] {
			fmt.Fprintf(&b, "  - Status: loaded for this session.\n")
		}
		for _, connection := range connections {
			if connection == nil || connection.PluginID != id || plugin.ViewConnection(connection).ReauthorizationRequired {
				continue
			}
			connectionID := strings.TrimSpace(connection.ID)
			if connectionID == "" {
				continue
			}
			fmt.Fprintf(&b, "  - Connection `%s`", connectionID)
			if name := strings.TrimSpace(connection.Name); name != "" && name != connectionID {
				fmt.Fprintf(&b, " (%s)", name)
			}
			if methodID := strings.TrimSpace(connection.Auth.MethodID); methodID != "" {
				fmt.Fprintf(&b, ": auth method `%s`", methodID)
			} else if authType := strings.TrimSpace(connection.Auth.Type); authType != "" {
				fmt.Fprintf(&b, ": auth type `%s`", authType)
			}
			if variant := strings.TrimSpace(connection.Auth.Variant); variant != "" {
				fmt.Fprintf(&b, ", variant `%s`", variant)
			}
			if connection.Account != nil {
				if login := strings.TrimSpace(connection.Account.Login); login != "" {
					fmt.Fprintf(&b, ", account `%s`", login)
				}
			}
			b.WriteByte('\n')
		}
		skillID, skillDescription := defaultPluginSkill(item)
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
	return &Segment{ID: "plugins_index", Layer: "plugin", Content: content}
}

func pluginPromptUsable(def *plugin.Definition, connectionCounts map[string]int) bool {
	if def == nil || !def.Enabled {
		return false
	}
	if !pluginRequiresConnection(def) {
		return true
	}
	return connectionCounts[strings.TrimSpace(def.ID)] > 0
}

func defaultPluginSkill(def *plugin.Definition) (string, string) {
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

func pluginRequiresConnection(def *plugin.Definition) bool {
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

func pluginConnectionCounts(connections []*plugin.Connection) map[string]int {
	out := map[string]int{}
	for _, conn := range connections {
		if conn == nil || plugin.ViewConnection(conn).ReauthorizationRequired {
			continue
		}
		if pluginID := strings.TrimSpace(conn.PluginID); pluginID != "" {
			out[pluginID]++
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

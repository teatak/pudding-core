package tool

import (
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

var chatCoreTools = map[string]bool{
	TimeGetCurrent: true, WebSearch: true, WebFetch: true,
	HistorySearch: true, HistoryGetMessage: true, SkillRead: true,
	AttachmentReadImage: true, WeatherGet: true,
}

var codeCoreTools = map[string]bool{
	ProjectInspect: true, ProjectInstructions: true,
	CommandRun: true, CommandSession: true, FileRead: true, PatchApply: true,
}

// CoreDefinitionsForMode returns the fixed, always-available tool surface.
// Optional tools must belong to an App and are added by the engine only after
// that App has been loaded for the session.
func CoreDefinitionsForMode(mode store.AgentMode, defs []provider.ToolDef) []provider.ToolDef {
	mode = normalizedMode(mode)
	byName := make(map[string]provider.ToolDef, len(defs))
	dynamicChatNames := make([]string, 0, 1)
	for _, def := range defs {
		name := strings.TrimSpace(def.Name)
		if name == "" {
			continue
		}
		byName[name] = def
		if name == RequestUserInput {
			dynamicChatNames = append(dynamicChatNames, name)
		}
	}

	out := []provider.ToolDef{RequestCapabilityDefinition()}
	seen := map[string]bool{RequestCapability: true}
	appendNames := func(names []string) {
		sort.Strings(names)
		for _, name := range names {
			def, ok := byName[name]
			if !ok || seen[name] || !ToolDefAllowedForMode(mode, def) {
				continue
			}
			seen[name] = true
			out = append(out, def)
		}
	}
	appendNames(mapKeys(chatCoreTools))
	appendNames(dynamicChatNames)
	if mode == store.ModeCode {
		appendNames(mapKeys(codeCoreTools))
	}
	return out
}

func IsCoreTool(name string) bool {
	return name == RequestCapability || name == RequestUserInput || chatCoreTools[name] || codeCoreTools[name]
}

func mapKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for name := range values {
		out = append(out, name)
	}
	return out
}

func normalizedMode(mode store.AgentMode) store.AgentMode {
	mode = store.NormalizeAgentMode(mode)
	if !store.ValidAgentMode(mode) {
		return store.ModeChat
	}
	return mode
}

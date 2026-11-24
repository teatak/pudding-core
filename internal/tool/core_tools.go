package tool

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

var chatCoreTools = map[string]bool{
	AssistantProject:      true,
	AssistantTaskDispatch: true,
	AssistantTaskList:     true,
	AssistantTaskSend:     true,
	AssistantRename:       true,
	AssistantMemory:       true,
	AssistantUserName:     true,
	SetNextPrompt:         true,
	SetTurnTopic:          true,
	ScheduledTask:         true,
	TimeGetCurrent:        true,
	HistorySearch:         true, HistoryGetMessage: true, SkillRead: true,
	MediaRead: true, WeatherGet: true,
}

var codeCoreTools = map[string]bool{
	CommandRun: true, CommandSession: true,
	FileList: true, FileRead: true, AttachmentExport: true, TempWorkspace: true,
	FileStat: true, FileSearch: true, FileSlice: true,
	FileWrite: true, FilePatch: true, FileDelete: true,
	FileMove: true, FileCopy: true,
	GitStatus: true, GitDiff: true, GitLog: true,
	GitStage: true, GitUnstage: true, GitCommit: true,
	CodeSymbols: true, CodeDefinition: true, CodeReferences: true,
	CodeDiagnostics: true, CodeRename: true,
}

// CoreDefinitionsForMode returns the fixed, always-available tool surface.
// Optional tools must belong to a plugin and are added by the engine only after
// that plugin has been loaded for the session.
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
			if name == MediaRead && mode != store.ModeCode {
				def = attachmentOnlyMediaReadDefinition(def)
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

func attachmentOnlyMediaReadDefinition(def provider.ToolDef) provider.ToolDef {
	def.Description = "Inspect a saved session image or audio attachment (up to 20 MiB). A screenshot shows its saved state, at most 2048 px per side. Reuse references already in context. Models without the input capability receive metadata only."
	def.InputSchema = json.RawMessage(`{"type":"object","properties":{"source":{"type":"string","enum":["attachment"]},"attachmentKey":{"type":"string","description":"Exact session attachment key; prefer over url."},"purpose":{"type":"string","enum":["reference","observation"],"description":"Images default to reference for comparison. Observation joins the recent screenshot budget. Both follow compaction."},"url":{"type":"string","description":"A session attachment URL fallback."}},"required":["source"],"additionalProperties":false}`)
	return def
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

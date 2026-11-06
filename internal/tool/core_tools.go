package tool

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

var chatCoreTools = map[string]bool{
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
	def.Description = "Route one supported raster image or audio attachment up to 20 MiB from the current session to the model. Use source=attachment and attachmentKey to inspect an older screenshot omitted from model context; this reloads the saved image, not the current screen. Image reads use a model copy up to 2048 pixels per side. purpose=reference (default) keeps a comparison image outside screenshot eviction; purpose=observation marks a temporary screenshot read and counts toward the recent screenshot budget. Reuse reference images already in context instead of rereading them. References still follow normal conversation compaction. Media bytes are visible only when the current model supports that input type; otherwise only metadata is available. This tool does not transcribe audio."
	def.InputSchema = json.RawMessage(`{"type":"object","properties":{"source":{"type":"string","enum":["attachment"],"description":"Use an existing attachment from the current session."},"attachmentKey":{"type":"string","description":"The exact session attachment key returned by an upload or capture tool. Prefer this field."},"purpose":{"type":"string","enum":["reference","observation"],"description":"For images only. reference (default): retain for ongoing comparison outside screenshot eviction. observation: temporary screenshot read, subject to the recent screenshot budget. Both follow normal conversation compaction."},"url":{"type":"string","description":"A session attachment URL fallback."}},"required":["source"],"additionalProperties":false}`)
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

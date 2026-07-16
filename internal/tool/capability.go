package tool

import (
	"encoding/json"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const RequestCapability = "request_capability"

type CapabilityRequest struct {
	TargetMode       store.AgentMode `json:"targetMode"`
	Reason           string          `json:"reason"`
	ProjectDirs      []string        `json:"projectDirs,omitempty"`
	NeedsProjectDir  bool            `json:"needsProjectDir,omitempty"`
	SuggestedDirName string          `json:"suggestedDirName,omitempty"`
	Risk             string          `json:"risk,omitempty"`
}

func RequestCapabilityDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        RequestCapability,
		Description: "Request Work capability for browser and connected-service operations, or Code capability for files, commands, tests, and Git. Code can use a session-isolated temporary workspace without a Project. When already in Code mode, do not call this tool unless a specific additional absolute project directory is needed. The user may approve only for the current turn or remember it for the session.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"targetMode":{"type":"string","enum":["work","code"],"description":"Use work for browser or connected-service operations. Use code for files, commands, tests, or Git."},"reason":{"type":"string","description":"Why the higher capability is needed."},"projectDirs":{"type":"array","items":{"type":"string"},"description":"Code mode only: optional absolute project directories requested. Use attached local folder paths exactly when the user provided them. Leave empty to use Pudding's session-isolated temporary workspace. When already in Code mode, call only with specific additional directories; do not call with an empty list to confirm existing access."},"needsProjectDir":{"type":"boolean","description":"Code mode only: set true to suggest choosing a project directory when no concrete directory is known; the user may still continue with the temporary workspace."},"suggestedDirName":{"type":"string","description":"Code mode only: optional short suggested folder name shown by the directory picker."},"risk":{"type":"string","description":"Potential side effects or privacy/local file risks."}},"required":["targetMode","reason"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func DefinitionsForMode(mode store.AgentMode, defs []provider.ToolDef) []provider.ToolDef {
	return DefinitionsForTurn(mode, defs, nil)
}

func ToolDefAllowedForMode(mode store.AgentMode, def provider.ToolDef) bool {
	mode = store.NormalizeAgentMode(mode)
	if !store.ValidAgentMode(mode) {
		mode = store.ModeChat
	}
	if def.Name == RequestCapability {
		return true
	}
	required := store.NormalizeAgentMode(def.Capability)
	if required == "" {
		required = store.ModeCode
	}
	return store.AgentModeRank(mode) >= store.AgentModeRank(required)
}

func RequiredModeForName(name string) store.AgentMode {
	if name == RequestCapability || name == ToolkitLoad || name == AppLoad {
		return store.ModeChat
	}
	if strings.HasPrefix(name, appMCPToolPrefix) {
		return store.ModeWork
	}
	if strings.HasPrefix(name, "canvas_") || name == "collect_user_input" {
		return store.ModeChat
	}
	for _, def := range BuiltinDefinitions() {
		if def.Name == name {
			mode := store.NormalizeAgentMode(def.Capability)
			if mode != "" {
				return mode
			}
			break
		}
	}
	return store.ModeCode
}

func NameAllowedForMode(mode store.AgentMode, name string) bool {
	mode = store.NormalizeAgentMode(mode)
	if !store.ValidAgentMode(mode) {
		mode = store.ModeChat
	}
	if name == RequestCapability || name == ToolkitLoad || name == AppLoad {
		return true
	}
	return store.AgentModeRank(mode) >= store.AgentModeRank(RequiredModeForName(name))
}

func AllowedForMode(mode store.AgentMode, defs []provider.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return ToolDefAllowedForMode(mode, def)
		}
	}
	return false
}

func HasDefinition(defs []provider.ToolDef, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

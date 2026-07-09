package tool

import (
	"encoding/json"

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
		Description: "Request project capability, or request additional project directories when project capability is already active. The user may approve only for the current turn or remember it for the session.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"targetMode":{"type":"string","enum":["project"],"description":"Project capability is required to inspect or change local project files."},"reason":{"type":"string","description":"Why project capability or additional project directories are needed."},"projectDirs":{"type":"array","items":{"type":"string"},"description":"Absolute project directories requested. Use attached local folder paths exactly when the user provided them. Leave empty only if the user must choose."},"needsProjectDir":{"type":"boolean","description":"Set true when project capability is needed but no concrete directory is known."},"suggestedDirName":{"type":"string","description":"Optional short suggested folder name when asking the user to choose a project directory."},"risk":{"type":"string","description":"Potential side effects or privacy/local file risks."}},"required":["targetMode","reason"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func DefinitionsForMode(mode store.AgentMode, defs []provider.ToolDef) []provider.ToolDef {
	mode = store.NormalizeAgentMode(mode)
	if !store.ValidAgentMode(mode) {
		mode = store.ModeChat
	}
	out := make([]provider.ToolDef, 0, len(defs)+1)
	out = append(out, RequestCapabilityDefinition())
	for _, def := range defs {
		if ToolDefAllowedForMode(mode, def) {
			out = append(out, def)
		}
	}
	return out
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
		required = store.ModeProject
	}
	return store.AgentModeRank(mode) >= store.AgentModeRank(required)
}

func RequiredModeForName(name string) store.AgentMode {
	if name == RequestCapability {
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
	return store.ModeProject
}

func NameAllowedForMode(mode store.AgentMode, name string) bool {
	mode = store.NormalizeAgentMode(mode)
	if !store.ValidAgentMode(mode) {
		mode = store.ModeChat
	}
	if name == RequestCapability {
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

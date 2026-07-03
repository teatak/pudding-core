package tool

import (
	"encoding/json"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const RequestCapability = "request_capability"

type CapabilityRequest struct {
	TargetMode        store.AgentMode `json:"targetMode"`
	Reason            string          `json:"reason"`
	WorkspaceDirs     []string        `json:"workspaceDirs,omitempty"`
	NeedsWorkspaceDir bool            `json:"needsWorkspaceDir,omitempty"`
	SuggestedDirName  string          `json:"suggestedDirName,omitempty"`
	Risk              string          `json:"risk,omitempty"`
}

func RequestCapabilityDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        RequestCapability,
		Description: "Request workspace capability, or request additional workspace directories when workspace capability is already active.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"targetMode":{"type":"string","enum":["workspace"],"description":"Workspace capability is required to inspect or change local files."},"reason":{"type":"string","description":"Why workspace capability or additional workspace directories are needed."},"workspaceDirs":{"type":"array","items":{"type":"string"},"description":"Absolute workspace directories requested. Leave empty if the user must choose."},"needsWorkspaceDir":{"type":"boolean","description":"Set true when workspace capability is needed but no concrete directory is known."},"suggestedDirName":{"type":"string","description":"Optional short suggested folder name when asking the user to choose a workspace directory."},"risk":{"type":"string","description":"Potential side effects or privacy/local file risks."}},"required":["targetMode","reason"],"additionalProperties":false}`),
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
		required = store.ModeWorkspace
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
	return store.ModeWorkspace
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

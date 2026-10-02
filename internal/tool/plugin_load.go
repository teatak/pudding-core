package tool

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const PluginLoad = "builtin_plugin_load"

type PluginLoadRequest struct {
	PluginID string `json:"plugin_id"`
	SkillID  string `json:"skill_id,omitempty"`
}

func PluginLoadDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        PluginLoad,
		Description: "Explicitly load one enabled plugin for this session and select its default or specified skill. Use only plugin ids from Available Plugins. The selected skill is a registered reference: its current body is supplied in the tool result on each model request, including after compaction. Do not reload merely to refresh instructions; skill_id selects a different skill of this plugin. The plugin's tools become available on the next model step when its required capability and runtime are available.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"plugin_id":{"type":"string","description":"Plugin id from Available Plugins."},"skill_id":{"type":"string","description":"Optional plugin skill id. Defaults to the plugin's default skill."}},"required":["plugin_id"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func DecodePluginLoadRequest(raw json.RawMessage) (PluginLoadRequest, error) {
	var request PluginLoadRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return request, errors.New("plugin load arguments must be a JSON object")
	}
	request.PluginID = strings.TrimSpace(request.PluginID)
	request.SkillID = strings.TrimSpace(request.SkillID)
	if request.PluginID == "" {
		return request, errors.New("plugin_id is required")
	}
	return request, nil
}

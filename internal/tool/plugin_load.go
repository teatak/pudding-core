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
	PluginID     string `json:"plugin_id"`
	SkillID      string `json:"skill_id,omitempty"`
	ConnectionID string `json:"connection_id,omitempty"`
}

func PluginLoadDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        PluginLoad,
		Description: "Load one enabled plugin for this session and select its default or specified skill. Use plugin ids from Available Plugins. If the current capability is insufficient, this call requests authorization and continues loading after approval; do not request capability separately before loading. Existing connection bindings are reused; a sole usable connection is automatic. When an account must be selected or switched, call this tool with connection_id from the returned connections, based on the user's intent; ask in chat only if ambiguous. This also works for already-loaded plugins. The selected skill is a registered reference whose current body is supplied on each model request. Do not reload merely to refresh instructions. Tools become available on the next model step when the required capability and runtime are available.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"plugin_id":{"type":"string","description":"Plugin id from Available Plugins."},"skill_id":{"type":"string","description":"Optional plugin skill id. Defaults to the plugin's default skill."},"connection_id":{"type":"string","description":"Optional saved connection ID from returned connections. Selects and remembers that account for this chat. Omit to reuse the binding or automatically use the sole usable connection."}},"required":["plugin_id"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func DecodePluginLoadRequest(raw json.RawMessage) (PluginLoadRequest, error) {
	var request PluginLoadRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return request, errors.New("plugin load arguments must be a JSON object")
	}
	request.PluginID = store.CurrentPluginID(request.PluginID)
	request.SkillID = store.CurrentPluginSkillID(request.PluginID, request.SkillID)
	request.ConnectionID = strings.TrimSpace(request.ConnectionID)
	if request.PluginID == "" {
		return request, errors.New("plugin_id is required")
	}
	return request, nil
}

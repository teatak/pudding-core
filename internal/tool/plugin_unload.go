package tool

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const PluginUnload = "builtin_plugin_unload"

type PluginUnloadRequest struct {
	PluginID string `json:"plugin_id"`
}

func PluginUnloadDefinition(loadedPluginIDs []string) provider.ToolDef {
	pluginID := map[string]any{
		"type":        "string",
		"description": "Plugin id currently loaded for this session.",
	}
	if ids := store.NormalizePluginIDs(loadedPluginIDs); len(ids) > 0 {
		pluginID["enum"] = ids
	}
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"plugin_id": pluginID,
		},
		"required":             []string{"plugin_id"},
		"additionalProperties": false,
	})
	return provider.ToolDef{
		Name:        PluginUnload,
		Description: "Unload one currently loaded plugin from this session when it is no longer relevant. This only removes its tools from future model steps; it does not uninstall the plugin or delete connections.",
		InputSchema: schema,
		Capability:  store.ModeChat,
	}
}

func DecodePluginUnloadRequest(raw json.RawMessage) (PluginUnloadRequest, error) {
	var request PluginUnloadRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return request, errors.New("plugin unload arguments must be a JSON object")
	}
	request.PluginID = strings.TrimSpace(request.PluginID)
	if request.PluginID == "" {
		return request, errors.New("plugin_id is required")
	}
	return request, nil
}

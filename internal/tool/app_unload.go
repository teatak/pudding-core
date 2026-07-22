package tool

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const AppUnload = "builtin_app_unload"

type AppUnloadRequest struct {
	AppID string `json:"app_id"`
}

func AppUnloadDefinition(loadedAppIDs []string) provider.ToolDef {
	appID := map[string]any{
		"type":        "string",
		"description": "App id currently loaded for this session.",
	}
	if ids := store.NormalizeAppIDs(loadedAppIDs); len(ids) > 0 {
		appID["enum"] = ids
	}
	schema, _ := json.Marshal(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"app_id": appID,
		},
		"required":             []string{"app_id"},
		"additionalProperties": false,
	})
	return provider.ToolDef{
		Name:        AppUnload,
		Description: "Unload one currently loaded App from this session when it is no longer relevant. This only removes its tools from future model steps; it does not uninstall the App or delete connections.",
		InputSchema: schema,
		Capability:  store.ModeChat,
	}
}

func DecodeAppUnloadRequest(raw json.RawMessage) (AppUnloadRequest, error) {
	var request AppUnloadRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return request, errors.New("app unload arguments must be a JSON object")
	}
	request.AppID = strings.TrimSpace(request.AppID)
	if request.AppID == "" {
		return request, errors.New("app_id is required")
	}
	return request, nil
}

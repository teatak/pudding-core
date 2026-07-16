package tool

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const AppLoad = "builtin_app_load"

type AppLoadRequest struct {
	AppID   string `json:"app_id"`
	SkillID string `json:"skill_id,omitempty"`
}

func AppLoadDefinition() provider.ToolDef {
	return provider.ToolDef{
		Name:        AppLoad,
		Description: "Explicitly load one enabled App for this session and return its selected or default skill instructions when available. Use only app ids from Available Apps. The App's tools become available on the next model step when its required capability and runtime are available.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"app_id":{"type":"string","description":"App id from Available Apps."},"skill_id":{"type":"string","description":"Optional App skill id. Defaults to the App's default skill."}},"required":["app_id"],"additionalProperties":false}`),
		Capability:  store.ModeChat,
	}
}

func DecodeAppLoadRequest(raw json.RawMessage) (AppLoadRequest, error) {
	var request AppLoadRequest
	if len(raw) == 0 || json.Unmarshal(raw, &request) != nil {
		return request, errors.New("app load arguments must be a JSON object")
	}
	request.AppID = strings.TrimSpace(request.AppID)
	request.SkillID = strings.TrimSpace(request.SkillID)
	if request.AppID == "" {
		return request, errors.New("app_id is required")
	}
	return request, nil
}

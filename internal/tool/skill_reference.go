package tool

import (
	"encoding/json"
	"strings"
)

// SkillReference identifies registered instructions, never an arbitrary file.
// Only the reference is canonical; its body is resolved for each model request.
type SkillReference struct {
	Kind     string `json:"kind"`
	PluginID string `json:"pluginID,omitempty"`
	SkillID  string `json:"skillID"`
}

const (
	PluginSkillReference = "plugin_skill"
	GlobalSkillReference = "skill"
)

// Canonical messages are never rewritten. Results stored before plugins were
// renamed from Apps keep builtin_app_load, app_skill references, appID fields
// and the earlier built-in IDs; they are read under the current names.
const (
	legacyPluginLoad           = "builtin_app_load"
	legacyPluginSkillReference = "app_skill"
)

var (
	legacyPluginIDs      = map[string]string{"app-authoring": "plugin-authoring", "canvas": "widget-authoring"}
	legacyPluginSkillIDs = map[string]string{"app-creator": "plugin-creator"}
)

// IsPluginLoad reports a plugin load result, including one stored under its earlier name.
func IsPluginLoad(name string) bool { return name == PluginLoad || name == legacyPluginLoad }

// CurrentToolName reads a tool call stored before plugins and widgets were
// renamed under the name the same tool has now, so a model request keeps the
// call instead of dropping it as unavailable.
func CurrentToolName(name string) string {
	switch {
	case name == legacyPluginLoad:
		return PluginLoad
	case name == "builtin_app_unload":
		return PluginUnload
	case name == "builtin_app_save":
		return PluginSave
	case strings.HasPrefix(name, "app_mcp__"):
		return pluginMCPToolPrefix + strings.TrimPrefix(name, "app_mcp__")
	case strings.HasPrefix(name, "canvas_"):
		return "widget_" + strings.TrimPrefix(name, "canvas_")
	}
	return name
}

func legacyPluginReference(pluginID, skillID string) SkillReference {
	pluginID, skillID = strings.TrimSpace(pluginID), strings.TrimSpace(skillID)
	if renamed, ok := legacyPluginIDs[pluginID]; ok {
		pluginID = renamed
	}
	if renamed, ok := legacyPluginSkillIDs[skillID]; ok {
		skillID = renamed
	}
	return SkillReference{Kind: PluginSkillReference, PluginID: pluginID, SkillID: skillID}
}

func (r SkillReference) Key() string {
	if r.Kind == PluginSkillReference {
		// Loading another skill of a plugin selects it instead of its previous one.
		return r.Kind + ":" + r.PluginID
	}
	return r.Kind + ":" + r.SkillID
}

// ReferencedSkill also projects pre-reference canonical results. Old rows are
// never rewritten and their saved body is never used as a fallback.
func ReferencedSkill(name string, ok bool, content string) (SkillReference, bool) {
	if !ok || (!IsPluginLoad(name) && name != SkillRead) {
		return SkillReference{}, false
	}
	var payload struct {
		Reference *struct {
			Kind     string `json:"kind"`
			PluginID string `json:"pluginID"`
			AppID    string `json:"appID"`
			SkillID  string `json:"skillID"`
		} `json:"reference"`
		AppID   string  `json:"appID"`
		SkillID string  `json:"skillID"`
		ID      string  `json:"id"`
		Content *string `json:"content"`
	}
	if json.Unmarshal([]byte(content), &payload) != nil {
		return SkillReference{}, false
	}
	if ref := payload.Reference; ref != nil {
		if ref.Kind == legacyPluginSkillReference {
			return legacyPluginReference(ref.AppID, ref.SkillID), true
		}
		return SkillReference{Kind: ref.Kind, PluginID: ref.PluginID, SkillID: ref.SkillID}, true
	}
	if payload.Content == nil {
		return SkillReference{}, false
	}
	switch name {
	case legacyPluginLoad:
		return legacyPluginReference(payload.AppID, payload.SkillID), true
	case SkillRead:
		return SkillReference{Kind: GlobalSkillReference, SkillID: strings.TrimSpace(payload.ID)}, true
	}
	return SkillReference{}, false
}

// SkillReferencePayload starts from historical status/identity metadata, but
// replaces instruction data exclusively with the caller's current projection.
func SkillReferencePayload(content string, ref SkillReference, fields map[string]any) string {
	var payload map[string]any
	_ = json.Unmarshal([]byte(content), &payload)
	if payload == nil {
		payload = make(map[string]any)
	}
	delete(payload, "content")
	delete(payload, "instructionStatus")
	delete(payload, "instructionError")
	payload["reference"] = ref
	for key, value := range fields {
		payload[key] = value
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func SkillReferenceOnly(name string, ok bool, content string) string {
	if ref, found := ReferencedSkill(name, ok, content); found {
		return SkillReferencePayload(content, ref, nil)
	}
	return content
}

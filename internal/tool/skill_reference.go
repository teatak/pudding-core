package tool

import (
	"encoding/json"
	"strings"
)

// SkillReference identifies registered instructions, never an arbitrary file.
// Only the reference is canonical; its body is resolved for each model request.
type SkillReference struct {
	Kind    string `json:"kind"`
	AppID   string `json:"appID,omitempty"`
	SkillID string `json:"skillID"`
}

const (
	AppSkillReference    = "app_skill"
	GlobalSkillReference = "skill"
)

func (r SkillReference) Key() string {
	if r.Kind == AppSkillReference {
		// Loading another skill of an App selects it instead of its previous one.
		return r.Kind + ":" + r.AppID
	}
	return r.Kind + ":" + r.SkillID
}

// ReferencedSkill also projects pre-reference canonical results. Old rows are
// never rewritten and their saved body is never used as a fallback.
func ReferencedSkill(name string, ok bool, content string) (SkillReference, bool) {
	if !ok || (name != AppLoad && name != SkillRead) {
		return SkillReference{}, false
	}
	var payload struct {
		Reference *SkillReference `json:"reference"`
		AppID     string          `json:"appID"`
		SkillID   string          `json:"skillID"`
		ID        string          `json:"id"`
		Content   *string         `json:"content"`
	}
	if json.Unmarshal([]byte(content), &payload) != nil {
		return SkillReference{}, false
	}
	if payload.Reference != nil {
		return *payload.Reference, true
	}
	if payload.Content == nil {
		return SkillReference{}, false
	}
	if name == AppLoad {
		return SkillReference{Kind: AppSkillReference, AppID: strings.TrimSpace(payload.AppID), SkillID: strings.TrimSpace(payload.SkillID)}, true
	}
	return SkillReference{Kind: GlobalSkillReference, SkillID: strings.TrimSpace(payload.ID)}, true
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

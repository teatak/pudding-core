package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/skill"
)

func (r *BuiltinRunner) skillRead(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		SkillID string `json:"skill_id"`
		AppID   string `json:"app_id"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			out.Ok = false
			out.Content = "invalid arguments: " + err.Error()
			return out
		}
	}
	id := strings.TrimSpace(args.SkillID)
	if id == "" {
		out.Ok = false
		out.Content = `{"ok":false,"reason":"skill_id_required","hint":"Pass skill_id from the Available Skills index."}`
		return out
	}
	appID := strings.TrimSpace(args.AppID)
	if appID != "" {
		return r.appSkillRead(ctx, out, appID, id)
	}
	if r.skillReader == nil {
		out.Ok = false
		out.Content = `{"ok":false,"reason":"skill_reader_unavailable"}`
		return out
	}
	doc, err := r.skillReader.ReadSkill(ctx, id)
	if err != nil {
		reason := "read_failed"
		switch {
		case errors.Is(err, skill.ErrInvalidID):
			reason = "invalid_skill_id"
		case errors.Is(err, skill.ErrNotFound):
			reason = "skill_not_found"
		}
		payload, _ := json.Marshal(map[string]any{"ok": false, "reason": reason, "detail": err.Error()})
		out.Ok = false
		out.Content = string(payload)
		return out
	}
	payload, err := json.Marshal(map[string]any{
		"ok":          true,
		"id":          doc.ID,
		"name":        doc.Name,
		"description": doc.Description,
		"scope":       doc.Scope,
		"source":      doc.Source,
		"path":        doc.Path,
		"content":     doc.Content,
	})
	if err != nil {
		out.Ok = false
		out.Content = err.Error()
		return out
	}
	out.Ok = true
	out.Content = string(payload)
	out.SummaryKind = SummaryReadChars
	out.SummaryCount = len(doc.Content)
	return out
}

func (r *BuiltinRunner) appSkillRead(ctx context.Context, out Result, appID, skillID string) Result {
	if r.appSkills == nil {
		out.Ok = false
		out.Content = `{"ok":false,"reason":"app_skill_reader_unavailable"}`
		return out
	}
	doc, err := r.appSkills.ReadSkill(ctx, appID, skillID)
	if err != nil {
		reason := "read_failed"
		switch {
		case errors.Is(err, app.ErrInvalidID):
			reason = "invalid_app_id"
		case errors.Is(err, app.ErrNotFound):
			reason = "app_skill_not_found"
		case errors.Is(err, app.ErrDisabled):
			reason = "app_disabled"
		}
		payload, _ := json.Marshal(map[string]any{"ok": false, "reason": reason, "detail": err.Error()})
		out.Ok = false
		out.Content = string(payload)
		return out
	}
	id := strings.TrimSpace(doc.ID)
	if id == "" {
		id = strings.TrimSpace(doc.Path)
	}
	payload, err := json.Marshal(map[string]any{
		"ok":          true,
		"appID":       appID,
		"id":          id,
		"name":        doc.Name,
		"description": doc.Description,
		"scope":       "app",
		"source":      "app",
		"path":        doc.Path,
		"content":     doc.Content,
	})
	if err != nil {
		out.Ok = false
		out.Content = err.Error()
		return out
	}
	out.Ok = true
	out.Content = string(payload)
	out.SummaryKind = SummaryReadChars
	out.SummaryCount = len(doc.Content)
	return out
}

package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/skill"
)

func (r *BuiltinRunner) ApplySkillDraft(ctx context.Context, id string) error {
	if r.skillDrafts == nil {
		return errors.New("skill draft service is not configured")
	}
	return r.skillDrafts.ApplyDraft(ctx, id)
}

func (r *BuiltinRunner) skillValidate(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	id, err := draftIDFromArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if r.skillDrafts == nil {
		return toolJSONError(out, "skill_drafts_unavailable", "skill draft service is not configured")
	}
	validation, err := r.skillDrafts.ValidateDraft(ctx, id)
	if err != nil {
		return skillDraftError(out, err)
	}
	out.Ok = validation != nil && validation.OK
	out.Content = jsonString(map[string]any{"ok": out.Ok, "draft_id": id, "validation": validation})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func (r *BuiltinRunner) skillSubmit(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	id, err := draftIDFromArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if r.skillDrafts == nil {
		return toolJSONError(out, "skill_drafts_unavailable", "skill draft service is not configured")
	}
	detail, err := r.skillDrafts.DraftDetail(ctx, id)
	if err != nil {
		return skillDraftError(out, err)
	}
	if !detail.Draft.Validation.OK {
		out.Ok = false
		out.Content = jsonString(map[string]any{
			"ok":         false,
			"reason":     "validation_failed",
			"draft_id":   id,
			"validation": detail.Draft.Validation,
		})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 4
		return out
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{
		"ok":        true,
		"status":    "pending_user_review",
		"draft":     detail.Draft,
		"fileCount": len(detail.Files),
	})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(detail.Files)
	return out
}

func draftIDFromArgs(raw json.RawMessage) (string, error) {
	var args struct {
		DraftID string `json:"draft_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	id := strings.TrimSpace(args.DraftID)
	if id == "" {
		return "", errors.New("draft_id is required")
	}
	return id, nil
}

func skillDraftError(out Result, err error) Result {
	reason := "skill_draft_failed"
	switch {
	case errors.Is(err, skill.ErrInvalidID):
		reason = "invalid_draft_id"
	case errors.Is(err, skill.ErrNotFound):
		reason = "skill_draft_not_found"
	case errors.Is(err, skill.ErrInvalidDraft):
		reason = "skill_draft_invalid"
	}
	return toolJSONError(out, reason, err.Error())
}

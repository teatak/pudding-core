package tool

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/teatak/pudding-core/internal/skill"
)

func (r *BuiltinRunner) skillValidate(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	id, err := skillIDFromArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if r.skillValidator == nil {
		return toolJSONError(out, "skill_validator_unavailable", "skill validator is not configured")
	}
	validation, err := r.skillValidator.ValidateSkill(ctx, id)
	if err != nil {
		return skillValidationError(out, err)
	}
	out.Ok = validation != nil && validation.OK
	out.Content = jsonString(map[string]any{"ok": out.Ok, "skill_id": id, "validation": validation})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func skillIDFromArgs(raw json.RawMessage) (string, error) {
	var args struct {
		SkillID string `json:"skill_id"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &args); err != nil {
			return "", err
		}
	}
	id := strings.TrimSpace(args.SkillID)
	if id == "" {
		return "", errors.New("skill_id is required")
	}
	return id, nil
}

func skillValidationError(out Result, err error) Result {
	reason := "skill_validation_failed"
	if errors.Is(err, skill.ErrInvalidID) {
		reason = "invalid_skill_id"
	}
	return toolJSONError(out, reason, err.Error())
}

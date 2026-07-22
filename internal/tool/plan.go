package tool

import (
	"strings"
	"unicode/utf8"
)

const (
	planMinSteps       = 2
	planMaxSteps       = 12
	planMaxDescription = 200
)

type planStep struct {
	Step   string `json:"step"`
	Status string `json:"status"`
}

type planUpdateArgs struct {
	Plan []planStep `json:"plan"`
}

func planUpdate(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args planUpdateArgs
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if len(args.Plan) < planMinSteps || len(args.Plan) > planMaxSteps {
		return toolJSONError(out, "invalid_plan_size", "plan must contain 2 to 12 steps")
	}

	currentStep := 0
	seenInProgress := false
	seenPending := false
	allCompleted := true
	for index := range args.Plan {
		step := &args.Plan[index]
		step.Step = strings.TrimSpace(step.Step)
		if step.Step == "" || utf8.RuneCountInString(step.Step) > planMaxDescription {
			return toolJSONError(out, "invalid_step", "each step must contain 1 to 200 characters")
		}
		switch step.Status {
		case "completed":
			if seenInProgress || seenPending {
				return toolJSONError(out, "invalid_status_order", "completed steps must come before the current and pending steps")
			}
		case "in_progress":
			allCompleted = false
			if seenInProgress || seenPending {
				return toolJSONError(out, "invalid_status_order", "plan must contain exactly one ordered in_progress step")
			}
			seenInProgress = true
			currentStep = index + 1
		case "pending":
			allCompleted = false
			seenPending = true
		default:
			return toolJSONError(out, "invalid_status", "status must be pending, in_progress, or completed")
		}
	}
	if !allCompleted && !seenInProgress {
		return toolJSONError(out, "missing_current_step", "an unfinished plan must contain exactly one in_progress step")
	}
	if allCompleted {
		currentStep = len(args.Plan)
	}

	out.Ok = true
	out.Content = jsonString(map[string]any{
		"ok":          true,
		"plan":        args.Plan,
		"currentStep": currentStep,
		"totalSteps":  len(args.Plan),
	})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(args.Plan)
	return out
}

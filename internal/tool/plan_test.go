package tool

import (
	"encoding/json"
	"testing"
)

func TestPlanUpdateReturnsNormalizedProgress(t *testing.T) {
	result := planUpdate(Call{
		CallID: "call_plan",
		Name:   PlanUpdate,
		Args: json.RawMessage(`{"plan":[
			{"step":" Inspect project ","status":"completed"},
			{"step":"Implement progress grid","status":"in_progress"},
			{"step":"Verify build","status":"pending"}
		]}`),
	})
	if !result.Ok {
		t.Fatalf("plan update failed: %s", result.Content)
	}
	var payload struct {
		CurrentStep int        `json:"currentStep"`
		TotalSteps  int        `json:"totalSteps"`
		Plan        []planStep `json:"plan"`
	}
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.CurrentStep != 2 || payload.TotalSteps != 3 || payload.Plan[0].Step != "Inspect project" {
		t.Fatalf("unexpected progress payload: %+v", payload)
	}
}

func TestPlanUpdateRequiresOneOrderedCurrentStep(t *testing.T) {
	tests := []struct {
		name string
		plan string
	}{
		{name: "missing current", plan: `[{"step":"One","status":"pending"},{"step":"Two","status":"pending"}]`},
		{name: "multiple current", plan: `[{"step":"One","status":"in_progress"},{"step":"Two","status":"in_progress"}]`},
		{name: "completed after current", plan: `[{"step":"One","status":"in_progress"},{"step":"Two","status":"completed"}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := planUpdate(Call{Name: PlanUpdate, Args: json.RawMessage(`{"plan":` + tt.plan + `}`)})
			if result.Ok {
				t.Fatalf("invalid plan unexpectedly succeeded: %s", result.Content)
			}
		})
	}
}

func TestPlanUpdateAllowsCompletedPlan(t *testing.T) {
	result := planUpdate(Call{Name: PlanUpdate, Args: json.RawMessage(`{"plan":[{"step":"One","status":"completed"},{"step":"Two","status":"completed"}]}`)})
	if !result.Ok {
		t.Fatalf("completed plan failed: %s", result.Content)
	}
}

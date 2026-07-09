package tool

import (
	"encoding/json"
	"testing"
)

func TestClassifyToolCallProjectFileWrite(t *testing.T) {
	risk, ok := ClassifyToolCall(FilePatch, json.RawMessage(`{"scope":"project","path":"main.go","old_string":"a","new_string":"b"}`))
	if !ok {
		t.Fatal("project file patch should be classified")
	}
	if risk.Class != RiskClassWrite || risk.Operation != "patch" || len(risk.Paths) != 1 || risk.Paths[0] != "main.go" {
		t.Fatalf("bad risk: %+v", risk)
	}
}

func TestClassifyToolCallRejectsLegacyWorkspaceScope(t *testing.T) {
	if risk, ok := ClassifyToolCall(FileWrite, json.RawMessage(`{"scope":"workspace","path":"main.go","content":"x"}`)); ok {
		t.Fatalf("legacy workspace scope must not be classified as a project write: %+v", risk)
	}
}

func TestClassifyToolCallIgnoresManagedWrites(t *testing.T) {
	if risk, ok := ClassifyToolCall(FileWrite, json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md","content":"x"}`)); ok {
		t.Fatalf("managed writes should not use project approval: %+v", risk)
	}
}

func TestClassifyToolCallCommandRisk(t *testing.T) {
	tests := []struct {
		name      string
		args      string
		class     RiskClass
		operation string
		lowRisk   bool
	}{
		{name: "test", args: `{"scope":"project","argv":["go","test","./..."]}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "test with env", args: `{"scope":"project","argv":["go","test","./..."],"env":{"GOFLAGS":"-race"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "test outside project", args: `{"scope":"project","argv":["go","test","../other"]}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "arbitrary", args: `{"scope":"project","argv":["python3","script.py"]}`, class: RiskClassCommand, operation: "python3", lowRisk: false},
		{name: "destructive", args: `{"scope":"project","argv":["rm","-rf","build"]}`, class: RiskClassDestructive, operation: "rm", lowRisk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(tt.args))
			if !ok || risk.Class != tt.class || risk.Operation != tt.operation || risk.LowRisk != tt.lowRisk || risk.Scope != "project" {
				t.Fatalf("unexpected command risk: %+v ok=%v", risk, ok)
			}
		})
	}
}

func TestClassifyToolCallRejectsLegacyCommandScope(t *testing.T) {
	if risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(`{"scope":"workspace","argv":["go","test","./..."]}`)); ok {
		t.Fatalf("legacy command scope must be rejected: %+v", risk)
	}
}

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

func TestClassifyToolCallGitReadRisk(t *testing.T) {
	for _, name := range []string{GitStatus, GitDiff, GitLog} {
		risk, ok := ClassifyToolCall(name, json.RawMessage(`{"scope":"project","cwd":"."}`))
		if !ok || risk.Class != RiskClassRead || !risk.LowRisk || risk.Scope != "project" || len(risk.Paths) != 1 || risk.Paths[0] != "." {
			t.Fatalf("unexpected git read risk for %s: %+v ok=%v", name, risk, ok)
		}
	}
	if risk, ok := ClassifyToolCall(GitStatus, json.RawMessage(`{"scope":"workspace"}`)); ok {
		t.Fatalf("legacy git scope must be rejected: %+v", risk)
	}
}

func TestClassifyToolCallCodeReadRisk(t *testing.T) {
	for _, test := range []struct {
		name string
		args string
	}{
		{name: CodeSymbols, args: `{"scope":"project","path":".","query":"Runner"}`},
		{name: CodeDefinition, args: `{"scope":"project","path":"main.go","line":1,"column":1}`},
		{name: CodeReferences, args: `{"scope":"project","path":"main.go","line":1,"column":1}`},
		{name: CodeDiagnostics, args: `{"scope":"project","paths":["main.go"]}`},
		{name: CodeRename, args: `{"scope":"project","path":"main.go","line":1,"column":1,"new_name":"renamed"}`},
	} {
		risk, ok := ClassifyToolCall(test.name, json.RawMessage(test.args))
		if !ok || risk.Class != RiskClassRead || !risk.LowRisk || risk.Scope != managedScopeProject {
			t.Fatalf("unexpected code read risk for %s: %+v ok=%v", test.name, risk, ok)
		}
	}
}

func TestClassifyToolCallPatchApplyRisk(t *testing.T) {
	risk, ok := ClassifyToolCall(PatchApply, json.RawMessage(`{"proposal_id":"patch_123"}`))
	if !ok || risk.Class != RiskClassWrite || risk.Operation != "patch_apply" || risk.Scope != "project" {
		t.Fatalf("unexpected patch apply risk: %+v ok=%v", risk, ok)
	}
	if _, ok := ClassifyToolCall(PatchApply, json.RawMessage(`{"proposal_id":""}`)); ok {
		t.Fatal("empty patch proposal id must not be classified")
	}
}

func TestClassifyToolCallGitWriteRisk(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      string
		operation string
		paths     int
	}{
		{name: GitStage, args: `{"scope":"project","paths":["main.go"]}`, operation: "git_stage", paths: 1},
		{name: GitUnstage, args: `{"scope":"project","paths":["main.go"]}`, operation: "git_unstage", paths: 1},
		{name: GitCommit, args: `{"scope":"project","message":"test"}`, operation: "git_commit"},
	} {
		risk, ok := ClassifyToolCall(tt.name, json.RawMessage(tt.args))
		if !ok || risk.Class != RiskClassWrite || risk.Operation != tt.operation || risk.Scope != "project" || len(risk.Paths) != tt.paths {
			t.Fatalf("unexpected Git write risk for %s: %+v ok=%v", tt.name, risk, ok)
		}
	}
	if _, ok := ClassifyToolCall(GitStage, json.RawMessage(`{"scope":"workspace","paths":["main.go"]}`)); ok {
		t.Fatal("legacy Git write scope must not be classified")
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

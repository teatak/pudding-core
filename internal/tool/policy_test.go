package tool

import (
	"encoding/json"
	"testing"
)

func TestClassifyToolCallWorkspaceFileWrite(t *testing.T) {
	risk, ok := ClassifyToolCall(FilePatch, json.RawMessage(`{"scope":"project","path":"main.go","old_string":"a","new_string":"b"}`))
	if !ok {
		t.Fatal("workspace file patch should be classified")
	}
	if risk.Class != RiskClassWrite || risk.Operation != "patch" || len(risk.Paths) != 1 || risk.Paths[0] != "main.go" {
		t.Fatalf("bad risk: %+v", risk)
	}
}

func TestClassifyToolCallIgnoresManagedWrites(t *testing.T) {
	if risk, ok := ClassifyToolCall(FileWrite, json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md","content":"x"}`)); ok {
		t.Fatalf("managed writes should not use project approval: %+v", risk)
	}
}

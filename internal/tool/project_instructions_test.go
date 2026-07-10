package tool

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type projectInstructionsPayload struct {
	OK               bool                           `json:"ok"`
	Targets          []projectInstructionTargetView `json:"targets"`
	TargetCount      int                            `json:"targetCount"`
	Instructions     []projectInstructionFileView   `json:"instructions"`
	InstructionCount int                            `json:"instructionCount"`
	Warnings         []projectInstructionWarning    `json:"warnings"`
}

func TestProjectInstructionsResolveDirectoryScopes(t *testing.T) {
	root := t.TempDir()
	writeProjectInstructionTestFile(t, root, "CONTRIBUTING.md", "root contributing\n")
	writeProjectInstructionTestFile(t, root, "AGENTS.md", "root agents\n")
	writeProjectInstructionTestFile(t, root, "web/CLAUDE.md", "web assistant\n")
	writeProjectInstructionTestFile(t, root, "web/AGENTS.md", "web agents\n")
	writeProjectInstructionTestFile(t, root, "web/src/app.ts", "export const app = true;\n")
	writeProjectInstructionTestFile(t, root, "backend/main.go", "package backend\n")

	runner := NewBuiltinRunner()
	result := projectInstructionsTestCall(runner, root, []string{"web/src/app.ts", "backend/main.go", "web/src/new.ts"})
	payload := decodeProjectInstructionsPayload(t, result)
	if !result.Ok || !payload.OK || payload.TargetCount != 3 || payload.InstructionCount != 4 {
		t.Fatalf("unexpected instructions result: result=%+v payload=%+v", result, payload)
	}
	wantOrder := []string{"CONTRIBUTING.md", "AGENTS.md", "web/CLAUDE.md", "web/AGENTS.md"}
	for index, want := range wantOrder {
		instruction := payload.Instructions[index]
		if instruction.Path != want || instruction.Order != index+1 || instruction.Content == "" {
			t.Fatalf("instruction %d=%+v want path=%s", index, instruction, want)
		}
	}
	rootAgents := payload.Instructions[1]
	if len(rootAgents.AppliesTo) != 3 {
		t.Fatalf("root instructions should apply to every target: %+v", rootAgents.AppliesTo)
	}
	webAgents := payload.Instructions[3]
	if len(webAgents.AppliesTo) != 2 || containsString(webAgents.AppliesTo, "backend/main.go") {
		t.Fatalf("nested instructions leaked to sibling target: %+v", webAgents.AppliesTo)
	}
	missing := findInstructionTarget(payload.Targets, "web/src/new.ts")
	if missing == nil || missing.Exists || missing.Directory != "web/src" {
		t.Fatalf("missing target scope was not resolved: %+v", missing)
	}
}

func TestProjectInstructionsRejectOutsideTarget(t *testing.T) {
	root := t.TempDir()
	runner := NewBuiltinRunner()
	result := projectInstructionsTestCall(runner, root, []string{"../outside.go"})
	if result.Ok || !strings.Contains(result.Content, `"reason":"path_not_authorized"`) {
		t.Fatalf("outside target should fail: %+v", result)
	}
}

func TestProjectInstructionsTruncateLargeContent(t *testing.T) {
	root := t.TempDir()
	writeProjectInstructionTestFile(t, root, "AGENTS.md", strings.Repeat("instruction line\n", projectInstructionMaxFileBytes/8))
	runner := NewBuiltinRunner()
	payload := decodeProjectInstructionsPayload(t, projectInstructionsTestCall(runner, root, []string{"."}))
	if len(payload.Instructions) != 1 || !payload.Instructions[0].Truncated {
		t.Fatalf("large instruction must be truncated: %+v", payload.Instructions)
	}
	if len([]byte(payload.Instructions[0].Content)) > projectInstructionMaxFileBytes {
		t.Fatalf("instruction exceeded content limit: %d", len([]byte(payload.Instructions[0].Content)))
	}
}

func projectInstructionsTestCall(runner *BuiltinRunner, root string, paths []string) Result {
	raw, _ := json.Marshal(map[string]any{"scope": "project", "paths": paths})
	return runner.Call(context.Background(), Call{
		CallID:      "call_project_instructions",
		Name:        ProjectInstructions,
		Args:        raw,
		ProjectDirs: []string{root},
	})
}

func decodeProjectInstructionsPayload(t *testing.T, result Result) projectInstructionsPayload {
	t.Helper()
	var payload projectInstructionsPayload
	if err := json.Unmarshal([]byte(result.Content), &payload); err != nil {
		t.Fatalf("decode project instructions: %v content=%q", err, result.Content)
	}
	return payload
}

func writeProjectInstructionTestFile(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findInstructionTarget(targets []projectInstructionTargetView, path string) *projectInstructionTargetView {
	for index := range targets {
		if targets[index].Path == path {
			return &targets[index]
		}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

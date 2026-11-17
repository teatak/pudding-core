package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestDebugMCPAssistantProjectLookup(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	ctx := context.Background()
	if _, err := fixture.store.OpenAssistant(ctx, "Puddy", "mock", "mock-model", store.AssistantAppearance{"version": 1}); err != nil {
		t.Fatal(err)
	}
	project := &store.Project{ID: "mcp-project", Name: "pudding-core", RootDirs: []string{t.TempDir()}}
	if err := fixture.store.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	debugMCPBindingForTest(t, fixture.server.URL, map[string]string{"sessionID": "personal"})
	if _, ok := debugMCPListForTest(t, fixture.server.URL)[tool.AssistantProject]; !ok {
		t.Fatal("MCP personal binding is missing the assistant project tool")
	}
	result := debugMCPCallForTest(t, fixture.server.URL, tool.AssistantProject, map[string]string{"action": "list", "query": project.Name})
	var listed struct {
		Projects []*store.Project `json:"projects"`
	}
	if len(result.Content) != 1 {
		t.Fatalf("unexpected project lookup content: %+v", result.Content)
	}
	text, _ := result.Content[0]["text"].(string)
	if json.Unmarshal([]byte(text), &listed) != nil || len(listed.Projects) != 1 || listed.Projects[0].ID != project.ID {
		t.Fatalf("MCP project name did not resolve to its ID: %s", text)
	}
}

package api

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestDebugMCPAssistantProjectSessionDiscovery(t *testing.T) {
	fixture := newDebugMCPTestServer(t)
	ctx := context.Background()
	if _, err := fixture.store.OpenAssistant(ctx, "Puddy", "mock", "mock-model", store.AssistantAppearance{"version": 1}); err != nil {
		t.Fatal(err)
	}
	project := &store.Project{ID: "session-discovery-project", Name: "pudding-core"}
	if err := fixture.store.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	worker := &store.Session{ID: "manual-project-session", Title: "Existing review", ProjectID: project.ID, Provider: "mock", Model: "mock-model"}
	if err := fixture.store.CreateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	debugMCPBindingForTest(t, fixture.server.URL, map[string]string{"sessionID": "personal"})
	if _, ok := debugMCPListForTest(t, fixture.server.URL)[tool.AssistantProject]; !ok {
		t.Fatal("personal MCP binding is missing project discovery")
	}
	result := debugMCPCallForTest(t, fixture.server.URL, tool.AssistantProject, map[string]string{
		"action": "sessions", "project_id": project.ID, "query": "review",
	})
	var listed struct {
		ProjectID string          `json:"projectID"`
		Sessions  []store.Session `json:"sessions"`
	}
	if len(result.Content) != 1 {
		t.Fatalf("unexpected session discovery content: %+v", result.Content)
	}
	text, _ := result.Content[0]["text"].(string)
	if json.Unmarshal([]byte(text), &listed) != nil || listed.ProjectID != project.ID || len(listed.Sessions) != 1 || listed.Sessions[0].ID != worker.ID || listed.Sessions[0].ActiveMode != store.ModeCode {
		t.Fatalf("MCP did not discover the manually created project conversation: %s", text)
	}
}

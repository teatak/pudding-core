package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestApproveApprovalReturnsAuthoritativeSessionProject(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	hub := event.NewHub()
	homeDir := t.TempDir()
	projectDir := t.TempDir()
	client := &approvalProjectClient{}
	eng := engine.New(
		ms,
		hub,
		approvalResolver{client: client},
		ms,
		engine.WithAttachmentHome(homeDir),
		engine.WithTools(tool.NewBuiltinRunner()),
	)
	server := httptest.NewServer(New(eng, ms, ms, hub).WithHome(homeDir).Handler(testToken, nil))
	t.Cleanup(server.Close)

	const sessionID = "sess_approval_response"
	if err := ms.CreateSession(ctx, &store.Session{
		ID: sessionID, Title: "approval", Provider: "approval", Model: "approval-model",
	}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		DisplayName: "approval",
		Protocol:    "openai-compatible",
		Models: []store.ProviderModel{{
			ID:           "approval-model",
			Capabilities: &store.ModelCaps{Tools: true},
			Limits:       &store.ModelLimits{MaxToolLoops: 3},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	sub, unsubscribe := hub.Subscribe(sessionID)
	defer unsubscribe()
	if _, err := eng.Submit(ctx, engine.SubmitInput{
		SessionID: sessionID, ClientMessageID: "client_approval", Text: "write a file",
	}); err != nil {
		t.Fatal(err)
	}

	var approvalID string
	deadline := time.After(time.Second)
	for approvalID == "" {
		select {
		case ev := <-sub:
			if ev.Kind == event.ApprovalRequested {
				approvalID = ev.ApprovalID
			}
		case <-deadline:
			t.Fatal("approval request not emitted")
		}
	}

	response := req(t, http.MethodPost, server.URL+"/sessions/"+sessionID+"/approvals/"+approvalID+"/approve", map[string]any{
		"scope":       "session",
		"projectDirs": []string{projectDir},
	})
	if response.StatusCode != http.StatusAccepted {
		response.Body.Close()
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusAccepted)
	}
	payload := decodeJSON[struct {
		Status  string        `json:"status"`
		Session store.Session `json:"session"`
	}](t, response)
	if payload.Status != "approved" {
		t.Fatalf("status = %q, want approved", payload.Status)
	}
	if payload.Session.ID != sessionID || payload.Session.ProjectID == "" {
		t.Fatalf("session response is not authoritative: %+v", payload.Session)
	}
	if !payload.Session.Running {
		t.Fatalf("session response lost derived running state: %+v", payload.Session)
	}

	project, err := ms.GetProject(ctx, payload.Session.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(project.RootDirs) != 1 || project.RootDirs[0] != projectDir {
		t.Fatalf("project roots = %+v, want %q", project.RootDirs, projectDir)
	}
}

type approvalResolver struct {
	client provider.Client
}

func (r approvalResolver) Resolve(_ context.Context, name string) (provider.Client, error) {
	if name != "approval" {
		return nil, errors.New("unknown provider: " + name)
	}
	return r.client, nil
}

type approvalProjectClient struct {
	requests int
}

func (c *approvalProjectClient) Name() string { return "approval-project" }

func (c *approvalProjectClient) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	c.requests++
	out := make(chan provider.Chunk, 2)
	if c.requests == 1 {
		out <- provider.Chunk{Tool: &provider.ToolCallChunk{
			Index:     0,
			CallID:    "call_approval_project",
			Name:      tool.RequestCapability,
			ArgsDelta: `{"targetMode":"code","reason":"write a file","needsProjectDir":true}`,
		}}
		out <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		out <- provider.Chunk{Part: provider.PartText, Delta: "done"}
		out <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(out)
	return out, nil
}

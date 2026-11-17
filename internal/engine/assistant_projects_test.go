package engine

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func newAssistantProjectEngine(t *testing.T) (*Engine, *storetest.Store, string) {
	t.Helper()
	eng, db := newDirectToolEngine(t)
	eng.resolver = mapResolver{"configured": &nextPromptClient{}}
	profile, err := db.OpenAssistant(context.Background(), "Puddy", "configured", "configured-model", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	return eng, db, profile.Assistant.SessionID
}

func assistantProjectList(t *testing.T, ctx context.Context, eng *Engine, sid, query string) []*store.Project {
	t.Helper()
	result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantProject, mustJSON(map[string]string{"action": "list", "query": query}))
	var listed struct {
		Projects []*store.Project `json:"projects"`
	}
	if err != nil || !result.Ok || json.Unmarshal([]byte(result.Content), &listed) != nil {
		t.Fatalf("list projects: %+v, %v", result, err)
	}
	return listed.Projects
}

func TestAssistantProjectLookupAndScope(t *testing.T) {
	ctx := context.Background()
	eng, db, sid := newAssistantProjectEngine(t)
	first := &store.Project{ID: "project-first", Name: "pudding-core", RootDirs: []string{t.TempDir()}}
	second := &store.Project{ID: "project-second", Name: first.Name, RootDirs: []string{t.TempDir()}}
	for _, project := range []*store.Project{first, second} {
		if err := db.CreateProject(ctx, project); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query string
		count int
	}{
		{"", 2}, {"PUDDING-CORE", 2}, {first.RootDirs[0], 1}, {first.ID, 1}, {"not-a-project", 0},
	} {
		if got := assistantProjectList(t, ctx, eng, sid, tc.query); len(got) != tc.count {
			t.Fatalf("query %q returned %d projects; want %d", tc.query, len(got), tc.count)
		}
	}
	defs, err := eng.ToolDefinitions(ctx, sid, store.ModeChat)
	if err != nil || !tool.HasDefinition(defs, tool.AssistantProject) {
		t.Fatalf("assistant cannot discover project tool: %v", err)
	}
	ordinary, err := eng.DebugSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defs, err = eng.ToolDefinitions(ctx, ordinary.ID, store.ModeCode)
	if err != nil || tool.HasDefinition(defs, tool.AssistantProject) {
		t.Fatalf("assistant project tool leaked to ordinary session: %v", err)
	}
	result := eng.executeAssistantTask(ctx, ordinary.ID, "", tool.Call{Name: tool.AssistantProject, Args: mustJSON(map[string]string{"action": "list"})})
	if result.Ok {
		t.Fatal("ordinary session bypassed assistant scope")
	}
}

func TestAssistantProjectCreateLookupAndDispatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	eng, db, sid := newAssistantProjectEngine(t)
	if listed := assistantProjectList(t, ctx, eng, sid, "pudding-core"); len(listed) != 0 {
		t.Fatal("unexpected initial project")
	}
	root := t.TempDir()
	type outcome struct {
		result tool.Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantProject, mustJSON(map[string]any{"action": "create", "name": "pudding-core", "root_dirs": []string{root}}))
		done <- outcome{result, err}
	}()
	approval := waitAssistantTaskApproval(t, eng, sid)
	var details struct {
		ProjectName  string             `json:"projectName"`
		RootDirs     []string           `json:"rootDirs"`
		ApprovalMode store.ApprovalMode `json:"approvalMode"`
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(approval.Payload, &details) != nil || details.ProjectName != "pudding-core" || len(details.RootDirs) != 1 || details.RootDirs[0] != resolvedRoot || details.ApprovalMode != store.ApprovalAsk {
		t.Fatalf("approval does not describe project scope: %s", approval.Payload)
	}
	if projects, err := db.ListProjects(ctx); err != nil || len(projects) != 0 {
		t.Fatalf("project created before approval: %v, %v", projects, err)
	}
	if err := eng.ApproveApproval(ctx, sid, approval.ID, ApprovalScopeTurn, nil); err != nil {
		t.Fatal(err)
	}
	var created struct {
		Project *store.Project `json:"project"`
	}
	select {
	case out := <-done:
		if out.err != nil || !out.result.Ok || json.Unmarshal([]byte(out.result.Content), &created) != nil || created.Project == nil {
			t.Fatalf("create failed: %+v, %v", out.result, out.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if created.Project.ApprovalMode != store.ApprovalAsk || len(created.Project.RootDirs) != 1 || created.Project.RootDirs[0] != resolvedRoot {
		t.Fatalf("unexpected project permissions: %+v", created.Project)
	}
	listed := assistantProjectList(t, ctx, eng, sid, resolvedRoot)
	if len(listed) != 1 || listed[0].ID != created.Project.ID {
		t.Fatalf("directory lookup lost created project: %+v", listed)
	}
	parent, err := db.GetSession(ctx, sid)
	if err != nil || parent.ProjectID != "" || parent.ActiveMode != store.ModeChat {
		t.Fatalf("project creation rebound or upgraded the assistant: %+v, %v", parent, err)
	}
	if dirs, err := db.GetSessionDirectories(ctx, sid); err != nil || len(dirs) != 0 {
		t.Fatalf("project creation granted assistant directory access: %v, %v", dirs, err)
	}
	result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantTaskDispatch, mustJSON(map[string]string{
		"title": "Project task", "prompt": "Return a brief acknowledgement.", "project_id": listed[0].ID,
	}))
	var task store.AssistantTaskRun
	if err != nil || !result.Ok || json.Unmarshal([]byte(result.Content), &task) != nil || task.SessionID == "" {
		t.Fatalf("cannot dispatch using discovered project ID: %+v, %v", result, err)
	}
	worker, err := db.GetSession(ctx, task.SessionID)
	if err != nil || worker.ProjectID != created.Project.ID || worker.ActiveMode != store.ModeWork {
		t.Fatalf("dispatch lost project or silently granted Code: %+v, %v", worker, err)
	}
}

func TestAssistantProjectDeniedCreation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	eng, db, sid := newAssistantProjectEngine(t)
	done := make(chan tool.Result, 1)
	go func() {
		result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantProject, mustJSON(map[string]string{"action": "create", "name": "Declined project"}))
		if err != nil {
			result = tool.Result{Content: err.Error()}
		}
		done <- result
	}()
	approval := waitAssistantTaskApproval(t, eng, sid)
	if err := eng.DenyApproval(ctx, sid, approval.ID, "Do not create it"); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-done:
		var denied struct{ Status string }
		if result.Ok || json.Unmarshal([]byte(result.Content), &denied) != nil || denied.Status != "denied" {
			t.Fatalf("denial not returned: %+v", result)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if projects, err := db.ListProjects(ctx); err != nil || len(projects) != 0 {
		t.Fatalf("denied creation changed projects: %v, %v", projects, err)
	}
}

func TestAssistantProjectRejectsInvalidCreation(t *testing.T) {
	ctx := context.Background()
	eng, db, sid := newAssistantProjectEngine(t)
	for _, args := range []string{
		`{"action":"create","name":""}`,
		`{"action":"create","name":"Project","root_dirs":["relative/path"]}`,
		`{"action":"create","name":"Project","approval_mode":"full"}`,
	} {
		result, err := eng.CallTool(ctx, sid, store.ModeChat, tool.AssistantProject, json.RawMessage(args))
		if err != nil || result.Ok || len(eng.PendingApprovals(sid)) != 0 {
			t.Fatalf("invalid creation reached approval or succeeded: %+v, %v", result, err)
		}
	}
	if projects, err := db.ListProjects(ctx); err != nil || len(projects) != 0 {
		t.Fatalf("invalid creation changed projects: %v, %v", projects, err)
	}
}

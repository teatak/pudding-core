package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// executeAssistantTask has already checked the fixed assistant conversation.
// Project metadata uses the existing store; creation never upgrades a session.
func (e *Engine) executeAssistantProject(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	fail := func(err error) tool.Result {
		return approvalToolResult(call, false, map[string]any{"ok": false, "error": err.Error()})
	}
	var input struct {
		Action   string   `json:"action"`
		Query    string   `json:"query"`
		Name     string   `json:"name"`
		RootDirs []string `json:"root_dirs"`
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return fail(err)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fail(errors.New("expected one project arguments object"))
	}
	switch input.Action {
	case "list":
		if input.Name != "" || len(input.RootDirs) != 0 {
			return fail(errors.New("list accepts only action and query"))
		}
		projects, err := e.store.ListProjects(ctx)
		if err != nil {
			return fail(err)
		}
		query := strings.TrimSpace(input.Query)
		if filepath.IsAbs(query) {
			query = filepath.Clean(query)
		}
		query = strings.ToLower(query)
		matches := make([]*store.Project, 0)
		for _, project := range projects {
			matched := query == "" || strings.Contains(strings.ToLower(project.Name), query) || strings.EqualFold(project.ID, query)
			for _, dir := range project.RootDirs {
				matched = matched || strings.Contains(strings.ToLower(dir), query)
			}
			if matched {
				matches = append(matches, project)
			}
		}
		return approvalToolResult(call, true, map[string]any{"ok": true, "projects": matches})
	case "create":
		input.Name = strings.TrimSpace(input.Name)
		if input.Query != "" || input.Name == "" || len([]rune(input.Name)) > 100 || strings.ContainsAny(input.Name, "\r\n\x00") {
			return fail(errors.New("create requires a one-line name of 1..100 characters and does not accept query"))
		}
		// Validate before normalization, which intentionally drops relative paths.
		for _, dir := range input.RootDirs {
			if !filepath.IsAbs(dir) {
				return fail(fmt.Errorf("project directory must be absolute: %s", dir))
			}
		}
		dirs, err := approvedDirectories(input.RootDirs)
		if err != nil {
			return fail(err)
		}
		approvalMode := store.NormalizeApprovalMode("")
		risk := tool.ToolRisk{
			Class: tool.RiskClassWrite, Operation: "assistant_project_create", Scope: "project", Paths: dirs,
			Summary: fmt.Sprintf("Create project %q with these directory roots and Auto approval. New project conversations start in Code; tool actions still follow the project approval policy. This does not change the assistant's capability or grant Full Access.", input.Name),
		}
		details := map[string]any{"projectName": input.Name, "rootDirs": dirs, "approvalMode": approvalMode}
		result, approved := e.requestToolCallApproval(ctx, sessionID, turnID, call, risk, nil, details, "", nil, nil)
		if !approved {
			return result
		}
		project := &store.Project{ID: store.NewID("proj"), Name: input.Name, RootDirs: dirs, ApprovalMode: approvalMode}
		if err := e.store.CreateProject(ctx, project); err != nil {
			return fail(err)
		}
		return approvalToolResult(call, true, map[string]any{"ok": true, "project": project})
	default:
		return fail(errors.New("action must be list or create"))
	}
}

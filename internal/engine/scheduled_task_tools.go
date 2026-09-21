package engine

import (
	"context"
	"encoding/json"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
	"strings"
)

func (e *Engine) scheduledToolDefinitions(ctx context.Context, sessionID string, defs []provider.ToolDef) ([]provider.ToolDef, error) {
	out := make([]provider.ToolDef, 0, len(defs)+1)
	for _, d := range defs {
		if !tool.IsScheduledTaskTool(d.Name) {
			out = append(out, d)
		}
	}
	owner, err := e.store.ParentSessionID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if owner == "" {
		out = append(out, tool.ScheduledTaskDefinitions()...)
	}
	return out, nil
}
func (e *Engine) executeScheduledTask(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	result := tool.Result{CallID: call.CallID, Name: call.Name}
	fail := func(err error) tool.Result { result.Content = err.Error(); return result }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	owner, err := e.store.ParentSessionID(ctx, sessionID)
	if err != nil {
		return fail(err)
	}
	if owner != "" {
		return fail(store.ErrInvalidSessionRelation)
	}
	var args struct {
		Action       string              `json:"action"`
		TaskID       string              `json:"taskID"`
		Revision     int64               `json:"revision"`
		Name         *string             `json:"name"`
		Prompt       *string             `json:"prompt"`
		Schedule     *store.TaskSchedule `json:"schedule"`
		Enabled      *bool               `json:"enabled"`
		DelaySeconds int64               `json:"delaySeconds"`
	}
	dec := json.NewDecoder(strings.NewReader(string(call.Args)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&args); err != nil {
		return fail(err)
	}
	var payload any
	if args.Action != "create" && args.Action != "list" {
		task, err := e.store.GetScheduledTask(ctx, args.TaskID)
		if err != nil {
			return fail(err)
		}
		if task.SessionID != sessionID {
			return fail(store.ErrNotFound)
		}
	}
	switch args.Action {
	case "create":
		if args.Name == nil || args.Prompt == nil || args.Schedule == nil {
			return fail(store.ErrInvalidSchedule)
		}
		payload, err = e.CreateScheduledTask(ctx, store.ScheduledTaskCreate{SessionID: sessionID, RequestID: "tool_" + turnID + "_" + call.CallID, Name: *args.Name, Prompt: *args.Prompt, Schedule: *args.Schedule, DelaySeconds: args.DelaySeconds})
	case "list":
		payload, err = e.ScheduledTasks(ctx, sessionID, false)
	case "update", "delete":
		payload, err = e.UpdateScheduledTask(ctx, args.TaskID, store.ScheduledTaskUpdate{Revision: args.Revision, Name: args.Name, Prompt: args.Prompt, Schedule: args.Schedule, Enabled: args.Enabled, Delete: args.Action == "delete"})
	case "run":
		payload, err = e.RunScheduledTask(ctx, args.TaskID, "tool_"+turnID+"_"+call.CallID)
	default:
		return fail(store.ErrInvalidSchedule)
	}
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(map[string]any{"action": args.Action, "result": payload})
	if err != nil {
		return fail(err)
	}
	result.Ok = true
	result.Content = string(raw)
	return result
}

package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) executeAssistantMemory(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	result := tool.Result{CallID: call.CallID, Name: call.Name}
	fail := func(err error) tool.Result { result.Content = err.Error(); return result }
	var args struct {
		Action    string `json:"action"`
		Query     string `json:"query"`
		Content   string `json:"content"`
		MemoryID  string `json:"memory_id"`
		Revision  int64  `json:"revision"`
		ProjectID string `json:"project_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return fail(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fail(store.ErrInvalidAssistantMemory)
	}
	var payload any
	var err error
	switch args.Action {
	case "search":
		if args.Content != "" || args.Revision != 0 {
			return fail(store.ErrInvalidAssistantMemory)
		}
		memories, searchErr := e.store.SearchAssistantMemories(ctx, sessionID, store.AssistantMemoryQuery{ID: args.MemoryID, Query: args.Query, ProjectID: args.ProjectID, Limit: 20})
		err = searchErr
		payload = map[string]any{"memories": memories}
	case "save":
		if args.Query != "" {
			return fail(store.ErrInvalidAssistantMemory)
		}
		payload, err = e.store.SaveAssistantMemory(ctx, sessionID, store.AssistantMemoryWrite{ID: args.MemoryID, ExpectedRevision: args.Revision, Content: args.Content, ProjectID: args.ProjectID, SourceTurnID: turnID})
	case "delete":
		if args.Query != "" || args.Content != "" || args.ProjectID != "" {
			return fail(store.ErrInvalidAssistantMemory)
		}
		err = e.store.DeleteAssistantMemory(ctx, sessionID, args.MemoryID, args.Revision)
		payload = map[string]any{"deleted": args.MemoryID}
	default:
		return fail(store.ErrInvalidAssistantMemory)
	}
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fail(err)
	}
	result.Ok, result.Content = true, string(raw)
	return result
}

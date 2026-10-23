package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) assistantToolDefinitions(ctx context.Context, sessionID string, defs []provider.ToolDef) ([]provider.ToolDef, error) {
	out := make([]provider.ToolDef, 0, len(defs)+1)
	for _, def := range defs {
		if !tool.IsAssistantTool(def.Name) {
			out = append(out, def)
		}
	}
	profile, err := e.store.GetAssistant(ctx)
	if err != nil {
		return nil, err
	}
	if profile.Assistant != nil && profile.Assistant.SessionID == sessionID {
		out = append(out, tool.AssistantDefinitions()...)
	}
	return out, nil
}

func (e *Engine) executeAssistantRename(ctx context.Context, sessionID string, call tool.Call) tool.Result {
	result := tool.Result{CallID: call.CallID, Name: call.Name}
	fail := func(err error) tool.Result { result.Content = err.Error(); return result }
	var args struct {
		Name *string `json:"name"`
	}
	decoder := json.NewDecoder(bytes.NewReader(call.Args))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return fail(err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF || args.Name == nil {
		return fail(store.ErrInvalidAssistant)
	}
	profile, err := e.store.GetAssistant(ctx)
	if err != nil {
		return fail(err)
	}
	a := profile.Assistant
	if a == nil || a.SessionID != sessionID {
		return fail(errors.New("renaming the personal assistant is only available in its own conversation"))
	}
	in := store.SaveAssistantInput{ExpectedRevision: &a.Revision, Name: *args.Name, UserName: a.UserName, Appearance: a.Appearance}
	for _, memory := range profile.Memories {
		if memory.ID == "communication_preference" {
			in.CommunicationPreference = memory.Content
			break
		}
	}
	if err := in.Normalize(); err != nil {
		return fail(err)
	}
	saved, err := e.store.SaveAssistant(ctx, in)
	if errors.Is(err, store.ErrAssistantConflict) {
		return fail(errors.New("assistant profile changed during renaming; no changes were saved; retry to use the latest profile"))
	}
	if err != nil {
		return fail(err)
	}
	raw, err := json.Marshal(map[string]any{"name": saved.Assistant.Name, "usesDefaultName": saved.Assistant.Name == "", "revision": saved.Assistant.Revision})
	if err != nil {
		return fail(err)
	}
	result.Ok, result.Content = true, string(raw)
	return result
}

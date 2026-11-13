package engine

import (
	"context"
	"sync"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const maxParallelFileReads = 4

// Only fixed, read-only builtins can share a batch. Commands, plugins and
// mutations remain barriers, even when their names suggest a read operation.
func parallelFileRead(call tool.Call, allowed []provider.ToolDef) bool {
	switch call.Name {
	case tool.FileRead, tool.FileSlice, tool.FileSearch, tool.FileList:
		definition, ok := providerToolDefinition(allowed, call.Name)
		return ok && definition.PluginID == ""
	default:
		return false
	}
}

func (e *Engine) executeFileReads(ctx context.Context, sessionID, turnID string, mode store.AgentMode, allowed []provider.ToolDef, parts *turnPartAccumulator, calls []tool.Call) error {
	results := make([]tool.Result, len(calls))
	var workers sync.WaitGroup
	for index, call := range calls {
		call.SessionID, call.TurnID = sessionID, turnID
		e.hub.Publish(event.Event{SessionID: sessionID, Kind: event.TurnTool, TurnID: turnID, CallID: call.CallID, Name: call.Name, Phase: "running"})
		workers.Add(1)
		go func() {
			defer workers.Done()
			results[index] = e.executeAllowedTool(ctx, sessionID, turnID, mode, call)
		}()
	}
	workers.Wait()
	// Persist in provider order, including every dispatched result on cancellation.
	// The accumulator and canonical transcript are only mutated on this goroutine.
	for index, call := range calls {
		if err := e.completeToolCall(sessionID, turnID, allowed, parts, call, results[index]); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) completeToolCall(sessionID, turnID string, allowed []provider.ToolDef, parts *turnPartAccumulator, call tool.Call, result tool.Result) error {
	if result.CallID == "" {
		result.CallID = call.CallID
	}
	if result.Name == "" {
		result.Name = call.Name
	}
	if result.ModelOutput == nil {
		if definition, ok := providerToolDefinition(allowed, call.Name); ok {
			result.ModelOutput = definition.ModelOutput
		}
	}
	parts.AppendToolResult(result)
	parts.AppendAttachments(result.ContextAttachments)
	if err := e.commitTurnParts(turnID, parts, true); err != nil {
		return err
	}
	phase := "ok"
	if !result.Ok {
		phase = "error"
	}
	e.hub.Publish(event.Event{
		SessionID: sessionID, Kind: event.TurnTool, TurnID: turnID,
		CallID: result.CallID, Name: result.Name, Phase: phase, Ok: &result.Ok,
		Content: result.Content, SummaryKind: result.SummaryKind, SummaryCount: result.SummaryCount,
		Attachments: eventAttachmentsFromStore(result.Attachments),
	})
	return nil
}

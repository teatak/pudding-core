package tool

import (
	"context"
	"fmt"

	"github.com/teatak/pudding-core/internal/provider"
)

type MultiRunner struct {
	runners []Runner
}

func NewMultiRunner(runners ...Runner) *MultiRunner {
	out := make([]Runner, 0, len(runners))
	for _, runner := range runners {
		if runner != nil {
			out = append(out, runner)
		}
	}
	return &MultiRunner{runners: out}
}

func (r *MultiRunner) Definitions(ctx context.Context, sessionID string) ([]provider.ToolDef, error) {
	var defs []provider.ToolDef
	seen := map[string]bool{}
	for _, runner := range r.runners {
		runnerDefs, err := runner.Definitions(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		for _, def := range runnerDefs {
			if def.Name == "" || seen[def.Name] {
				continue
			}
			seen[def.Name] = true
			defs = append(defs, def)
		}
	}
	return defs, nil
}

func (r *MultiRunner) Call(ctx context.Context, call Call) Result {
	for _, runner := range r.runners {
		defs, err := runner.Definitions(ctx, call.SessionID)
		if err != nil {
			return Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: fmt.Sprintf("list tools: %v", err)}
		}
		if HasDefinition(defs, call.Name) {
			return runner.Call(ctx, call)
		}
	}
	return Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: fmt.Sprintf("unknown tool: %s", call.Name)}
}

func (r *MultiRunner) ApprovalDetails(ctx context.Context, call Call) (map[string]any, error) {
	for _, runner := range r.runners {
		defs, err := runner.Definitions(ctx, call.SessionID)
		if err != nil {
			return nil, err
		}
		if !HasDefinition(defs, call.Name) {
			continue
		}
		if provider, ok := runner.(ApprovalDetailsProvider); ok {
			return provider.ApprovalDetails(ctx, call)
		}
		return nil, nil
	}
	return nil, nil
}

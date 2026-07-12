package tool

import (
	"context"
	"errors"
	"fmt"

	"github.com/teatak/pudding-core/internal/provider"
)

type MultiRunner struct {
	runners []Runner
}

func (r *MultiRunner) CloseSession(sessionID string) {
	for _, runner := range r.runners {
		if cleaner, ok := runner.(SessionResourceCleaner); ok {
			cleaner.CloseSession(sessionID)
		}
	}
}

func (r *MultiRunner) Close() error {
	var errs []error
	for _, runner := range r.runners {
		if closer, ok := runner.(ResourceCloser); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (r *MultiRunner) ListBackgroundProcesses(sessionID string) []BackgroundProcessSnapshot {
	items := make([]BackgroundProcessSnapshot, 0)
	for _, runner := range r.runners {
		if controller, ok := runner.(BackgroundProcessController); ok {
			items = append(items, controller.ListBackgroundProcesses(sessionID)...)
		}
	}
	return items
}

func (r *MultiRunner) BackgroundProcessCount(sessionID string) int {
	count := 0
	for _, runner := range r.runners {
		if controller, ok := runner.(BackgroundProcessController); ok {
			count += controller.BackgroundProcessCount(sessionID)
		}
	}
	return count
}

func (r *MultiRunner) ReadBackgroundProcess(sessionID, processID string, offset int64, maxBytes, tailBytes int) (BackgroundProcessLogSnapshot, error) {
	for _, runner := range r.runners {
		controller, ok := runner.(BackgroundProcessController)
		if !ok {
			continue
		}
		item, err := controller.ReadBackgroundProcess(sessionID, processID, offset, maxBytes, tailBytes)
		if errors.Is(err, ErrBackgroundProcessNotFound) {
			continue
		}
		return item, err
	}
	return BackgroundProcessLogSnapshot{}, ErrBackgroundProcessNotFound
}

func (r *MultiRunner) StopBackgroundProcess(sessionID, processID string) (BackgroundProcessSnapshot, error) {
	for _, runner := range r.runners {
		controller, ok := runner.(BackgroundProcessController)
		if !ok {
			continue
		}
		item, err := controller.StopBackgroundProcess(sessionID, processID)
		if errors.Is(err, ErrBackgroundProcessNotFound) {
			continue
		}
		return item, err
	}
	return BackgroundProcessSnapshot{}, ErrBackgroundProcessNotFound
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
	return r.collectDefinitions(ctx, sessionID, nil, false)
}

func (r *MultiRunner) DefinitionsForApps(ctx context.Context, sessionID string, appIDs []string) ([]provider.ToolDef, error) {
	return r.collectDefinitions(ctx, sessionID, appIDs, true)
}

func (r *MultiRunner) collectDefinitions(ctx context.Context, sessionID string, appIDs []string, appScoped bool) ([]provider.ToolDef, error) {
	var defs []provider.ToolDef
	seen := map[string]bool{}
	for _, runner := range r.runners {
		var runnerDefs []provider.ToolDef
		var err error
		if scoped, ok := runner.(AppScopedDefinitionRunner); appScoped && ok {
			runnerDefs, err = scoped.DefinitionsForApps(ctx, sessionID, appIDs)
		} else {
			runnerDefs, err = runner.Definitions(ctx, sessionID)
		}
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

package engine

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/tool"
)

// All sessions use these daemon-owned locks, including the main conversation.
// Permission waiting happens before acquisition, so an unanswered approval cannot
// monopolize a project or the desktop.
func (e *Engine) acquireToolResources(ctx context.Context, call tool.Call) (func(), error) {
	// The process registry protects scratch lifetime without giving persistent
	// background services exclusive ownership of the project or its artifacts.
	// Check before waiting; the runner checks again after the lock is acquired.
	if call.Name == tool.TempWorkspace && e.BackgroundProcessCount(call.SessionID) > 0 {
		var args struct {
			Action string `json:"action"`
		}
		if json.Unmarshal(call.Args, &args) == nil && args.Action == "clear" {
			return nil, errors.New("workspace_in_use: Stop background processes before clearing the workspace")
		}
	}
	var keys []string
	if tool.UsesSessionArtifacts(call) {
		keys = append(keys, "session-artifacts:"+call.SessionID)
	}
	// Computer Use already serializes native mutations in computer.Manager.
	risk, classified, err := e.classifyToolCall(call.SessionID, &call)
	if err != nil {
		return nil, err
	}
	_, mutation := tool.MutationTrackingForCall(call)
	var releases []func()
	if mutation || call.Name == tool.CommandRun || classified && risk.Class != tool.RiskClassRead && risk.Scope != "computer" {
		release, err := e.AcquireWorkspaceWrite(ctx, call.ProjectDirs)
		if err != nil {
			return nil, err
		}
		releases = append(releases, release)
	}
	sort.Strings(keys)
	releaseAll := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	previous := ""
	for _, key := range keys {
		if key == previous {
			continue
		}
		previous = key
		e.resourceMu.Lock()
		if e.resourceLocks == nil {
			e.resourceLocks = make(map[string]chan struct{})
		}
		lock := e.resourceLocks[key]
		if lock == nil {
			lock = make(chan struct{}, 1)
			lock <- struct{}{}
			e.resourceLocks[key] = lock
		}
		e.resourceMu.Unlock()
		select {
		case <-ctx.Done():
			releaseAll()
			return nil, ctx.Err()
		case <-lock:
		}
		releases = append(releases, func() { lock <- struct{}{} })
	}
	if err := ctx.Err(); err != nil {
		releaseAll()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(releaseAll) }, nil
}

// Yielded foreground commands still own their mutation baseline and output
// capture, so retain their locks until actual completion. Explicit background
// services must allow later edits, commands and artifact reads while running.
func (e *Engine) releaseToolResources(ctx context.Context, call tool.Call, release func()) {
	if call.Name != tool.CommandRun {
		release()
		return
	}
	var ids []string
	for _, process := range e.BackgroundProcesses(call.SessionID) {
		if process.Running && process.CallID == call.CallID && process.TurnID == call.TurnID {
			if ctx.Err() != nil {
				_, _ = e.StopBackgroundProcess(call.SessionID, process.ProcessID)
			}
			ids = append(ids, process.ProcessID)
		}
	}
	var args struct {
		Background bool `json:"background"`
	}
	_ = json.Unmarshal(call.Args, &args)
	if len(ids) == 0 || args.Background {
		release()
		return
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer release()
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			running := false
			for _, process := range e.BackgroundProcesses(call.SessionID) {
				if process.Running {
					for _, id := range ids {
						if id == process.ProcessID {
							running = true
						}
					}
				}
			}
			if !running {
				return
			}
			select {
			case <-e.auxCtx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

package engine

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/tool"
)

// All sessions use these daemon-owned locks, including the main conversation.
// Permission waiting happens before acquisition, so an unanswered approval cannot
// monopolize a project or the desktop.
func (e *Engine) acquireToolResources(ctx context.Context, call tool.Call) (func(), error) {
	var keys []string
	// Computer Use already serializes native mutations in computer.Manager.
	risk, classified := tool.ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs)
	_, mutation := tool.MutationTrackingForCall(call)
	if mutation || call.Name == tool.CommandRun || classified && risk.Class != tool.RiskClassRead && risk.Scope != "computer" {
		for _, dir := range call.ProjectDirs {
			keys = append(keys, "directory:"+dir)
		}
	}
	sort.Strings(keys)
	var releases []func()
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

// A command returning a process handle has not released the shared resource.
// Keep ownership until the process manager reports actual exit/stop/removal.
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
	if len(ids) == 0 {
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

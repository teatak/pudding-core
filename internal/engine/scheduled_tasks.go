package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type ScheduledRunView struct {
	QueuedInputID string `json:"queuedInputID,omitempty"`
	SkippedCount  int    `json:"skippedCount,omitempty"`
	*store.ScheduledTaskRun
	Status      string     `json:"status"`
	TurnID      string     `json:"turnID,omitempty"`
	MessageID   string     `json:"messageID,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	Detail      string     `json:"detail,omitempty"`
	AttentionID string     `json:"attentionID,omitempty"`
}
type ScheduledTaskView struct {
	SessionArchived bool `json:"sessionArchived"`
	*store.ScheduledTask
	SessionTitle string            `json:"sessionTitle"`
	State        string            `json:"state"`
	LatestRun    *ScheduledRunView `json:"latestRun,omitempty"`
	ActiveRun    *ScheduledRunView `json:"activeRun,omitempty"`
}

func (e *Engine) CreateScheduledTask(ctx context.Context, in store.ScheduledTaskCreate) (*store.ScheduledTask, error) {
	e.scheduleMu.Lock()
	defer e.scheduleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	t, err := store.PrepareScheduledTask(in, time.Now().UTC().Truncate(time.Millisecond))
	if err != nil {
		return nil, err
	}
	var session *store.Session
	if in.NewSession != nil {
		session = &store.Session{ID: t.SessionID, Title: t.Name, Provider: in.NewSession.Provider, Model: in.NewSession.Model}
	}
	t, err = e.store.CreateScheduledTask(ctx, t, session)
	if err == nil {
		e.rememberScheduledRuntime(t.SessionID, app.RuntimeIDFromContext(ctx))
	}
	return t, err
}
func (e *Engine) UpdateScheduledTask(ctx context.Context, id string, in store.ScheduledTaskUpdate) (*store.ScheduledTask, error) {
	e.scheduleMu.Lock()
	defer e.scheduleMu.Unlock()
	return e.store.UpdateScheduledTask(ctx, id, in, time.Now().UTC().Truncate(time.Millisecond))
}
func (e *Engine) rememberScheduledRuntime(sessionID, runtimeID string) {
	if runtimeID == "" {
		return
	}
	e.mu.Lock()
	e.scheduledRuntimeIDs[sessionID] = runtimeID
	e.mu.Unlock()
}

func (e *Engine) ScheduledTasks(ctx context.Context, sessionID string, includeDeleted bool) ([]ScheduledTaskView, error) {
	tasks, err := e.store.ListScheduledTasks(ctx, sessionID, includeDeleted)
	if err != nil {
		return nil, err
	}
	sessions, err := e.store.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListAll})
	if err != nil {
		return nil, err
	}
	titles := map[string]string{}
	archived := map[string]bool{}
	for _, session := range sessions {
		titles[session.ID] = session.Title
		archived[session.ID] = session.ArchivedAt != nil
	}
	out := make([]ScheduledTaskView, 0, len(tasks))
	for _, t := range tasks {
		v := ScheduledTaskView{ScheduledTask: t, State: "enabled"}
		v.SessionTitle = titles[t.SessionID]
		v.SessionArchived = archived[t.SessionID]
		last, active, err := e.scheduledTaskLatest(ctx, t.ID)
		if err != nil {
			return nil, err
		}
		v.LatestRun, v.ActiveRun = last, active
		if t.Deleted {
			v.State = "deleted"
		} else if !t.Enabled {
			v.State = "paused"
		} else if t.NextAt == nil && active == nil {
			v.State = "ended"
		}
		out = append(out, v)
	}
	return out, nil
}
func scheduledRunBusy(v *ScheduledRunView) bool {
	return v != nil && (v.Status == "pending" || v.Status == "queued" || v.Status == "running" || v.Status == "awaiting_approval" || v.Status == "awaiting_input")
}
func (e *Engine) scheduledTaskLatest(ctx context.Context, id string) (*ScheduledRunView, *ScheduledRunView, error) {
	var latest *ScheduledRunView
	// Skipped periods must never obscure the actual outstanding execution.
	for offset := 0; ; offset += 50 {
		runs, err := e.store.ListScheduledTaskRuns(ctx, id, 50, offset)
		if err != nil {
			return nil, nil, err
		}
		for _, r := range runs {
			v, err := e.ScheduledRun(ctx, r)
			if err != nil {
				return nil, nil, err
			}
			if latest == nil {
				latest = v
			}
			if r.Handoff == "skipped" {
				continue
			}
			if scheduledRunBusy(v) {
				return latest, v, nil
			}
			return latest, nil, nil
		}
		if len(runs) < 50 {
			return latest, nil, nil
		}
	}
}
func (e *Engine) ScheduledRuns(ctx context.Context, taskID string, limit, offset int) ([]*ScheduledRunView, error) {
	runs, err := e.store.ListScheduledTaskRuns(ctx, taskID, limit, offset)
	if err != nil {
		return nil, err
	}
	out := make([]*ScheduledRunView, 0, len(runs))
	for _, r := range runs {
		v, err := e.ScheduledRun(ctx, r)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
func (e *Engine) ScheduledRun(ctx context.Context, r *store.ScheduledTaskRun) (*ScheduledRunView, error) {
	v := &ScheduledRunView{ScheduledTaskRun: r, Status: r.Handoff, Detail: r.Reason}
	if r.Handoff == "skipped" {
		v.SkippedCount = 1
		if r.SkippedThrough != nil {
			for at := r.Schedule.Next(r.ScheduledFor); at != nil && !at.After(*r.SkippedThrough); at = r.Schedule.Next(*at) {
				v.SkippedCount++
			}
		}
	}
	if r.Handoff == "skipped" || r.Handoff == "failed" {
		return v, nil
	}
	found, err := e.scheduledInputView(ctx, r.SessionID, r.ClientMessageID, v, map[string]bool{})
	if err != nil {
		return nil, err
	}
	if !found && r.Handoff == "submitted" {
		return nil, fmt.Errorf("scheduled input missing: %s", r.ID)
	}
	return v, nil
}
func (e *Engine) scheduledInputView(ctx context.Context, sessionID, clientID string, v *ScheduledRunView, seen map[string]bool) (bool, error) {
	turn, err := e.store.FindInputTurn(ctx, sessionID, clientID)
	if errors.Is(err, store.ErrNotFound) {
		input, err := e.store.FindQueuedInput(ctx, sessionID, clientID)
		if errors.Is(err, store.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if input.Status == store.QueuedInputCancelled {
			v.Status = "cancelled"
			at := input.UpdatedAt
			v.FinishedAt = &at
			return true, nil
		}
		if input.TurnID != "" {
			return e.scheduledTurnView(ctx, sessionID, input.TurnID, v, seen)
		}
		v.QueuedInputID = input.ClientMessageID
		v.Status = "queued"
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return e.scheduledTurnView(ctx, sessionID, turn.ID, v, seen)
}
func (e *Engine) scheduledTurnView(ctx context.Context, sessionID, turnID string, v *ScheduledRunView, seen map[string]bool) (bool, error) {
	if seen[turnID] {
		return true, nil
	}
	seen[turnID] = true
	t, err := e.store.GetScheduledTaskTurn(ctx, sessionID, turnID)
	if err != nil {
		return false, err
	}
	v.QueuedInputID = ""
	v.TurnID = t.ID
	v.Status = string(t.Status)
	v.Detail = t.Error
	v.AttentionID = ""
	v.FinishedAt = nil
	if v.StartedAt == nil {
		at := t.CreatedAt
		v.StartedAt = &at
	}
	for _, m := range t.Messages {
		if m.Role == store.RoleUser && m.ClientMessageID == v.ClientMessageID {
			v.MessageID = m.ID
		}
	}
	retry, err := e.store.FindRetryTurn(ctx, sessionID, t.ID)
	if err == nil {
		return e.scheduledTurnView(ctx, sessionID, retry.ID, v, seen)
	}
	if !errors.Is(err, store.ErrNotFound) {
		return false, err
	}
	if t.Status == store.TurnFailed || t.Status == store.TurnCancelled {
		at := t.UpdatedAt
		v.FinishedAt = &at
		return true, nil
	}
	// Question state comes from canonical tool results and exact answer identities.
	// Live requests supply only the in-flight state before their result is committed.
	questions := map[string]string{}
	for _, m := range t.Messages {
		for _, p := range m.Parts {
			if p.Name != tool.RequestUserInput {
				continue
			}
			id := t.ID + ":" + p.CallID
			if p.Type == store.ContentPartToolUse {
				if _, ok := questions[id]; !ok {
					questions[id] = "cancelled"
				}
			}
			if p.Type == store.ContentPartToolResult {
				var result struct {
					RequestID string `json:"requestID"`
					Status    string `json:"status"`
				}
				if json.Unmarshal([]byte(p.Content), &result) == nil && result.RequestID == id {
					questions[id] = result.Status
				}
			}
		}
	}
	if t.Status == store.TurnRunning {
		e.mu.Lock()
		for _, p := range e.inputRequests {
			if p.SessionID == sessionID && p.TurnID == t.ID {
				settleUserInputWait(p, time.Now())
				questions[p.ID] = p.Status
			}
		}
		e.mu.Unlock()
	}
	var continuations []string
	for requestID, status := range questions {
		clientID := "input-flow-" + requestID
		_, err := e.store.FindInputTurn(ctx, sessionID, clientID)
		answered := err == nil
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return false, err
		}
		if !answered {
			q, err := e.store.FindQueuedInput(ctx, sessionID, clientID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return false, err
			}
			answered = q != nil && q.Status != store.QueuedInputCancelled
		}
		if answered {
			continuations = append(continuations, clientID)
			continue
		}
		if status == "waiting" || status == "awaiting_user" || status == "timeout" {
			v.Status = "awaiting_input"
			v.AttentionID = requestID
		}
	}
	if v.Status == "awaiting_input" {
		return true, nil
	}
	for _, clientID := range continuations {
		found, err := e.scheduledInputView(ctx, sessionID, clientID, v, seen)
		if err != nil {
			return false, err
		}
		if found && scheduledRunBusy(v) {
			return true, nil
		}
	}
	for _, a := range e.PendingApprovals(sessionID) {
		if a.TurnID == v.TurnID {
			v.Status = "awaiting_approval"
			v.AttentionID = a.ID
			return true, nil
		}
	}
	if v.Status == "completed" {
		at := t.UpdatedAt
		if v.FinishedAt == nil {
			v.FinishedAt = &at
		}
	}
	return true, nil
}
func (e *Engine) RunScheduledTask(ctx context.Context, id, requestID string) (*ScheduledRunView, error) {
	if requestID == "" || len(requestID) > 256 {
		return nil, store.ErrInvalidSchedule
	}
	e.scheduleMu.Lock()
	defer e.scheduleMu.Unlock()
	t, err := e.store.GetScheduledTask(ctx, id)
	if err != nil {
		return nil, err
	}
	// A repeated manual request acknowledges its original run, including after
	// that run has completed or the plan was deleted.
	for offset := 0; ; offset += 100 {
		runs, err := e.store.ListScheduledTaskRuns(ctx, id, 100, offset)
		if err != nil {
			return nil, err
		}
		for _, r := range runs {
			if r.Key == "manual:"+requestID {
				return e.ScheduledRun(ctx, r)
			}
		}
		if len(runs) < 100 {
			break
		}
	}
	_, active, err := e.scheduledTaskLatest(ctx, id)
	if err != nil {
		return nil, err
	}
	if active != nil {
		return nil, store.ErrScheduledTaskBusy
	}
	r, err := e.store.AcceptScheduledTask(ctx, id, store.ScheduledTaskAccept{Revision: t.Revision, RequestID: requestID, Now: time.Now().UTC()})
	if err != nil {
		return nil, err
	}
	e.rememberScheduledRuntime(t.SessionID, app.RuntimeIDFromContext(ctx))
	if err := e.submitScheduledRun(ctx, r); err != nil {
		return nil, err
	}
	return e.ScheduledRun(ctx, r)
}
func (e *Engine) submitScheduledRun(ctx context.Context, r *store.ScheduledTaskRun) error {
	if r.Handoff != "pending" {
		return nil
	}
	v := &ScheduledRunView{ScheduledTaskRun: r, Status: "pending"}
	found, err := e.scheduledInputView(ctx, r.SessionID, r.ClientMessageID, v, map[string]bool{})
	if err != nil {
		return err
	}
	if !found {
		e.mu.Lock()
		runtimeID := e.scheduledRuntimeIDs[r.SessionID]
		e.mu.Unlock()
		_, err = e.Submit(app.WithRuntimeID(ctx, runtimeID), SubmitInput{SessionID: r.SessionID, ClientMessageID: r.ClientMessageID, Text: r.Prompt})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Check again before classifying an uncertain submit as failed.
			found, lookupErr := e.scheduledInputView(ctx, r.SessionID, r.ClientMessageID, v, map[string]bool{})
			if lookupErr != nil {
				return lookupErr
			}
			if !found {
				if saveErr := e.store.SetScheduledTaskHandoff(ctx, r.ID, "failed", err.Error()); saveErr != nil {
					return saveErr
				}
				r.Handoff = "failed"
				r.Reason = err.Error()
				return nil
			}
		}
	}
	if err := e.store.SetScheduledTaskHandoff(ctx, r.ID, "submitted", ""); err != nil {
		return err
	}
	r.Handoff = "submitted"
	return nil
}

// StartScheduledTasks is called only after Recover. It is not tied to an open
// page, and shares the daemon's auxiliary cancellation lifetime.
func (e *Engine) StartScheduledTasks() {
	e.scheduleStart.Do(func() {
		e.wg.Add(1)
		go func() {
			defer e.wg.Done()
			last := time.Now()
			if err := e.scheduledTick(e.auxCtx, last, true); err != nil && !errors.Is(err, context.Canceled) {
				slog.Warn("scheduler recovery", "err", err)
			}
			timer := time.NewTicker(time.Second)
			defer timer.Stop()
			for {
				select {
				case <-e.auxCtx.Done():
					return
				case <-timer.C:
					now := time.Now()
					offline := now.UnixMilli()-last.UnixMilli() > 5000 || now.Before(last)
					if err := e.scheduledTick(e.auxCtx, now, offline); err != nil && !errors.Is(err, context.Canceled) {
						slog.Warn("scheduler tick", "err", err)
					}
					last = now
				}
			}
		}()
	})
}
func (e *Engine) scheduledTick(ctx context.Context, now time.Time, offline bool) error {
	e.scheduleMu.Lock()
	defer e.scheduleMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	pending, err := e.store.PendingScheduledTaskRuns(ctx)
	if err != nil {
		return err
	}
	for _, r := range pending {
		if err := e.submitScheduledRun(ctx, r); err != nil {
			return err
		}
	}
	tasks, err := e.store.ListScheduledTasks(ctx, "", false)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if !t.Enabled || t.NextAt == nil || t.NextAt.After(now) {
			continue
		}
		skip := ""
		if offline {
			skip = "missed"
		} else {
			_, active, err := e.scheduledTaskLatest(ctx, t.ID)
			if err != nil {
				return err
			}
			if active != nil {
				skip = "busy"
			}
		}
		r, err := e.store.AcceptScheduledTask(ctx, t.ID, store.ScheduledTaskAccept{Revision: t.Revision, Now: now, SkipReason: skip})
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrScheduleConflict) {
			continue
		}
		if err != nil {
			return err
		}
		if err := e.submitScheduledRun(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

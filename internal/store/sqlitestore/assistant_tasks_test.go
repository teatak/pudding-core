package sqlitestore

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestAssistantTaskLifecycleAndExactResults(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	p, err := s.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Assistant.SessionID
	beginTestTurn(t, s, sid, "source", "source-msg", "source-client")
	work := &store.Session{ID: "work", Title: "Assigned work", Provider: "mock", Model: "mock"}
	in := store.AssignAssistantTaskInput{AssistantSessionID: sid, SourceTurnID: "source", CallID: "dispatch", Title: work.Title, Session: work, CreateSession: true, Input: store.QueueInputInput{SessionID: work.ID, ClientMessageID: "assigned-input", Text: "Original task", Provider: "mock", Model: "mock", Mode: store.ModeChat}}
	assigned, err := s.AssignAssistantTask(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := s.AssignAssistantTask(ctx, in); err != nil || duplicate.TaskID != assigned.TaskID {
		t.Fatal("dispatch not idempotent", duplicate, err)
	}
	if parent, err := s.ParentSessionID(ctx, work.ID); err != nil || parent != "" {
		t.Fatal("work must remain a root", parent, err)
	}
	if created, err := s.GetSession(ctx, work.ID); err != nil || created.ActiveMode != store.ModeChat || created.ModeLease != store.ModeLeaseNone {
		t.Fatal("work defaults must satisfy the session contract", created, err)
	}
	sessions, err := s.ListSessions(ctx)
	if err != nil || len(sessions) != 1 || sessions[0].ID != work.ID {
		t.Fatal("work missing from ordinary list", sessions, err)
	}
	if err := s.CreateChildSession(ctx, work.ID, &store.Session{ID: "work-child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal("work cannot use subagents", err)
	}
	other := in
	other.CallID = "another"
	other.Session = &store.Session{ID: "other-work", Provider: "mock", Model: "mock"}
	other.Input.SessionID = "other-work"
	other.Input.ClientMessageID = "another-input"
	if _, err := s.AssignAssistantTask(ctx, other); !errors.Is(err, store.ErrAssistantTaskBusy) {
		t.Fatal("single active task gate", err)
	}
	if _, err := s.GetSession(ctx, "other-work"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("failed admission created a phantom session", err)
	}
	if _, err := s.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: work.ID, TurnID: "assigned-turn", UserMessageID: "assigned-msg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishTurn(ctx, store.FinishTurnInput{TurnID: "assigned-turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("EXACT_ASSIGNED_RESULT")}); err != nil {
		t.Fatal(err)
	}
	beginTestTurn(t, s, work.ID, "manual-turn", "manual-msg", "manual-client")
	if _, err := s.FinishTurn(ctx, store.FinishTurnInput{TurnID: "manual-turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("UNRELATED_LATER_REPLY")}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CollectAssistantTaskResults(ctx, sid); err != nil || !changed {
		t.Fatal("result not collected", err)
	}
	msg, err := s.GetMessage(ctx, sid, "assistant_task_result_assigned-turn")
	if err != nil || !strings.Contains(msg.Text, "EXACT_ASSIGNED_RESULT") || strings.Contains(msg.Text, "UNRELATED_LATER_REPLY") {
		t.Fatal("wrong execution reported", msg, err)
	}
	if changed, err := s.CollectAssistantTaskResults(ctx, sid); err != nil || changed {
		t.Fatal("duplicate result collection", err)
	}
	follow := in
	follow.TaskID = assigned.TaskID
	follow.CreateSession = false
	follow.CallID = "follow-up"
	follow.Input.ClientMessageID = "follow-input"
	follow.Input.Text = "Continue this task"
	continued, err := s.AssignAssistantTask(ctx, follow)
	if err != nil || continued.TaskID != assigned.TaskID || continued.SessionID != work.ID {
		t.Fatal("follow-up lost identity", continued, err)
	}
	// Finish this queued follow-up as failed; its own status/result must survive restart.
	if _, err := s.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: work.ID, TurnID: "follow-turn", UserMessageID: "follow-msg"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishTurn(ctx, store.FinishTurnInput{TurnID: "follow-turn", Status: store.TurnFailed, Error: "validation failed"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if created, err := s.IsAssistantCreatedSession(ctx, sid, work.ID); err != nil || !created {
		t.Fatal("restart lost creation origin", created, err)
	}
	runs, err := s.ListAssistantTaskRuns(ctx, sid)
	if err != nil || len(runs) != 2 || runs[1].Status != "failed" || runs[1].Summary != "validation failed" {
		t.Fatal("restart lost exact task runs", runs, err)
	}
	if _, err := s.CollectAssistantTaskResults(ctx, sid); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMessage(ctx, sid, "assistant_task_result_follow-turn"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: work.ID, TurnID: "retry-turn", RetryOfTurnID: "follow-turn", ClientMessageID: "retry-client", SystemMessageID: "retry-message", Text: "retry assigned execution"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FinishTurn(ctx, store.FinishTurnInput{TurnID: "retry-turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("RETRIED_ASSIGNED_RESULT")}); err != nil {
		t.Fatal(err)
	}
	runs, err = s.ListAssistantTaskRuns(ctx, sid)
	if err != nil || runs[1].TurnID != "retry-turn" || runs[1].Status != "completed" || runs[1].ResultCollected {
		t.Fatal("explicit retry not tracked as the same assigned execution", runs, err)
	}
	if changed, err := s.CollectAssistantTaskResults(ctx, sid); err != nil || !changed {
		t.Fatal("retry result not collected", err)
	}
	if msg, err := s.GetMessage(ctx, sid, "assistant_task_result_retry-turn"); err != nil || !strings.Contains(msg.Text, "RETRIED_ASSIGNED_RESULT") {
		t.Fatal("retry result missing", msg, err)
	}
	if err := s.DeleteAssistant(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSession(ctx, work.ID); err != nil {
		t.Fatal("independent work was deleted with assistant", err)
	}
	if runs, err := s.ListAssistantTaskRuns(ctx, work.ID); err != nil || len(runs) != 0 {
		t.Fatal("assignment metadata survived assistant deletion", runs, err)
	}
}

func TestAssistantTasksRejectForeignCallsAndReportCancelledQueue(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	p, err := s.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Assistant.SessionID
	beginTestTurn(t, s, sid, "source", "source-msg", "source-client")
	createTestSession(t, s, "ordinary")
	beginTestTurn(t, s, "ordinary", "ordinary-turn", "ordinary-msg", "ordinary-client")
	in := store.AssignAssistantTaskInput{AssistantSessionID: "ordinary", SourceTurnID: "ordinary-turn", CallID: "fake", Title: "Fake", Session: &store.Session{ID: "work", Provider: "mock", Model: "mock"}, CreateSession: true, Input: store.QueueInputInput{SessionID: "work", ClientMessageID: "assigned-input", Text: "task", Provider: "mock", Model: "mock", Mode: store.ModeChat}}
	if _, err := s.AssignAssistantTask(ctx, in); !errors.Is(err, store.ErrAssistantToolUnavailable) {
		t.Fatal("foreign dispatch accepted", err)
	}
	in.AssistantSessionID = sid
	in.SourceTurnID = "source"
	run, err := s.AssignAssistantTask(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	status := store.QueuedInputCancelled
	if _, err := s.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "work", ClientMessageID: run.ClientMessageID, Status: &status}); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.CollectAssistantTaskResults(ctx, sid); err != nil || !changed {
		t.Fatal("cancelled queue not reportable", err)
	}
	msg, err := s.GetMessage(ctx, sid, run.ResultMessageID())
	if err != nil || !strings.Contains(msg.Text, "status cancelled") {
		t.Fatal("cancel status missing", msg, err)
	}
}

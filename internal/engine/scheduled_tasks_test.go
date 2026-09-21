package engine

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestScheduledTaskQueueRecoveryAndSkip(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"waiting"}), mock.WithDelay(time.Second)))
	_, err := eng.Submit(ctx, SubmitInput{SessionID: "root", ClientMessageID: "foreground", Text: "Already working"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.Cancel("root") })
	task, err := eng.CreateScheduledTask(ctx, store.ScheduledTaskCreate{SessionID: "root", RequestID: "create", Name: "Check", Prompt: "Check independently", Schedule: store.TaskSchedule{Kind: "daily", Timezone: "UTC", Time: "09:00"}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := eng.RunScheduledTask(ctx, task.ID, "manual-1")
	if err != nil || run.Status != "queued" {
		t.Fatalf("busy session queue: %+v %v", run, err)
	}
	if _, err = eng.RunScheduledTask(ctx, task.ID, "manual-2"); !errors.Is(err, store.ErrScheduledTaskBusy) {
		t.Fatalf("overlap admitted: %v", err)
	}
	same, err := eng.RunScheduledTask(ctx, task.ID, "manual-1")
	if err != nil || same.ID != run.ID {
		t.Fatal("manual replay duplicated", err)
	}
	// Simulate the accepted input being persisted while its handoff acknowledgement was lost.
	copy := *run.ScheduledTaskRun
	copy.Handoff = "pending"
	if err = eng.submitScheduledRun(ctx, &copy); err != nil {
		t.Fatal(err)
	}
	queued, _ := st.ListQueuedInputs(ctx, "root")
	if len(queued) != 1 {
		t.Fatalf("recovery duplicated queue: %d", len(queued))
	}
	if err = eng.scheduledTick(ctx, *task.NextAt, false); err != nil {
		t.Fatal(err)
	}
	views, err := eng.ScheduledTasks(ctx, "root", false)
	if err != nil || views[0].LatestRun.Reason != "busy" || views[0].ActiveRun.ID != run.ID {
		t.Fatalf("skipped cycle hid active run: %+v %v", views, err)
	}
}

func TestScheduledTaskTracksQuestionsAnswersAndRetry(t *testing.T) {
	ctx := context.Background()
	eng, st, _, sid := newTestEngine(t)
	defer eng.Stop()
	task, err := eng.CreateScheduledTask(ctx, store.ScheduledTaskCreate{SessionID: sid, RequestID: "create", Name: "Question", Prompt: "Ask", Schedule: store.TaskSchedule{Kind: "daily", Timezone: "UTC", Time: "09:00"}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := st.AcceptScheduledTask(ctx, task.ID, store.ScheduledTaskAccept{Revision: task.Revision, RequestID: "manual", Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	begin := func(id, client string) {
		t.Helper()
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: id, UserMessageID: "message_" + id, ClientMessageID: client, UserText: "input"}); err != nil {
			t.Fatal(err)
		}
	}
	finish := func(id string, status store.TurnStatus, parts []store.ContentPart) {
		t.Helper()
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: status, AssistantParts: parts}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(status, turn string) {
		t.Helper()
		v, err := eng.ScheduledRun(ctx, run)
		if err != nil || v.Status != status || v.TurnID != turn {
			t.Fatalf("run=%+v err=%v want=%s %s", v, err, status, turn)
		}
	}
	begin("original", run.ClientMessageID)
	deadline := time.Now().Add(time.Minute)
	eng.inputRequests = map[string]*pendingUserInput{sid + ":original:q": {UserInputRequest: UserInputRequest{ID: "original:q", SessionID: sid, TurnID: "original", Status: "waiting", Deadline: &deadline}, ctx: ctx, wake: make(chan struct{}, 1)}}
	check("awaiting_input", "original")
	finish("original", store.TurnCompleted, []store.ContentPart{{Type: store.ContentPartToolUse, Name: tool.RequestUserInput, CallID: "q", Args: json.RawMessage(`{"title":"question"}`)}, {Type: store.ContentPartToolResult, Name: tool.RequestUserInput, CallID: "q", Content: `{"requestID":"original:q","status":"timeout"}`}})
	eng.inputRequests = nil
	begin("unrelated", "unrelated")
	finish("unrelated", store.TurnCompleted, nil)
	check("awaiting_input", "original")
	begin("answer", "input-flow-original:q")
	finish("answer", store.TurnFailed, nil)
	check("failed", "answer")
	if _, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: sid, TurnID: "retry", RetryOfTurnID: "answer", SystemMessageID: "retry_message", ClientMessageID: "retry_client", Text: "retry"}); err != nil {
		t.Fatal(err)
	}
	finish("retry", store.TurnCompleted, nil)
	check("completed", "retry")
	if _, err := st.ArchiveSession(ctx, sid); err != nil {
		t.Fatal(err)
	}
	check("completed", "retry")
}

func TestScheduledTaskMissedAndAcceptedBeforeRestart(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"done"})))
	now := time.Now().UTC().Truncate(time.Millisecond)
	for _, id := range []string{"missed", "accepted"} {
		task, err := store.PrepareScheduledTask(store.ScheduledTaskCreate{SessionID: "root", RequestID: id, Name: id, Prompt: id, Schedule: store.TaskSchedule{Kind: "once", Timezone: "UTC"}, DelaySeconds: 60}, now)
		if err != nil {
			t.Fatal(err)
		}
		task, err = st.CreateScheduledTask(ctx, task)
		if err != nil {
			t.Fatal(err)
		}
		if id == "accepted" {
			if _, err = st.AcceptScheduledTask(ctx, task.ID, store.ScheduledTaskAccept{Revision: task.Revision, Now: now.Add(time.Minute)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := eng.scheduledTick(ctx, now.Add(2*time.Minute), true); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	if err := eng.scheduledTick(ctx, now.Add(3*time.Minute), true); err != nil {
		t.Fatal(err)
	}
	views, err := eng.ScheduledTasks(ctx, "root", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range views {
		if v.NextAt != nil || v.State != "ended" {
			t.Fatalf("once not ended: %+v", v)
		}
		want := "completed"
		if v.Name == "missed" {
			want = "skipped"
		}
		if v.LatestRun == nil || v.LatestRun.Status != want {
			t.Fatalf("%s run=%+v", v.Name, v.LatestRun)
		}
		runs, _ := st.ListScheduledTaskRuns(ctx, v.ID, 50, 0)
		if len(runs) != 1 {
			t.Fatal("duplicate after restart")
		}
	}
}

func TestScheduledTaskManualConsumesOnce(t *testing.T) {
	ctx := context.Background()
	eng, st, _ := newCollaborationEngine(t, mock.New(mock.WithScript([]string{"done"})))
	task, err := eng.CreateScheduledTask(ctx, store.ScheduledTaskCreate{SessionID: "root", RequestID: "once", Name: "Once", Prompt: "Check now", Schedule: store.TaskSchedule{Kind: "once", Timezone: "UTC"}, DelaySeconds: 3600})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.RunScheduledTask(ctx, task.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	eng.Wait()
	if err := eng.scheduledTick(ctx, *task.NextAt, false); err != nil {
		t.Fatal(err)
	}
	runs, err := st.ListScheduledTaskRuns(ctx, task.ID, 50, 0)
	if err != nil || len(runs) != 1 {
		t.Fatalf("manual run fired again at original time: %d %v", len(runs), err)
	}
	updated, _ := st.GetScheduledTask(ctx, task.ID)
	if updated.NextAt != nil {
		t.Fatal("once cursor not consumed")
	}
}

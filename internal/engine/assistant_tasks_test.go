package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type assignmentClient struct {
	mu                sync.Mutex
	assistantRequests []provider.Request
	workerRequests    []provider.Request
	releases          [2]chan struct{}
	taskID            string
}

func (c *assignmentClient) Name() string { return "assignment-test" }
func (c *assignmentClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.mu.Lock()
	isAssistant := strings.Contains(req.System, "## Personal Assistant Profile")
	var chunks []provider.Chunk
	var release chan struct{}
	if isAssistant {
		i := len(c.assistantRequests)
		c.assistantRequests = append(c.assistantRequests, req)
		if i == 0 || i == 3 {
			name := tool.AssistantTaskDispatch
			args := map[string]string{"title": "Independent task", "prompt": "WORK_ONE"}
			if i == 3 {
				name = tool.AssistantTaskSend
				args = map[string]string{"task_id": c.taskID, "prompt": "WORK_TWO"}
			}
			raw, _ := json.Marshal(args)
			chunks = []provider.Chunk{{Tool: &provider.ToolCallChunk{Index: 0, CallID: store.NewID("call"), Name: name, ArgsDelta: string(raw)}}, {Done: true, Finish: provider.FinishToolCalls}}
		} else {
			chunks = []provider.Chunk{{Part: provider.PartText, Delta: "ASSISTANT_REPORT"}, {Done: true, Finish: provider.FinishStop}}
		}
	} else {
		i := len(c.workerRequests)
		c.workerRequests = append(c.workerRequests, req)
		if i < len(c.releases) {
			release = c.releases[i]
		}
		chunks = []provider.Chunk{{Part: provider.PartText, Delta: "EXACT_WORK_RESULT"}, {Done: true, Finish: provider.FinishStop}}
	}
	c.mu.Unlock()
	out := make(chan provider.Chunk, len(chunks))
	go func() {
		defer close(out)
		if release != nil {
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
		}
		for _, chunk := range chunks {
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func TestAssistantTaskDispatchReportAndFollowUp(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	client := &assignmentClient{releases: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
	eng := New(db, event.NewHub(), mapResolver{"assign": client}, db)
	t.Cleanup(func() { eng.Stop(); eng.Wait() })
	if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "assign", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	p, err := db.OpenAssistant(ctx, "小布", "assign", "test", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Assistant.SessionID
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "assign", Text: "Arrange work"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, db, sid)
	tasks, err := eng.ListAssistantTasks(ctx, sid)
	if err != nil || len(tasks) != 1 {
		t.Fatal("missing task", tasks, err)
	}
	task := tasks[0]
	client.mu.Lock()
	client.taskID = task.TaskID
	client.mu.Unlock()
	if parent, err := db.ParentSessionID(ctx, task.SessionID); err != nil || parent != "" {
		t.Fatal("work is not independent", parent, err)
	}
	waitReports := func(count int) {
		t.Helper()
		end := time.Now().Add(5 * time.Second)
		for time.Now().Before(end) {
			client.mu.Lock()
			n := len(client.assistantRequests)
			client.mu.Unlock()
			if n >= count {
				waitTurnDone(t, db, sid)
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("assistant was not woken for completed work")
	}
	close(client.releases[0])
	waitReports(3)
	tasks, err = eng.ListAssistantTasks(ctx, sid)
	if err != nil || tasks[0].Status != "completed" || !tasks[0].ResultCollected {
		t.Fatal("completion not reported", tasks, err)
	}
	firstTurn := tasks[0].TurnID
	if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "follow", Text: "Continue that work"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, db, sid)
	close(client.releases[1])
	waitReports(6)
	tasks, err = eng.ListAssistantTasks(ctx, sid)
	if err != nil || len(tasks) != 1 || tasks[0].SessionID != task.SessionID || tasks[0].TurnID == firstTurn || !tasks[0].ResultCollected {
		t.Fatal("follow-up lost identity or result", tasks, err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.workerRequests) != 2 {
		t.Fatal("duplicate work execution", len(client.workerRequests))
	}
	for _, req := range client.workerRequests {
		for _, def := range req.Tools {
			if tool.IsAssistantTool(def.Name) {
				t.Fatal("assistant tool leaked to worker", def.Name)
			}
		}
		if strings.Contains(req.System, "## Personal Assistant Profile") {
			t.Fatal("private profile leaked")
		}
	}
	for _, i := range []int{2, 5} {
		found := false
		for _, msg := range client.assistantRequests[i].Messages {
			if strings.Contains(msg.Text, "Assigned work result:") && strings.Contains(msg.Text, "EXACT_WORK_RESULT") {
				found = true
			}
		}
		if !found {
			t.Fatal("report lacks canonical result", i)
		}
	}
}

func TestAssistantTaskRecoveryReportsSettledExecutionOnce(t *testing.T) {
	for _, status := range []store.TurnStatus{store.TurnFailed, store.TurnCancelled} {
		t.Run(string(status), func(t *testing.T) {
			ctx := context.Background()
			db := storetest.New(t)
			if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
				t.Fatal(err)
			}
			p, err := db.OpenAssistant(ctx, "小布", "capture", "test", store.AssistantAppearance{"version": 1})
			if err != nil {
				t.Fatal(err)
			}
			sid := p.Assistant.SessionID
			if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "source", UserMessageID: "source-msg", ClientMessageID: "source-client", UserText: "Assign work"}); err != nil {
				t.Fatal(err)
			}
			run, err := db.AssignAssistantTask(ctx, store.AssignAssistantTaskInput{AssistantSessionID: sid, SourceTurnID: "source", CallID: "assign", Title: "Recover work", CreateSession: true, Session: &store.Session{ID: "work", Provider: "capture", Model: "test"}, Input: store.QueueInputInput{SessionID: "work", ClientMessageID: "assigned", Text: "Do work", Provider: "capture", Model: "test", Mode: store.ModeChat}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "source", Status: store.TurnCompleted}); err != nil {
				t.Fatal(err)
			}
			if status == store.TurnFailed {
				if _, err := db.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "work", TurnID: "worker-turn", UserMessageID: "worker-msg"}); err != nil {
					t.Fatal(err)
				}
				// Recover itself must settle the interrupted worker before reporting it.
			} else {
				cancelled := store.QueuedInputCancelled
				if _, err := db.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "work", ClientMessageID: run.ClientMessageID, Status: &cancelled}); err != nil {
					t.Fatal(err)
				}
			}
			client := &nextPromptClient{}
			eng := New(db, event.NewHub(), mapResolver{"capture": client}, db)
			t.Cleanup(func() { eng.Stop(); eng.Wait() })
			if err := eng.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, db, sid)
			if err := eng.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, db, sid)
			requests := client.snapshot()
			if len(requests) != 1 {
				t.Fatal("restart duplicated work or reporting", len(requests))
			}
			found := false
			for _, m := range requests[0].Messages {
				if strings.Contains(m.Text, "Assigned work result:") && strings.Contains(m.Text, "status "+string(status)) {
					found = true
				}
			}
			if !found {
				t.Fatal("recovery did not report canonical failed/cancelled status")
			}
		})
	}
}

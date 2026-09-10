package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
)

func inputAnswer() UserInputAction {
	return UserInputAction{Action: "answer", Text: "已填写测试：确认", Parts: []store.ContentPart{{Type: store.ContentPartText, Text: "已填写测试：确认"}}}
}

func waitInputRequest(t *testing.T, e *Engine, sessionID, requestID string) *UserInputRequest {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if req, err := e.UserInputRequest(context.Background(), sessionID, requestID); err == nil {
			return req
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("question did not become available")
	return nil
}

func TestUserInputWaitRenewAnswerTimeoutAndCancel(t *testing.T) {
	for _, scenario := range []string{"answer", "timeout", "cancel", "dismiss", "async"} {
		t.Run(scenario, func(t *testing.T) {
			e, st, _, sid := newTestEngine(t)
			e.tools = &recordingToolRunner{result: tool.Result{Ok: true, Content: `{"status":"awaiting_user"}`}}
			t.Cleanup(e.Stop)
			if _, err := st.BeginTurn(context.Background(), store.BeginTurnInput{SessionID: sid, TurnID: "turn", UserMessageID: "initial", ClientMessageID: "initial", UserText: "ask"}); err != nil {
				t.Fatal(err)
			}
			e.running[sid] = newActiveTurn("turn", func() {})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			wait := 1
			if scenario == "async" {
				wait = 0
			}
			call := tool.Call{SessionID: sid, TurnID: "turn", CallID: "question", Name: tool.RequestUserInput, Args: json.RawMessage(fmt.Sprintf(`{"title":"test","waitSeconds":%d}`, wait))}
			result := make(chan tool.Result, 1)
			go func() { result <- e.requestUserInput(ctx, call) }()
			initial := waitInputRequest(t, e, sid, "turn:question")
			if _, err := e.ActOnUserInput(ctx, "another-session", "turn:question", inputAnswer()); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("cross-session answer: %v", err)
			}
			if scenario == "answer" {
				time.Sleep(80 * time.Millisecond)
				touched, err := e.ActOnUserInput(ctx, sid, initial.ID, UserInputAction{Action: "touch"})
				if err != nil || !touched.Request.Deadline.After(*initial.Deadline) {
					t.Fatalf("deadline not renewed: %+v %v", touched, err)
				}
				select {
				case <-result:
					t.Fatal("model continued without answer")
				default:
				}
				answered, err := e.ActOnUserInput(ctx, sid, initial.ID, inputAnswer())
				if err != nil || answered.SubmitResult == nil || answered.UserMessageID == "" {
					t.Fatalf("answer delivery: %+v %v", answered, err)
				}
				duplicate, err := e.ActOnUserInput(ctx, sid, initial.ID, inputAnswer())
				if err != nil || duplicate.Request.Status != "answered" {
					t.Fatal("answer retry must not submit another message")
				}
			} else if scenario == "cancel" {
				cancel()
			} else if scenario == "dismiss" {
				_, _ = e.ActOnUserInput(ctx, sid, initial.ID, UserInputAction{Action: "dismiss"})
			}
			var got tool.Result
			select {
			case got = <-result:
			case <-time.After(2 * time.Second):
				t.Fatal("wait did not finish")
			}
			expected := map[string]string{"answer": "answered", "timeout": "timeout", "cancel": "cancelled", "dismiss": "dismissed", "async": "awaiting_user"}[scenario]
			if !got.Ok || !strings.Contains(got.Content, `"status":"`+expected+`"`) {
				t.Fatalf("wrong result: %+v", got)
			}
			if strings.Contains(got.Content, "已填写测试") || strings.Contains(got.Content, `"answer":`) {
				t.Fatal("answer must only exist in the user message, not the tool result")
			}
			req, err := e.ActOnUserInput(context.Background(), sid, initial.ID, UserInputAction{Action: "touch"})
			if err != nil || req.Request.Deadline != nil || req.Request.Status != expected {
				t.Fatalf("ended wait revived: %+v %v", req, err)
			}
			messages, _ := st.ListMessages(context.Background(), sid, 0)
			wantMessages := 1
			if scenario == "answer" {
				wantMessages++
			}
			if len(messages) != wantMessages {
				t.Fatalf("messages=%d, want %d", len(messages), wantMessages)
			}
		})
	}
}

func TestUserInputDefaultAndInvalidWait(t *testing.T) {
	e, _, _, sid := newTestEngine(t)
	t.Cleanup(e.Stop)
	runner := &recordingToolRunner{result: tool.Result{Ok: true}}
	e.tools = runner
	for _, value := range []string{"-1", "301", "1.5", "\"10\""} {
		got := e.requestUserInput(context.Background(), tool.Call{SessionID: sid, Name: tool.RequestUserInput, Args: json.RawMessage(`{"waitSeconds":` + value + `}`)})
		if got.Ok {
			t.Fatalf("accepted %s", value)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatal("invalid wait displayed a question")
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan tool.Result, 1)
	go func() {
		result <- e.requestUserInput(ctx, tool.Call{SessionID: sid, TurnID: "t", CallID: "default", Name: tool.RequestUserInput, Args: json.RawMessage(`{"title":"default"}`)})
	}()
	req := waitInputRequest(t, e, sid, "t:default")
	if remaining := time.Until(*req.Deadline); remaining < 59*time.Second || remaining > 60*time.Second {
		t.Fatalf("default=%v", remaining)
	}
	cancel()
	<-result
}

func TestUserInputCancellationIsVisibleBeforeUIRPCReturns(t *testing.T) {
	e, _, _, sid := newTestEngine(t)
	t.Cleanup(e.Stop)
	shown, release := make(chan struct{}), make(chan struct{})
	e.tools = &recordingToolRunner{result: tool.Result{Ok: true}, callFunc: func(tool.Call) { close(shown); <-release }}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan tool.Result, 1)
	go func() {
		result <- e.requestUserInput(ctx, tool.Call{SessionID: sid, TurnID: "t", CallID: "cancel", Name: tool.RequestUserInput, Args: json.RawMessage(`{"title":"cancel"}`)})
	}()
	<-shown
	defer func() { close(release); <-result }()
	cancel()
	got, err := e.ActOnUserInput(context.Background(), sid, "t:cancel", UserInputAction{Action: "touch"})
	if err != nil || got.Request.Status != "cancelled" || got.Request.Deadline != nil {
		t.Fatalf("cancelled wait renewed before UI RPC returned: %+v %v", got, err)
	}
}

func TestUserInputModelLoopAndCanonicalRecovery(t *testing.T) {
	ctx := context.Background()
	shown := make(chan tool.Call, 1)
	next := make(chan provider.Request, 1)
	client := &backgroundEngineClient{step: func(_ context.Context, req provider.Request, step int) (<-chan provider.Chunk, error) {
		if step == 1 {
			return smokeToolStream("question", tool.RequestUserInput, `{"title":"Confirm","type":"form","steps":[{"id":"choice","type":"text_input","title":"Choice"}],"waitSeconds":60}`), nil
		}
		next <- req
		return smokeTextStream("done"), nil
	}}
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "input.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	config := memstore.New()
	runner := &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.RequestUserInput, Capability: store.ModeChat}}, result: tool.Result{Ok: true}, callFunc: func(call tool.Call) { shown <- call }}
	e := New(st, event.NewHub(), registry.Static(client), config, WithTools(runner))
	t.Cleanup(func() { e.Wait(); e.Stop() })
	_ = st.CreateSession(ctx, &store.Session{ID: "s", Title: "Question test", Provider: "mock", Model: "mock"})
	_ = config.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock"}}})
	if _, err := e.Submit(ctx, SubmitInput{SessionID: "s", ClientMessageID: "start", Text: "ask"}); err != nil {
		t.Fatal(err)
	}
	var call tool.Call
	select {
	case call = <-shown:
	case <-time.After(2 * time.Second):
		t.Fatal("UI not called")
	}
	id := call.TurnID + ":" + call.CallID
	select {
	case <-next:
		t.Fatal("model advanced before answer")
	case <-time.After(40 * time.Millisecond):
	}
	if _, err := e.ActOnUserInput(ctx, "s", id, inputAnswer()); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-next:
		answers := 0
		for i, message := range req.Messages {
			for _, part := range message.Parts {
				if part.Type == provider.PartToolResult && strings.Contains(part.Content, "已填写测试") {
					t.Fatal("tool result contains user answer")
				}
			}
			if message.Role == provider.RoleUser && strings.Contains(message.Text, "已填写测试") {
				answers++
				if i == 0 || len(req.Messages[i-1].Parts) == 0 || req.Messages[i-1].Parts[len(req.Messages[i-1].Parts)-1].Type != provider.PartToolResult {
					t.Fatalf("answer must immediately follow tool result: %+v", req.Messages)
				}
			}
		}
		if answers != 1 {
			t.Fatalf("model must receive one user answer, got %d: %+v", answers, req.Messages)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("model did not resume")
	}
	waitTurnDone(t, st, "s")
	e2 := New(st, event.NewHub(), registry.Static(client), config)
	t.Cleanup(e2.Stop)
	recovered, err := e2.UserInputRequest(ctx, "s", id)
	if err != nil || recovered.Status != "answered" || !strings.Contains(string(recovered.Args), "Choice") {
		t.Fatalf("canonical recovery: %+v %v", recovered, err)
	}
	if _, err := e2.ActOnUserInput(ctx, "s", id, inputAnswer()); err != nil {
		t.Fatal(err)
	}
	messages, _ := st.ListMessages(ctx, "s", 0)
	users := 0
	for i, m := range messages {
		if m.Role == store.RoleUser {
			users++
			if m.ClientMessageID == "input-flow-"+id {
				if i == 0 || messages[i-1].Role != store.RoleTool || messages[i-1].Parts[0].CallID != call.CallID {
					t.Fatalf("canonical user bubble must follow the tool result: %+v", messages)
				}
			}
		}
	}
	if users != 2 {
		t.Fatalf("synchronous/retried answer duplicated user input: %d", users)
	}
}

func TestLateUserInputRestoresCanonicalAndQueuesBehindOtherTurn(t *testing.T) {
	for _, sqlite := range []bool{false, true} {
		for _, drainCancelled := range []bool{false, true} {
			t.Run(fmt.Sprintf("sqlite=%v/drained=%v", sqlite, drainCancelled), func(t *testing.T) { testLateUserInputQueue(t, sqlite, drainCancelled) })
		}
	}
}

func testLateUserInputQueue(t *testing.T, sqlite, drainCancelled bool) {
	ctx := context.Background()
	e, mem, _, sid := newTestEngine(t)
	var st store.Store = mem
	if sqlite {
		db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "queue.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		session, _ := mem.GetSession(ctx, sid)
		if err := db.CreateSession(ctx, session); err != nil {
			t.Fatal(err)
		}
		st, e.store = db, db
	}
	t.Cleanup(e.Stop)
	_, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "original", UserMessageID: "u", ClientMessageID: "initial", UserText: "ask"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "original", Parts: []store.ContentPart{
		{Type: store.ContentPartToolUse, CallID: "q", Name: tool.RequestUserInput, Args: json.RawMessage(`{"title":"old question","type":"form","steps":[{"id":"x","title":"x","type":"text_input"}]}`)},
		{Type: store.ContentPartToolResult, CallID: "q", Name: tool.RequestUserInput, Ok: true, Content: `{"requestID":"original:q","status":"timeout"}`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "original", Status: store.TurnCompleted})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "other", UserMessageID: "other-u", ClientMessageID: "other", UserText: "unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	e.running[sid] = newActiveTurn("other", func() {})
	answer, err := e.ActOnUserInput(ctx, sid, "original:q", inputAnswer())
	if err != nil || !answer.Queued {
		t.Fatalf("late answer should queue, not steer unrelated turn: %+v %v", answer, err)
	}
	snapshot, err := e.UserInputRequest(ctx, sid, "original:q")
	if err != nil || snapshot.Status != "answered" {
		t.Fatalf("queued answer status: %+v %v", snapshot, err)
	}
	_, err = e.ActOnUserInput(ctx, sid, "original:q", inputAnswer())
	if err != nil {
		t.Fatal(err)
	}
	queued, _ := st.ListQueuedInputs(ctx, sid)
	if len(queued) != 1 {
		t.Fatalf("duplicate late answer: %d", len(queued))
	}
	if len(e.running[sid].consumeSteers()) != 0 {
		t.Fatal("late answer steered unrelated task")
	}
	if drainCancelled {
		if next, err := e.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "next-task", Text: "next ordinary task"}); err != nil || !next.Queued {
			t.Fatalf("queue next task: %+v %v", next, err)
		}
	}
	cancelled := store.QueuedInputCancelled
	if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: sid, ClientMessageID: "input-flow-original:q", Status: &cancelled}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = e.UserInputRequest(ctx, sid, "original:q")
	if err != nil || snapshot.Status == "answered" {
		t.Fatalf("cancelled queue answer cannot be reopened: %+v %v", snapshot, err)
	}
	activeTurn := "other"
	if drainCancelled {
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: activeTurn, Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
		next, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: sid, TurnID: "next-task-turn", UserMessageID: "next-task-message"})
		if err != nil || next.Input.ClientMessageID != "next-task" {
			t.Fatalf("withdrawn answer must be skipped: %+v %v", next, err)
		}
		activeTurn = next.Turn.ID
		e.running[sid] = newActiveTurn(activeTurn, func() {})
		snapshot, err = e.UserInputRequest(ctx, sid, "original:q")
		if err != nil || snapshot.Status == "answered" {
			t.Fatalf("skipped answer was never delivered: %+v %v", snapshot, err)
		}
	}
	newAnswer := UserInputAction{Action: "answer", Text: "updated answer", Parts: store.TextPart("updated answer")}
	answer, err = e.ActOnUserInput(ctx, sid, "original:q", newAnswer)
	if err != nil || !answer.Queued || answer.Duplicate || answer.Request.Status != "answered" {
		t.Fatalf("explicit new answer must reactivate cancelled queue item: %+v %v", answer, err)
	}
	queued, err = st.ListQueuedInputs(ctx, sid)
	if err != nil || len(queued) != 1 || queued[0].Text != newAnswer.Text || queued[0].Status != store.QueuedInputQueued {
		t.Fatalf("acknowledged re-answer must really be queued: %+v %v", queued, err)
	}
	if queued[0].ClientMessageID != "input-flow-original:q" {
		t.Fatal("re-answer changed the question's idempotent identity")
	}
	if _, err := e.ActOnUserInput(ctx, sid, "original:q", newAnswer); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: activeTurn, Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	delivered, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: sid, TurnID: "answer-turn", UserMessageID: "answer-message"})
	if err != nil || delivered.UserMessage.Text != newAnswer.Text || delivered.UserMessage.ClientMessageID != "input-flow-original:q" {
		t.Fatalf("new answer must be delivered, not the withdrawn body: %+v %v", delivered, err)
	}
	e.running[sid] = newActiveTurn(delivered.Turn.ID, func() {})
	if _, err := e.ActOnUserInput(ctx, sid, "original:q", newAnswer); err != nil {
		t.Fatal(err)
	}
	queued, err = st.ListQueuedInputs(ctx, sid)
	if err != nil || len(queued) != 0 {
		t.Fatalf("delivered answer retry must not requeue: %+v %v", queued, err)
	}
	messages, err := st.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	answers := 0
	for _, message := range messages {
		if message.ClientMessageID == "input-flow-original:q" {
			answers++
		}
	}
	if answers != 1 {
		t.Fatalf("expected one canonical answer, got %d", answers)
	}
}

func TestUserInputAcknowledgedAnswerSurvivesDisplayFailureAndCancellation(t *testing.T) {
	for _, cancelAfterAnswer := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%v", cancelAfterAnswer), func(t *testing.T) {
			e, st, _, sid := newTestEngine(t)
			t.Cleanup(e.Stop)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "turn", UserMessageID: "u", ClientMessageID: "initial", UserText: "ask"}); err != nil {
				t.Fatal(err)
			}
			e.running[sid] = newActiveTurn("turn", cancel)
			shown, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			e.tools = &recordingToolRunner{result: tool.Result{Ok: false, Content: "display acknowledgement lost"}, callFunc: func(tool.Call) { close(shown); <-release }}
			done := make(chan tool.Result, 1)
			go func() {
				defer close(done)
				done <- e.requestUserInput(ctx, tool.Call{SessionID: sid, TurnID: "turn", CallID: "q", Name: tool.RequestUserInput, Args: json.RawMessage(`{"title":"question","waitSeconds":60}`)})
			}()
			<-shown
			t.Cleanup(func() { unblock(); <-done })
			reply, err := e.ActOnUserInput(ctx, sid, "turn:q", inputAnswer())
			if err != nil || reply.SubmitResult == nil || reply.UserMessageID == "" {
				t.Fatalf("answer not accepted as a user message: %+v %v", reply, err)
			}
			if cancelAfterAnswer {
				cancel()
			}
			messages, err := st.ListMessages(context.Background(), sid, 0)
			if err != nil || len(messages) != 2 || messages[1].Text != inputAnswer().Text {
				t.Fatalf("acknowledged answer must already be durable: %+v %v", messages, err)
			}
			// Allow the failed UI acknowledgement to arrive after the answer.
			unblock()
			result := <-done
			if !result.Ok || strings.Contains(result.Content, `"answer":`) || !strings.Contains(result.Content, `"status":"answered"`) {
				t.Fatalf("acknowledgement failure overwrote accepted answer status: %+v", result)
			}
		})
	}
}

type inputSnapshotBarrierKey struct{}

type inputSnapshotBarrierStore struct {
	store.Store
	ready, release chan struct{}
}

func (s *inputSnapshotBarrierStore) ListQueuedInputs(ctx context.Context, sessionID string) ([]*store.QueuedInput, error) {
	inputs, err := s.Store.ListQueuedInputs(ctx, sessionID)
	if ctx.Value(inputSnapshotBarrierKey{}) != nil {
		close(s.ready)
		<-s.release
	}
	return inputs, err
}

func TestLateUserInputConcurrentRetryAcrossTurnEnd(t *testing.T) {
	e, st, _, sid := newTestEngine(t)
	t.Cleanup(e.Stop)
	ctx := context.Background()
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "original", UserMessageID: "u", ClientMessageID: "initial", UserText: "ask"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "original", Parts: []store.ContentPart{
		{Type: store.ContentPartToolUse, CallID: "q", Name: tool.RequestUserInput, Args: json.RawMessage(`{"title":"question","waitSeconds":0}`)},
		{Type: store.ContentPartToolResult, CallID: "q", Name: tool.RequestUserInput, Ok: true, Content: `{"requestID":"original:q","status":"awaiting_user"}`},
	}}); err != nil {
		t.Fatal(err)
	}
	e.running[sid] = newActiveTurn("original", func() {})
	barrier := &inputSnapshotBarrierStore{Store: st, ready: make(chan struct{}), release: make(chan struct{})}
	e.store = barrier
	first, second := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := e.ActOnUserInput(context.WithValue(ctx, inputSnapshotBarrierKey{}, true), sid, "original:q", inputAnswer())
		first <- err
	}()
	<-barrier.ready
	go func() {
		_, err := e.ActOnUserInput(ctx, sid, "original:q", inputAnswer())
		second <- err
	}()
	finish := func() {
		t.Helper()
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "original", Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
		e.clearRunning(sid, "original")
	}
	secondFinished := false
	select {
	case err := <-second:
		if err != nil {
			t.Fatal(err)
		}
		// Without serialized routing the second answer can finish the original
		// turn while the first still holds an unanswered snapshot.
		secondFinished = true
		finish()
	case <-time.After(30 * time.Millisecond):
	}
	close(barrier.release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !secondFinished {
		finish()
		if err := <-second; err != nil {
			t.Fatal(err)
		}
	}
	messages, _ := st.ListMessages(ctx, sid, 0)
	count := 0
	for _, message := range messages {
		if message.ClientMessageID == "input-flow-original:q" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("concurrent answers duplicated across steer/submit boundary: %d", count)
	}
}

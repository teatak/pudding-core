package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
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
				if err != nil || answered.Delivery != "tool" {
					t.Fatalf("answer delivery: %+v %v", answered, err)
				}
				duplicate, err := e.ActOnUserInput(ctx, sid, initial.ID, inputAnswer())
				if err != nil || duplicate.Delivery != "tool" {
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
			if scenario == "answer" && !strings.Contains(got.Content, "已填写测试") {
				t.Fatal("missing actual answer")
			}
			req, err := e.ActOnUserInput(context.Background(), sid, initial.ID, UserInputAction{Action: "touch"})
			if err != nil || req.Request.Deadline != nil || req.Request.Status != expected {
				t.Fatalf("ended wait revived: %+v %v", req, err)
			}
			messages, _ := st.ListMessages(context.Background(), sid, 0)
			if len(messages) != 0 {
				t.Fatal("synchronous answer was also submitted as user message")
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
	st := memstore.New()
	runner := &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.RequestUserInput, Capability: store.ModeChat}}, result: tool.Result{Ok: true}, callFunc: func(call tool.Call) { shown <- call }}
	e := New(st, event.NewHub(), registry.Static(client), st, WithTools(runner))
	t.Cleanup(e.Stop)
	_ = st.CreateSession(ctx, &store.Session{ID: "s", Title: "Question test", Provider: "mock", Model: "mock"})
	_ = st.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "mock"}}})
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
		raw, _ := json.Marshal(req)
		if !strings.Contains(string(raw), "已填写测试") {
			t.Fatal("model did not receive answer")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("model did not resume")
	}
	waitTurnDone(t, st, "s")
	e2 := New(st, event.NewHub(), registry.Static(client), st)
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
	for _, m := range messages {
		if m.Role == store.RoleUser {
			users++
		}
	}
	if users != 1 {
		t.Fatalf("synchronous/retried answer duplicated user input: %d", users)
	}
}

func TestLateUserInputRestoresCanonicalAndQueuesBehindOtherTurn(t *testing.T) {
	ctx := context.Background()
	e, st, _, sid := newTestEngine(t)
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
	if err != nil || !answer.Queued || answer.Delivery != "message" {
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
}

package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/computer"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

// These tests use the real Engine, App loading, session/App approval, tool
// decoder and Manager. Only the native service is replaced with controlled
// barriers. They do not claim to validate macOS event delivery or TCC.
type backgroundEngineClient struct {
	calls atomic.Int32
	step  func(context.Context, provider.Request, int) (<-chan provider.Chunk, error)
}

func (c *backgroundEngineClient) Name() string { return "background-engine-test" }
func (c *backgroundEngineClient) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	return c.step(ctx, req, int(c.calls.Add(1)))
}

func backgroundBatchClient(actions int) *backgroundEngineClient {
	return &backgroundEngineClient{step: func(_ context.Context, req provider.Request, step int) (<-chan provider.Chunk, error) {
		switch step {
		case 1:
			return smokeToolStream("load", tool.AppLoad, `{"app_id":"computer-use"}`), nil
		case 2:
			if !smokeHasToolDef(req.Tools, tool.ComputerAct) {
				return nil, fmt.Errorf("Computer Use not loaded")
			}
			items := make([]map[string]any, actions)
			for index := range items {
				items[index] = map[string]any{"type": "click", "delivery": "background", "x": float64(index+1) / 10, "y": 0.5}
			}
			args, _ := json.Marshal(map[string]any{"appID": "com.apple.iCal", "windowID": 42, "actions": items})
			return smokeToolStream("batch", tool.ComputerAct, string(args)), nil
		default:
			return smokeTextStream("Finished; no automatic replay."), nil
		}
	}}
}

type backgroundEngineService struct {
	computer.Service
	pointer func(context.Context, string, computer.PointerInput) (computer.NativeAction, error)
}

func (s *backgroundEngineService) Pointer(ctx context.Context, sessionID, appID string, windowID uint32, input computer.PointerInput) (computer.NativeAction, error) {
	if appID != "com.apple.iCal" || windowID != 42 || input.Delivery != "background" {
		return computer.NativeAction{}, fmt.Errorf("wrong native target or delivery")
	}
	return s.pointer(ctx, sessionID, input)
}

func completedBackgroundClick(input computer.PointerInput) computer.NativeAction {
	return computer.NativeAction{AppID: "com.apple.iCal", Action: input.Action, Completed: true,
		Delivery: input.Delivery, X: &input.X, Y: &input.Y, Button: input.Button, ClickCount: input.ClickCount}
}

// Entry is observed after Engine approval, immediately before the real Manager
// acquires its write lock. This lets the test cancel a queued session without
// relying on sleeps or adding a production test hook.
type backgroundEngineController struct {
	computer.Controller
	entered chan string
}

func (c *backgroundEngineController) Act(ctx context.Context, sessionID, appID string, windowID uint32, actions []computer.ActionInput) (computer.ActionsResult, error) {
	c.entered <- sessionID
	return c.Controller.Act(ctx, sessionID, appID, windowID, actions)
}

type backgroundEngineHarness struct {
	engine    *Engine
	store     *memstore.Memstore
	hub       *event.Hub
	runner    *tool.BuiltinRunner
	apps      *app.Service
	entry     <-chan string
	terminal  map[string]chan event.Event
	mu        sync.Mutex
	events    []event.Event
	errors    []error
	approvals map[string]int
}

func newBackgroundEngineHarness(t *testing.T, service computer.Service, clients map[string]provider.Client) *backgroundEngineHarness {
	t.Helper()
	controller := &backgroundEngineController{Controller: computer.NewManager(service), entered: make(chan string, 16)}
	h := &backgroundEngineHarness{store: memstore.New(), hub: event.NewHub(), apps: app.NewService(t.TempDir(), nil),
		runner: tool.NewBuiltinRunner(tool.WithComputer(controller), tool.WithHomeDir(t.TempDir())),
		entry:  controller.entered, terminal: map[string]chan event.Event{}, approvals: map[string]int{}}
	h.engine = New(h.store, h.hub, mapResolver(clients), h.store, WithTools(h.runner), WithApps(h.apps))
	var subscribers sync.WaitGroup
	t.Cleanup(subscribers.Wait)
	for sessionID := range clients {
		ctx := context.Background()
		if err := h.store.CreateSession(ctx, &store.Session{ID: sessionID, Title: "Computer Use test", Provider: sessionID, Model: "test-model",
			ActiveMode: store.ModeWork, ModeLease: store.ModeLeaseSession}); err != nil {
			t.Fatal(err)
		}
		if err := h.store.PutProviderProfile(ctx, &store.ProviderProfile{DisplayName: sessionID, Protocol: "openai-compatible",
			Models: []store.ProviderModel{{ID: "test-model", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
			t.Fatal(err)
		}
		terminal := make(chan event.Event, 4)
		h.terminal[sessionID] = terminal
		events, unsubscribe := h.hub.Subscribe(sessionID)
		t.Cleanup(unsubscribe)
		subscribers.Add(1)
		go func() {
			defer subscribers.Done()
			for ev := range events {
				h.mu.Lock()
				h.events = append(h.events, ev)
				h.mu.Unlock()
				if ev.Kind == event.ApprovalRequested {
					var payload struct {
						AppID string `json:"appID"`
					}
					err := json.Unmarshal(ev.Payload, &payload)
					if err == nil && (ev.ApprovalKind != ApprovalKindToolCall || payload.AppID != "com.apple.iCal") {
						err = fmt.Errorf("unexpected approval: %+v", ev)
					}
					if err == nil {
						err = h.engine.ApproveApproval(ctx, sessionID, ev.ApprovalID, ApprovalScopeSession, nil)
					}
					h.mu.Lock()
					h.approvals[sessionID]++
					if err != nil {
						h.errors = append(h.errors, err)
					}
					h.mu.Unlock()
				}
				if ev.Kind == event.TurnCompleted || ev.Kind == event.TurnCancelled || ev.Kind == event.TurnFailed {
					terminal <- ev
				}
			}
		}()
	}
	// Registered last: stop turns before unsubscribing, so barriers see cancellation.
	t.Cleanup(func() {
		for sessionID := range clients {
			_ = h.engine.Cancel(sessionID)
		}
		h.engine.Stop()
		h.engine.Wait()
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, err := range h.errors {
			t.Error(err)
		}
	})
	return h
}

func awaitBackground[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for Computer Use test barrier")
		var zero T
		return zero
	}
}

func (h *backgroundEngineHarness) submit(t *testing.T, sessionID string) {
	t.Helper()
	if _, err := h.engine.Submit(context.Background(), SubmitInput{SessionID: sessionID, ClientMessageID: "input", Text: "Run the fixture batch."}); err != nil {
		t.Fatal(err)
	}
}

func (h *backgroundEngineHarness) result(t *testing.T, sessionID string) string {
	t.Helper()
	messages, err := h.store.ListMessages(context.Background(), sessionID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var results []string
	for _, message := range messages {
		for _, part := range message.Parts {
			if part.Type == store.ContentPartToolResult && part.Name == tool.ComputerAct {
				results = append(results, part.Content)
			}
		}
	}
	if len(results) != 1 {
		t.Fatalf("canonical Computer Use results = %d, want one (completed prefix must survive cancellation)", len(results))
	}
	return results[0]
}

func TestBackgroundEngineCancelPreservesCompletedPrefix(t *testing.T) {
	for _, outcome := range []string{"unknown", "not_started", "completed_during_cancel"} {
		t.Run(outcome, func(t *testing.T) {
			entered := make(chan struct{})
			var calls atomic.Int32
			service := &backgroundEngineService{pointer: func(ctx context.Context, _ string, input computer.PointerInput) (computer.NativeAction, error) {
				index := calls.Add(1)
				if index == 1 {
					return completedBackgroundClick(input), nil
				}
				if index > 2 {
					return computer.NativeAction{}, fmt.Errorf("later action dispatched after cancellation")
				}
				close(entered)
				<-ctx.Done()
				if outcome == "completed_during_cancel" {
					return completedBackgroundClick(input), nil
				}
				return computer.NativeAction{}, &computer.OperationError{Code: "computer_action_cancelled", Message: "cancelled at native barrier", Outcome: outcome}
			}}
			client := backgroundBatchClient(3)
			h := newBackgroundEngineHarness(t, service, map[string]provider.Client{"a": client})
			h.submit(t, "a")
			awaitBackground(t, entered)
			if err := h.engine.Cancel("a"); err != nil {
				t.Fatal(err)
			}
			if ev := awaitBackground(t, h.terminal["a"]); ev.Kind != event.TurnCancelled {
				t.Fatalf("terminal = %+v", ev)
			}
			h.engine.Wait()
			if calls.Load() != 2 || client.calls.Load() != 2 {
				t.Fatalf("native calls=%d model calls=%d; later actions/provider steps must stop", calls.Load(), client.calls.Load())
			}
			content := h.result(t, "a")
			var payload struct {
				Outcome   string                 `json:"outcome"`
				Retryable bool                   `json:"retryable"`
				Result    computer.ActionsResult `json:"result"`
			}
			if err := json.Unmarshal([]byte(content), &payload); err != nil {
				t.Fatal(err)
			}
			wantOutcome := outcome
			wantCount, wantFailedOutcome := 1, outcome
			if outcome == "not_started" {
				wantOutcome = "partial"
			}
			if outcome == "completed_during_cancel" {
				wantOutcome, wantCount, wantFailedOutcome = "partial", 2, "not_started"
			}
			if payload.Outcome != wantOutcome || payload.Retryable || payload.Result.CompletedCount != wantCount || len(payload.Result.Actions) != wantCount ||
				payload.Result.FailedIndex == nil || *payload.Result.FailedIndex != wantCount || payload.Result.Failure == nil || payload.Result.Failure.Outcome != wantFailedOutcome {
				t.Fatalf("completed prefix / failed item lost: %s", content)
			}
			// Reconstruct Engine: only canonical messages can carry these facts into
			// the next turn. The scripted model reads them and sends no new action.
			next := &backgroundEngineClient{step: func(_ context.Context, req provider.Request, _ int) (<-chan provider.Chunk, error) {
				for _, message := range req.Messages {
					for _, part := range message.Parts {
						if part.Type == provider.PartToolResult && part.Name == tool.ComputerAct && part.Content == content {
							return smokeTextStream("Prefix retained; uncertain click not replayed."), nil
						}
					}
				}
				return nil, fmt.Errorf("canonical partial result missing after Engine reconstruction")
			}}
			restarted := New(h.store, h.hub, mapResolver{"a": next}, h.store, WithTools(h.runner), WithApps(h.apps))
			defer func() {
				_ = restarted.Cancel("a")
				restarted.Stop()
				restarted.Wait()
			}()
			if _, err := restarted.Submit(context.Background(), SubmitInput{SessionID: "a", ClientMessageID: "followup", Text: "Report the last operation; do not replay it."}); err != nil {
				t.Fatal(err)
			}
			if ev := awaitBackground(t, h.terminal["a"]); ev.Kind != event.TurnCompleted {
				t.Fatalf("followup = %+v", ev)
			}
			restarted.Wait()
			if calls.Load() != 2 || next.calls.Load() != 1 {
				t.Fatal("unexpected replay")
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.approvals["a"] != 1 {
				t.Fatalf("approvals = %v", h.approvals)
			}
			var results int
			for _, ev := range h.events {
				if ev.Kind == event.TurnTool && ev.Name == tool.ComputerAct && ev.Phase == "error" && ev.Content == content {
					results++
				}
			}
			if results != 1 {
				t.Fatalf("live partial-result events = %d", results)
			}
		})
	}
}

func TestBackgroundEngineConcurrentSessions(t *testing.T) {
	for _, cancelQueued := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel_queued_%t", cancelQueued), func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var mu sync.Mutex
			var received []string
			service := &backgroundEngineService{pointer: func(ctx context.Context, session string, input computer.PointerInput) (computer.NativeAction, error) {
				mu.Lock()
				received = append(received, session)
				first := len(received) == 1
				mu.Unlock()
				if first {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return computer.NativeAction{}, ctx.Err()
					}
				}
				return completedBackgroundClick(input), nil
			}}
			h := newBackgroundEngineHarness(t, service, map[string]provider.Client{"a": backgroundBatchClient(2), "b": backgroundBatchClient(1)})
			h.submit(t, "a")
			awaitBackground(t, entered)
			if got := awaitBackground(t, h.entry); got != "a" {
				t.Fatal(got)
			}
			h.submit(t, "b")
			if got := awaitBackground(t, h.entry); got != "b" {
				t.Fatal(got)
			}
			if cancelQueued {
				if err := h.engine.Cancel("b"); err != nil {
					t.Fatal(err)
				}
				if ev := awaitBackground(t, h.terminal["b"]); ev.Kind != event.TurnCancelled {
					t.Fatalf("queued = %+v", ev)
				}
				queued := h.result(t, "b")
				var failure computer.Failure
				if err := json.Unmarshal([]byte(queued), &failure); err != nil || failure.Code != "computer_action_cancelled" || failure.Outcome != "not_started" {
					t.Fatalf("queued result = %s err=%v", queued, err)
				}
			}
			close(release)
			if ev := awaitBackground(t, h.terminal["a"]); ev.Kind != event.TurnCompleted {
				t.Fatalf("owner = %+v", ev)
			}
			want := []string{"a", "a"}
			if !cancelQueued {
				if ev := awaitBackground(t, h.terminal["b"]); ev.Kind != event.TurnCompleted {
					t.Fatalf("queued = %+v", ev)
				}
				want = append(want, "b")
			}
			h.engine.Wait()
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(received, want) {
				t.Fatalf("native sessions = %v", received)
			}
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.approvals["a"] != 1 || h.approvals["b"] != 1 {
				t.Fatalf("session grants = %v", h.approvals)
			}
		})
	}
}

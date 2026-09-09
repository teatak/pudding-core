package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/computer"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/tool"
)

// Test-only scheduling around the real bridge. No native input is simulated,
// no production hooks, SIGSTOP, TCC resets or journal writes are used.
type calendarSessionService struct {
	computer.Service
	window  uint32
	pointer func(context.Context, string, computer.PointerInput) (computer.NativeAction, error)
}

func (s *calendarSessionService) Pointer(ctx context.Context, session, appID string, window uint32, input computer.PointerInput) (computer.NativeAction, error) {
	if appID != "com.apple.iCal" || window != s.window || input.Delivery != "background" {
		return computer.NativeAction{}, fmt.Errorf("wrong native target or delivery")
	}
	return s.pointer(ctx, session, input)
}

func calendarBatchClient(window uint32, actions func() []computer.ActionInput) *backgroundEngineClient {
	return &backgroundEngineClient{step: func(_ context.Context, req provider.Request, step int) (<-chan provider.Chunk, error) {
		switch step {
		case 1:
			return smokeToolStream("load", tool.AppLoad, `{"app_id":"computer-use"}`), nil
		case 2:
			if !smokeHasToolDef(req.Tools, tool.ComputerAct) {
				return nil, fmt.Errorf("Computer Use was not loaded")
			}
			args, _ := json.Marshal(map[string]any{"appID": "com.apple.iCal", "windowID": window, "actions": actions()})
			return smokeToolStream("batch", tool.ComputerAct, string(args)), nil
		case 3:
			var result struct {
				Result computer.ActionsResult `json:"result"`
			}
			if err := decodeSmokeToolResult(req, tool.ComputerAct, &result); err != nil {
				return nil, err
			}
			if result.Result.CompletedCount != len(actions()) {
				return nil, fmt.Errorf("incomplete batch")
			}
			return smokeTextStream("Native batch complete, no replay."), nil
		default:
			return nil, fmt.Errorf("unexpected model step; no replay")
		}
	}}
}

// Read the exact observed PID's existing recovery record; do not create or
// modify it. More than one process-start record is ambiguous and stops the test.
func calendarJournalPhase(pid int32) (byte, error) {
	files, err := filepath.Glob(filepath.Join(os.TempDir(), "pudding-computer-input", fmt.Sprintf("%d-*-*.state", pid)))
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	if len(files) != 1 {
		return 0, fmt.Errorf("ambiguous Calendar recovery journal")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		return 0, err
	}
	if len(data) != 1 || (data[0] > 3 && data[0]&3 != 2) {
		return 0, fmt.Errorf("invalid Calendar recovery journal")
	}
	return data[0] & 3, nil
}

func waitCalendarPhase(ctx context.Context, pid int32, desired byte) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		phase, err := calendarJournalPhase(pid)
		if err != nil {
			return err
		}
		if phase == desired {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("journal phase %d not reached: %w; no replay", desired, ctx.Err())
		case <-tick.C:
		}
	}
}

func TestBackgroundCalendarEngineSessions(t *testing.T) {
	bridge, window := backgroundCalendarBridge(t)
	for _, scenario := range []string{"serial", "cancel_queued", "cancel_active"} {
		if !t.Run(scenario, func(t *testing.T) { runCalendarEngineSessions(t, bridge, window, scenario) }) {
			return // Do not continue a native matrix after a failed scenario.
		}
	}
}

func runCalendarEngineSessions(t *testing.T, bridge computer.Service, window uint32, scenario string) {
	const appID = "com.apple.iCal"
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	observe := func() computer.Observation {
		observed, err := bridge.Observe(ctx, "background-native-test-assertion", appID, window, 60)
		if err != nil {
			t.Fatal(err)
		}
		return observed
	}
	initial := observe()
	initialMonth, err := backgroundCalendarMonth(initial)
	if err != nil {
		t.Fatal(err)
	}
	positions, err := backgroundCalendarActions(initial)
	if err != nil {
		t.Fatal(err)
	}
	if phase, err := calendarJournalPhase(initial.PID); err != nil || phase != 0 {
		t.Fatalf("initial journal=%d err=%v", phase, err)
	}
	next, previous := positions[0], positions[1]
	actionsA, actionsB := []computer.ActionInput{next, next}, []computer.ActionInput{previous, previous}
	if scenario == "cancel_queued" {
		actionsA, actionsB = []computer.ActionInput{next, previous}, []computer.ActionInput{next}
	}
	if scenario == "cancel_active" {
		actionsA, actionsB = []computer.ActionInput{next, next, next}, nil
	}
	clientA := calendarBatchClient(window, func() []computer.ActionInput { return actionsA })
	clientB := calendarBatchClient(window, func() []computer.ActionInput { return actionsB })
	firstDone, releaseFirst, checkpoint := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var checkpointErr error
	var sessions, verified []string
	expectedMonth := initialMonth
	var h *backgroundEngineHarness
	service := &calendarSessionService{Service: bridge, window: window}
	service.pointer = func(callCtx context.Context, session string, input computer.PointerInput) (computer.NativeAction, error) {
		sessions = append(sessions, session)
		index := len(sessions)
		cancelHere := scenario == "cancel_active" && session == "a" && index == 2
		if cancelHere {
			go func() {
				checkpointErr = waitCalendarPhase(callCtx, initial.PID, 2)
				if checkpointErr == nil {
					checkpointErr = h.engine.Cancel("a")
				}
				close(checkpoint)
			}()
		}
		result, callErr := bridge.Pointer(callCtx, session, appID, window, input)
		if cancelHere {
			<-checkpoint
			if checkpointErr != nil {
				return result, checkpointErr
			}
		}
		if callErr != nil {
			return result, callErr
		}
		observed, err := bridge.Observe(callCtx, session, appID, window, 60)
		if err != nil {
			return result, err
		}
		month, err := backgroundCalendarMonth(observed)
		if err != nil {
			return result, err
		}
		delta := 1
		if input.X == *previous.X && input.Y == *previous.Y {
			delta = -1
		}
		want := expectedMonth.AddDate(0, delta, 0)
		if month != want {
			return result, fmt.Errorf("native month=%s want=%s; stop without replay", month, want)
		}
		expectedMonth = month
		verified = append(verified, month.Format("2006-01"))
		if index == 1 && scenario != "cancel_active" {
			close(firstDone)
			select {
			case <-releaseFirst:
			case <-callCtx.Done():
			}
		}
		return result, nil
	}
	h = newBackgroundEngineHarness(t, service, map[string]provider.Client{"a": clientA, "b": clientB})
	h.submit(t, "a")
	if got := awaitBackground(t, h.entry); got != "a" {
		t.Fatal(got)
	}
	if scenario == "cancel_active" {
		awaitBackground(t, checkpoint)
		if checkpointErr != nil {
			t.Fatal(checkpointErr)
		}
		if ev := awaitBackground(t, h.terminal["a"]); ev.Kind != event.TurnCancelled {
			t.Fatalf("active terminal=%+v", ev)
		}
		h.engine.Wait()
		if !reflect.DeepEqual(sessions, []string{"a", "a"}) || clientA.calls.Load() != 2 {
			t.Fatalf("after cancel: sessions=%v modelCalls=%d", sessions, clientA.calls.Load())
		}
		var result struct {
			Outcome   string                 `json:"outcome"`
			Retryable bool                   `json:"retryable"`
			Result    computer.ActionsResult `json:"result"`
		}
		content := h.result(t, "a")
		if err := json.Unmarshal([]byte(content), &result); err != nil {
			t.Fatal(err)
		}
		if result.Outcome != "unknown" || result.Retryable || result.Result.CompletedCount != 1 || len(result.Result.Actions) != 1 || result.Result.FailedIndex == nil || *result.Result.FailedIndex != 1 || result.Result.Failure == nil || result.Result.Failure.Outcome != "unknown" {
			t.Fatalf("cancel lost completed prefix or uncertain effect: %s", content)
		}
		if err := waitCalendarPhase(ctx, initial.PID, 0); err != nil {
			t.Fatal(err)
		}
		// An in-flight click may finish during Host drain. Observe before deciding
		// how many NEW reverse navigations restore the view, never replay the click.
		after := observe()
		month, err := backgroundCalendarMonth(after)
		if err != nil {
			t.Fatal(err)
		}
		delta := (month.Year()-initialMonth.Year())*12 + int(month.Month()-initialMonth.Month())
		if delta != 1 && delta != 2 {
			t.Fatalf("unexpected post-cancel month delta=%d; no recovery input", delta)
		}
		freshPositions, err := backgroundCalendarActions(after)
		if err != nil {
			t.Fatal(err)
		}
		previous, expectedMonth = freshPositions[1], month
		for range delta {
			actionsB = append(actionsB, previous)
		}
		t.Logf("checkpoint=pressed; post-cancel month=%s; canonical=%s", month.Format("2006-01"), content)
		h.submit(t, "b")
	} else {
		awaitBackground(t, firstDone)
		h.submit(t, "b")
		if got := awaitBackground(t, h.entry); got != "b" {
			t.Fatal(got)
		}
		if scenario == "cancel_queued" {
			if err := h.engine.Cancel("b"); err != nil {
				t.Fatal(err)
			}
			if ev := awaitBackground(t, h.terminal["b"]); ev.Kind != event.TurnCancelled {
				t.Fatalf("queued terminal=%+v", ev)
			}
			var failure computer.Failure
			content := h.result(t, "b")
			if err := json.Unmarshal([]byte(content), &failure); err != nil || failure.Code != "computer_action_cancelled" || failure.Outcome != "not_started" || clientB.calls.Load() != 2 {
				t.Fatalf("queued result=%s err=%v", content, err)
			}
			t.Logf("queued canonical=%s", content)
		}
		close(releaseFirst)
		if ev := awaitBackground(t, h.terminal["a"]); ev.Kind != event.TurnCompleted {
			t.Fatalf("owner terminal=%+v", ev)
		}
		_ = h.result(t, "a")
	}
	if scenario != "cancel_queued" {
		if ev := awaitBackground(t, h.terminal["b"]); ev.Kind != event.TurnCompleted {
			t.Fatalf("second terminal=%+v", ev)
		}
		_ = h.result(t, "b")
	}
	h.engine.Wait()
	wantSessions := []string{"a", "a"}
	if scenario != "cancel_queued" {
		for range actionsB {
			wantSessions = append(wantSessions, "b")
		}
	}
	if !reflect.DeepEqual(sessions, wantSessions) {
		t.Fatalf("native order=%v want=%v", sessions, wantSessions)
	}
	finalMonth, err := backgroundCalendarMonth(observe())
	if err != nil || finalMonth != initialMonth {
		t.Fatalf("final month=%s initial=%s err=%v", finalMonth, initialMonth, err)
	}
	if phase, err := calendarJournalPhase(initial.PID); err != nil || phase != 0 {
		t.Fatalf("final journal=%d err=%v", phase, err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.approvals["a"] != 1 || h.approvals["b"] != 1 {
		t.Fatalf("session approvals=%v", h.approvals)
	}
	t.Logf("scenario=%s native sessions=%v verified months=%v final=%s journal=idle approvals=%v", scenario, sessions, verified, finalMonth.Format("2006-01"), h.approvals)
}

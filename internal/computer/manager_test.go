package computer

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeService struct {
	mu           sync.Mutex
	observes     int
	uses         int
	actions      int
	pointers     int
	lastPointer  PointerInput
	pointerErr   error
	observeErr   error
	actErr       error
	actErrAt     int
	lastSession  string
	foreground   bool
	list         AppList
	launch       NativeUse
	quits        []NativeQuit
	quitRequests []NativeQuit
}

func (f *fakeService) Permissions(context.Context) (Permissions, error) {
	return Permissions{}, nil
}

func (f *fakeService) ListApps(_ context.Context, sessionID string) (AppList, error) {
	f.lastSession = sessionID
	return f.list, nil
}

func (f *fakeService) UseApp(_ context.Context, sessionID, appID string, foreground bool, selection AppSelection) (NativeUse, error) {
	f.uses++
	f.lastSession = sessionID
	f.foreground = foreground
	if f.launch.AppID == "" {
		windowAppID := appID
		return NativeUse{AppID: appID, Name: "Example", PID: 42, NewlyLaunched: true, WindowStatus: WindowStatusReady, Windows: []CapturableWindow{{WindowID: 7, PID: 42, AppID: &windowAppID, Frame: Frame{Width: 100, Height: 80}}}}, nil
	}
	return f.launch, nil
}

func (f *fakeService) QuitApp(_ context.Context, sessionID, appID string, pid int32) (NativeQuit, error) {
	f.lastSession = sessionID
	f.quitRequests = append(f.quitRequests, NativeQuit{AppID: appID, PID: pid})
	result := NativeQuit{AppID: appID, PID: pid, Closed: true}
	if len(f.quits) > 0 {
		result = f.quits[0]
		f.quits = f.quits[1:]
	}
	return result, nil
}

func (f *fakeService) Observe(_ context.Context, sessionID, appID string, windowID uint32, _ int) (Observation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observes++
	f.lastSession = sessionID
	if f.observeErr != nil && f.observes > 1 {
		return Observation{}, f.observeErr
	}
	return testObservation(appID, windowID), nil
}

func (f *fakeService) ObserveCapture(_ context.Context, _ string, appID string, windowID uint32, _ int, output string, includeAccessibility bool) (NativeObservationCapture, error) {
	return NativeObservationCapture{
		Observation: observationPointer(testObservation(appID, windowID)),
		Capture:     &Capture{WindowID: windowID, Output: output, Width: 1, Height: 1, ScaleFactor: 1},
	}, nil
}

func (f *fakeService) Act(_ context.Context, sessionID, appID string, _ uint32, elementID, action string, _ *string) (NativeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions++
	f.lastSession = sessionID
	if f.actErr != nil && (f.actErrAt == 0 || f.actions == f.actErrAt) {
		return NativeAction{}, f.actErr
	}
	return NativeAction{AppID: appID, ElementID: elementID, Action: action, Completed: true}, nil
}

func (f *fakeService) Pointer(_ context.Context, sessionID, appID string, _ uint32, pointer PointerInput) (NativeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pointers++
	f.lastPointer = pointer
	f.lastSession = sessionID
	if f.pointerErr != nil {
		return NativeAction{}, f.pointerErr
	}
	return NativeAction{
		Delivery: pointer.Delivery,
		AppID:    appID, Action: pointer.Action, Completed: true, X: &pointer.X, Y: &pointer.Y,
		ToX: pointer.ToX, ToY: pointer.ToY, Button: pointer.Button,
		ClickCount: pointer.ClickCount, DeltaX: pointer.DeltaX, DeltaY: pointer.DeltaY,
	}, nil
}

func (f *fakeService) ReleaseSession(_ context.Context, sessionID string) error {
	f.lastSession = sessionID
	return nil
}

func semanticActions(elementID, actionType string, value *string) []ActionInput {
	return []ActionInput{{Type: actionType, ElementID: elementID, Value: value}}
}

func pointerActions(pointer PointerInput) []ActionInput {
	x, y := pointer.X, pointer.Y
	action := ActionInput{
		Delivery: pointer.Delivery,
		Type:     pointer.Action, X: &x, Y: &y, ToX: pointer.ToX, ToY: pointer.ToY,
		Button: pointer.Button, DeltaX: pointer.DeltaX, DeltaY: pointer.DeltaY,
	}
	if pointer.ClickCount != 0 {
		count := pointer.ClickCount
		action.ClickCount = &count
	}
	return []ActionInput{action}
}

func TestManagerRoutesSessionAndReusesStableElementID(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	_, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, semanticActions("button", ActionPress, nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || !result.Actions[0].Completed || service.lastSession != "session_a" {
		t.Fatalf("unexpected result: %#v session=%q", result, service.lastSession)
	}
	_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, semanticActions("button", ActionPress, nil))
	if err != nil {
		t.Fatal(err)
	}
	if service.actions != 2 || service.observes != 1 {
		t.Fatalf("actions=%d observations=%d, want 2 and 1", service.actions, service.observes)
	}
}

func TestManagerActionsDoNotObserveAutomatically(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	_, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	value := "hello"
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, []ActionInput{
		{ElementID: "button", Type: ActionPress},
		{ElementID: "row", Type: ActionSelect},
		{ElementID: "field", Type: ActionSetValue, Value: &value},
		{ElementID: "field", Type: ActionSubmit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failure != nil || result.FailedIndex != nil || result.CompletedCount != 4 || len(result.Actions) != 4 {
		t.Fatalf("unexpected sequence result: %#v", result)
	}
	if service.actions != 4 || service.observes != 1 {
		t.Fatalf("native actions=%d observations=%d, want 4 and 1", service.actions, service.observes)
	}
	_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, []ActionInput{
		{ElementID: "button", Type: ActionPress},
		{ElementID: "button", Type: ActionPress},
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.actions != 6 || service.observes != 1 {
		t.Fatalf("native actions=%d observations=%d, want 6 and 1", service.actions, service.observes)
	}
}

func TestManagerActionsStopAtFirstFailureWithoutObserving(t *testing.T) {
	service := &fakeService{
		actErr:   &OperationError{Code: "computer_action_blocked", Message: "blocked", Outcome: "not_started", Retryable: true},
		actErrAt: 2,
	}
	manager := NewManager(service)
	_, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, []ActionInput{
		{ElementID: "button", Type: ActionPress},
		{ElementID: "row", Type: ActionSelect},
		{ElementID: "field", Type: ActionSubmit},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Failure == nil || result.Failure.Code != "computer_action_blocked" || result.Failure.Outcome != "not_started" || !result.Failure.Retryable || result.FailedIndex == nil || *result.FailedIndex != 1 || result.CompletedCount != 1 || len(result.Actions) != 1 {
		t.Fatalf("unexpected partial sequence result: %#v", result)
	}
	if service.actions != 2 || service.observes != 1 {
		t.Fatalf("native actions=%d observations=%d, want 2 and 1", service.actions, service.observes)
	}
}

func TestManagerRunsSemanticAndPointerActionsThroughOneArray(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	x, y := 0.5, 0.5
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, []ActionInput{
		{Type: ActionPress, ElementID: "button"},
		{Type: ActionClick, X: &x, Y: &y},
	})
	if err != nil || result.Failure != nil || result.CompletedCount != 2 || service.actions != 1 || service.pointers != 1 {
		t.Fatalf("unexpected mixed actions result=%#v semantic=%d pointer=%d err=%v", result, service.actions, service.pointers, err)
	}
}

func TestManagerInsertsTextWithoutObservationOrActivation(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	for _, value := range []string{"你好🙂", ""} {
		result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, []ActionInput{
			{Type: ActionInsertText, ElementID: "text", Value: &value},
		})
		if err != nil || result.Failure != nil || result.CompletedCount != 1 || result.Actions[0].Action != ActionInsertText {
			t.Fatalf("insert_text result=%+v err=%v", result, err)
		}
	}
	if service.actions != 2 || service.uses != 0 || service.observes != 0 || service.pointers != 0 {
		t.Fatalf("unexpected background insert side effects: %+v", service)
	}
	for _, action := range []ActionInput{
		{Type: ActionInsertText, ElementID: "text"},
		{Type: ActionInsertText, Value: new(string)},
		{Type: ActionInsertText, ElementID: "text", Value: new(string), Delivery: "background"},
	} {
		if _, err := NormalizeActions([]ActionInput{action}); err == nil {
			t.Fatalf("invalid insert_text accepted: %+v", action)
		}
	}
}

func TestManagerPointerUsesCurrentWindowCoordinatesWithoutObservation(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, pointerActions(PointerInput{Action: ActionClick, X: 0.5, Y: 0.5, Button: PointerButtonLeft, ClickCount: 2}))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Actions) != 1 || !result.Actions[0].Completed || result.Actions[0].Action != ActionClick || result.Actions[0].ClickCount != 2 || service.pointers != 1 {
		t.Fatalf("unexpected pointer result: %#v pointers=%d", result, service.pointers)
	}
	_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, pointerActions(PointerInput{Action: ActionClick, X: 0.5, Y: 0.5, Button: PointerButtonLeft, ClickCount: 1}))
	if err != nil || service.pointers != 2 {
		t.Fatalf("second pointer result: err=%v pointers=%d", err, service.pointers)
	}
}

func TestManagerPointerRejectsOutOfBounds(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	_, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, pointerActions(PointerInput{Action: ActionClick, X: 1, Y: 0, Button: PointerButtonLeft, ClickCount: 1}))
	assertOperationCode(t, err, "computer_invalid_request")
	if service.pointers != 0 {
		t.Fatalf("native pointer actions = %d, want 0", service.pointers)
	}
}

func TestManagerRoutesDragAndScroll(t *testing.T) {
	toX, toY := 0.75, 0.25
	deltaX, deltaY := 0, 240
	for _, pointer := range []PointerInput{
		{Action: ActionDrag, X: 0.25, Y: 0.25, ToX: &toX, ToY: &toY},
		{Action: ActionScroll, X: 0.5, Y: 0.5, DeltaX: &deltaX, DeltaY: &deltaY},
	} {
		service := &fakeService{}
		manager := NewManager(service)
		result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, pointerActions(pointer))
		if err != nil || len(result.Actions) != 1 || result.Actions[0].Action != pointer.Action || service.pointers != 1 {
			t.Fatalf("action=%s result=%#v pointers=%d err=%v", pointer.Action, result, service.pointers, err)
		}
	}
}

func TestManagerAcceptsSelectAndSubmitActions(t *testing.T) {
	for _, tc := range []struct {
		elementID string
		action    string
	}{
		{elementID: "row", action: ActionSelect},
		{elementID: "field", action: ActionSubmit},
	} {
		t.Run(tc.action, func(t *testing.T) {
			service := &fakeService{}
			manager := NewManager(service)
			result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, semanticActions(tc.elementID, tc.action, nil))
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Actions) != 1 || result.Actions[0].Action != tc.action || !result.Actions[0].Completed {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestManagerKeepsDuplicateStableElementsReadOnly(t *testing.T) {
	windowID := uint32(42)
	observed, err := validateObservation("com.example.App", windowID, Observation{
		AppID: "com.example.App", WindowID: &windowID,
		Elements: []Element{
			{ElementID: "same-label", WindowID: &windowID, Actions: []string{ActionPress}},
			{ElementID: "same-label", WindowID: &windowID, Actions: []string{ActionPress}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range observed.Elements {
		if len(element.Actions) != 0 {
			t.Fatalf("duplicate stable element remained actionable: %#v", element)
		}
	}
}

func TestManagerRejectsInvalidApplicationInventory(t *testing.T) {
	service := &fakeService{}
	serviceList := AppList{Apps: []Application{
		{AppID: "com.example.App", Name: "Example", Running: false, Active: true},
	}}
	service.list = serviceList
	_, err := NewManager(service).ListApps(context.Background(), "session_a")
	assertOperationCode(t, err, "computer_invalid_response")
}

func TestManagerOwnsOnlyNewSessionLaunches(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", true, AppSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if launched.LaunchID == nil || launched.PID != 42 || len(launched.Windows) != 1 || launched.Windows[0].WindowID != 7 || !service.foreground {
		t.Fatalf("unexpected launch: %#v", launched)
	}
	if appID, ok := manager.OwnedLaunchAppID("session_a", *launched.LaunchID); !ok || appID != "com.example.App" {
		t.Fatalf("unexpected launch owner: appID=%q ok=%v", appID, ok)
	}
	if _, ok := manager.OwnedLaunchAppID("session_b", *launched.LaunchID); ok {
		t.Fatal("cross-session launch owner must not resolve")
	}
	_, err = manager.QuitApp(context.Background(), "session_b", *launched.LaunchID)
	assertOperationCode(t, err, "computer_launch_not_owned")
	closed, err := manager.QuitApp(context.Background(), "session_a", *launched.LaunchID)
	if err != nil || !closed.Closed || closed.PID != 42 {
		t.Fatalf("unexpected quit: %#v err=%v", closed, err)
	}
	_, err = manager.QuitApp(context.Background(), "session_a", *launched.LaunchID)
	assertOperationCode(t, err, "computer_launch_not_owned")
}

func TestManagerDoesNotOwnAlreadyRunningApplication(t *testing.T) {
	appID := "com.example.App"
	service := &fakeService{launch: NativeUse{AppID: appID, Name: "Example", PID: 42, WindowStatus: WindowStatusReady, Windows: []CapturableWindow{{WindowID: 8, PID: 42, AppID: &appID, Frame: Frame{Width: 100, Height: 80}}}}}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if launched.LaunchID != nil || len(launched.Windows) != 1 || launched.Windows[0].WindowID != 8 {
		t.Fatalf("unexpected ownership: %#v", launched)
	}
	if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
		t.Fatal(err)
	}
	if len(service.quitRequests) != 0 {
		t.Fatalf("already-running app was quit: %#v", service.quitRequests)
	}
}

func TestManagerKeepsLaunchedApplicationAfterForegroundFailure(t *testing.T) {
	for _, code := range []string{"computer_activation_failed", "computer_window_raise_failed", "computer_use_failed"} {
		t.Run(code, func(t *testing.T) {
			service := &fakeService{launch: NativeUse{
				AppID: "com.example.App", Name: "Example", PID: 42, NewlyLaunched: true, WindowStatus: WindowStatusNone,
				ForegroundError: &Failure{Code: code, Message: "could not show the application", Outcome: "unknown"},
			}}
			manager := NewManager(service)
			result, err := manager.UseApp(context.Background(), "session_a", "com.example.App", true, AppSelection{})
			assertOperationCode(t, err, code)
			if failure := ErrorFailure(err); failure.Outcome != "unknown" || failure.Retryable || result.LaunchID != nil {
				t.Fatalf("foreground failure reported success or a safe retry: result=%+v failure=%+v", result, failure)
			}
			if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
				t.Fatal(err)
			}
			if len(service.quitRequests) != 1 || service.quitRequests[0].AppID != "com.example.App" || service.quitRequests[0].PID != 42 || len(manager.launches) != 0 {
				t.Fatalf("foreground failure lost the launched application: quits=%+v leases=%+v", service.quitRequests, manager.launches)
			}
		})
	}
}

func TestManagerRecoversSameLaunchAfterForegroundFailure(t *testing.T) {
	service := &fakeService{launch: NativeUse{
		AppID: "com.example.App", Name: "Example", PID: 42, NewlyLaunched: true, WindowStatus: WindowStatusNone,
		ForegroundError: &Failure{Code: "computer_activation_failed", Message: "could not activate", Outcome: "unknown"},
	}}
	manager := NewManager(service)
	_, err := manager.UseApp(context.Background(), "session_a", "com.example.App", true, AppSelection{})
	assertOperationCode(t, err, "computer_activation_failed")
	lease := manager.launches["session_a"]
	if lease == nil || len(lease.owned) != 1 {
		t.Fatalf("failed app entry has no launch ownership: %+v", lease)
	}
	var launchID string
	for id := range lease.owned {
		launchID = id
	}
	service.launch.NewlyLaunched = false
	_, err = manager.UseApp(context.Background(), "session_a", "com.example.App", true, AppSelection{})
	assertOperationCode(t, err, "computer_activation_failed")
	service.launch.ForegroundError = nil
	recovered, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
	if err != nil || recovered.LaunchID == nil || *recovered.LaunchID != launchID || len(lease.owned) != 1 {
		t.Fatalf("app entry did not recover original ownership: result=%+v err=%v lease=%+v", recovered, err, lease)
	}
	closed, err := manager.QuitApp(context.Background(), "session_a", *recovered.LaunchID)
	if err != nil || !closed.Closed || len(service.quitRequests) != 1 {
		t.Fatalf("recovered launch was not normally quit: result=%+v err=%v", closed, err)
	}
}

func TestManagerDoesNotOwnPreexistingApplicationAfterForegroundFailure(t *testing.T) {
	service := &fakeService{launch: NativeUse{
		AppID: "com.example.App", Name: "Example", PID: 42, WindowStatus: WindowStatusNone,
		ForegroundError: &Failure{Code: "computer_window_raise_failed", Message: "could not raise windows", Outcome: "unknown"},
	}}
	manager := NewManager(service)
	_, err := manager.UseApp(context.Background(), "session_a", "com.example.App", true, AppSelection{})
	assertOperationCode(t, err, "computer_window_raise_failed")
	if len(manager.launches) != 0 {
		t.Fatalf("preexisting app acquired launch ownership: %+v", manager.launches)
	}
	if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
		t.Fatal(err)
	}
	if len(service.quitRequests) != 0 {
		t.Fatalf("preexisting app was quit: %+v", service.quitRequests)
	}
}

func TestManagerRejectsMalformedForegroundFailureWithoutLosingLaunch(t *testing.T) {
	for _, tc := range []struct {
		name       string
		foreground bool
		failure    Failure
	}{
		{name: "empty object", foreground: true},
		{name: "invalid code", foreground: true, failure: Failure{Code: "invalid", Message: "activation failed", Outcome: "unknown"}},
		{name: "empty message", foreground: true, failure: Failure{Code: "computer_activation_failed", Outcome: "unknown"}},
		{name: "missing outcome", foreground: true, failure: Failure{Code: "computer_activation_failed", Message: "activation failed"}},
		{name: "not started", foreground: true, failure: Failure{Code: "computer_activation_failed", Message: "activation failed", Outcome: "not_started"}},
		{name: "completed", foreground: true, failure: Failure{Code: "computer_activation_failed", Message: "activation failed", Outcome: "completed"}},
		{name: "retryable", foreground: true, failure: Failure{Code: "computer_activation_failed", Message: "activation failed", Outcome: "unknown", Retryable: true}},
		{name: "not requested", failure: Failure{Code: "computer_activation_failed", Message: "activation failed", Outcome: "unknown"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &fakeService{launch: NativeUse{
				AppID: "com.example.App", Name: "Example", PID: 42, NewlyLaunched: true, WindowStatus: WindowStatusNone,
				ForegroundError: &tc.failure,
			}}
			manager := NewManager(service)
			_, err := manager.UseApp(context.Background(), "session_a", "com.example.App", tc.foreground, AppSelection{})
			assertOperationCode(t, err, "computer_invalid_response")
			if failure := ErrorFailure(err); failure.Outcome != "unknown" || failure.Retryable {
				t.Fatalf("malformed foreground error permitted replay: %+v", failure)
			}
			if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
				t.Fatal(err)
			}
			if len(service.quitRequests) != 1 || service.quitRequests[0].PID != 42 {
				t.Fatalf("malformed foreground error lost launch ownership: %+v", service.quitRequests)
			}
		})
	}
}

func TestManagerRejectsWindowFromAnotherProcess(t *testing.T) {
	appID := "com.example.App"
	service := &fakeService{launch: NativeUse{
		AppID:        appID,
		Name:         "Example",
		PID:          42,
		WindowStatus: WindowStatusReady,
		Windows:      []CapturableWindow{{WindowID: 8, PID: 99, AppID: &appID, Frame: Frame{Width: 100, Height: 80}}},
	}}
	_, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false, AppSelection{})
	assertOperationCode(t, err, "computer_invalid_response")
}

func TestManagerRequiresFailureDetailsForFailedWindowDiscovery(t *testing.T) {
	appID := "com.example.App"
	service := &fakeService{launch: NativeUse{
		AppID: appID, Name: "Example", PID: 42, WindowStatus: WindowStatusFailed,
	}}
	_, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false, AppSelection{})
	assertOperationCode(t, err, "computer_invalid_response")

	service.launch.WindowError = &Failure{Code: "computer_unavailable", Message: "window discovery failed", Outcome: "not_started"}
	used, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false, AppSelection{})
	if err != nil || used.WindowError == nil || used.WindowError.Code != "computer_unavailable" {
		t.Fatalf("unexpected failed discovery: %#v err=%v", used, err)
	}
}

func TestManagerKeepsOwnershipWhenNormalQuitNeedsAttention(t *testing.T) {
	service := &fakeService{quits: []NativeQuit{{AppID: "com.example.App", PID: 42, Closed: false}, {AppID: "com.example.App", PID: 42, Closed: true}}}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.QuitApp(context.Background(), "session_a", *launched.LaunchID)
	if err != nil || first.Closed {
		t.Fatalf("unexpected first quit: %#v err=%v", first, err)
	}
	second, err := manager.QuitApp(context.Background(), "session_a", *launched.LaunchID)
	if err != nil || !second.Closed {
		t.Fatalf("unexpected second quit: %#v err=%v", second, err)
	}
}

func TestManagerReleasesOwnedApplicationsWithNormalQuit(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
		t.Fatal(err)
	}
	if len(service.quitRequests) != 1 || service.quitRequests[0].PID != 42 {
		t.Fatalf("unexpected release quit requests: %#v", service.quitRequests)
	}
	_, err = manager.QuitApp(context.Background(), "session_a", *launched.LaunchID)
	assertOperationCode(t, err, "computer_launch_not_owned")
}

func TestManagerReleaseRevokesOwnershipEvenWhenWriteQueueTimesOut(t *testing.T) {
	closed := make(chan NativeQuit, 1)
	service := &computerLifecycleService{quit:func(_ context.Context,_ string,appID string,pid int32)(NativeQuit,error){
		result:=NativeQuit{AppID:appID,PID:pid,Closed:true};closed<-result;return result,nil
	}}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
	if err != nil {
		t.Fatal(err)
	}
	unlock := holdComputerWrite(t, manager)
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	err = manager.ReleaseSession(ctx, "session_a")
	if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "queued") {
		t.Fatalf("release error = %v", err)
	}
	if _, ok := manager.OwnedLaunchAppID("session_a", *launched.LaunchID); ok || len(manager.launches) != 0 {
		t.Fatal("timed-out release retained session ownership")
	}
	select {case <-closed:t.Fatal("cleanup bypassed write queue");default:}
	unlock()
	if result:=awaitComputerLifecycle(t,closed);result.PID!=42{t.Fatalf("wrong cleanup target: %+v",result)}
}

func TestManagerReleaseBudgetsEachQuitAfterTheWriteWait(t *testing.T) {
	var budgets []time.Duration
	service:=&computerLifecycleService{quit:func(ctx context.Context,_ string,appID string,pid int32)(NativeQuit,error){
		deadline,ok:=ctx.Deadline();if !ok {t.Error("quit has no deadline")};budgets=append(budgets,time.Until(deadline))
		if len(budgets)==1 {<-ctx.Done();return NativeQuit{},ctx.Err()}
		return NativeQuit{AppID:appID,PID:pid,Closed:true},nil
	}}
	manager:=NewManager(service)
	for _,appID:=range []string{"com.example.A","com.example.B"}{
		if _,err:=manager.UseApp(context.Background(),"session",appID,false,AppSelection{});err!=nil{t.Fatal(err)}
	}
	unlock:=holdComputerWrite(t,manager);defer unlock()
	waiting:=&computerWriteWaitContext{Context:context.Background(),waiting:make(chan struct{})}
	done:=make(chan error,1);go func(){done<-manager.ReleaseSession(waiting,"session")}()
	awaitComputerLifecycle(t,waiting.waiting)
	time.Sleep(2100*time.Millisecond)
	unlock()
	if err:=awaitComputerLifecycle(t,done);!errors.Is(err,context.DeadlineExceeded){t.Fatalf("expected first quit timeout: %v",err)}
	if len(budgets)!=2 {t.Fatalf("quit count=%d",len(budgets))}
	for _,budget:=range budgets {if budget<1900*time.Millisecond || budget>2*time.Second{t.Fatalf("quit budget=%v",budget)}}
}

func TestManagerReleaseStopsQueuedAppUse(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	unlock := holdComputerWrite(t, manager)
	defer unlock()
	ctx := &computerWriteWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := manager.UseApp(ctx, "session_a", "com.example.App", false, AppSelection{})
		done <- err
	}()
	awaitComputerLifecycle(t, ctx.waiting)
	if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
		t.Fatal(err)
	}
	unlock()
	err := awaitComputerLifecycle(t, done)
	assertOperationCode(t, err, "computer_action_cancelled")
	if failure := ErrorFailure(err); failure.Outcome != "not_started" || service.uses != 0 || len(manager.launches) != 0 {
		t.Fatalf("queued app entry ran or retained state: failure=%+v uses=%d", failure, service.uses)
	}
}

func TestManagerReleaseHandlesLateAppEntry(t *testing.T) {
	foregroundFailure := &Failure{Code: "computer_activation_failed", Message: "could not activate", Outcome: "unknown"}
	for _, tc := range []struct {
		name            string
		newlyLaunched   bool
		closed          bool
		foregroundError *Failure
	}{
		{name: "new launch", newlyLaunched: true, closed: true},
		{name: "already running", closed: true},
		{name: "new launch needs attention", newlyLaunched: true},
		{name: "new launch foreground failed", newlyLaunched: true, closed: true, foregroundError: foregroundFailure},
		{name: "already running foreground failed", closed: true, foregroundError: foregroundFailure},
		{name: "new launch malformed foreground error", newlyLaunched: true, closed: true, foregroundError: &Failure{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			started, finish := make(chan struct{}), make(chan struct{})
			var finishOnce sync.Once
			resume := func() { finishOnce.Do(func() { close(finish) }) }
			t.Cleanup(resume)
			service := &computerLifecycleService{}
			service.use = func(context.Context, string, string, bool, AppSelection) (NativeUse, error) {
				close(started)
				<-finish
				return NativeUse{AppID: "com.example.App", Name: "Example", PID: 42, NewlyLaunched: tc.newlyLaunched, ForegroundError: tc.foregroundError, WindowStatus: WindowStatusNone}, nil
			}
			cleanupBudgetValid := false
			service.quit = func(ctx context.Context, _ string, appID string, pid int32) (NativeQuit, error) {
				deadline, ok := ctx.Deadline()
				cleanupBudgetValid = ok && ctx.Err() == nil && time.Until(deadline) > 0 && time.Until(deadline) <= 2*time.Second
				service.quitRequests = append(service.quitRequests, NativeQuit{AppID: appID, PID: pid})
				return NativeQuit{AppID: appID, PID: pid, Closed: tc.closed}, nil
			}
			manager := NewManager(service)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				_, err := manager.UseApp(ctx, "session_a", "com.example.App", tc.foregroundError != nil, AppSelection{})
				done <- err
			}()
			awaitComputerLifecycle(t, started)
			if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
				t.Fatal(err)
			}
			cancel()
			resume()
			err := awaitComputerLifecycle(t, done)
			assertOperationCode(t, err, "computer_action_cancelled")
			if failure := ErrorFailure(err); failure.Outcome != "unknown" || len(manager.launches) != 0 {
				t.Fatalf("late app entry revived ownership: %+v", failure)
			}
			if tc.newlyLaunched {
				if len(service.quitRequests) != 1 || service.quitRequests[0].AppID != "com.example.App" || service.quitRequests[0].PID != 42 || !cleanupBudgetValid {
					t.Fatalf("late cleanup = %+v budgetValid=%t", service.quitRequests, cleanupBudgetValid)
				}
				if !tc.closed && !strings.Contains(err.Error(), "user attention") {
					t.Fatalf("cleanup refusal was hidden: %v", err)
				}
			} else if len(service.quitRequests) != 0 {
				t.Fatalf("already-running app was quit: %+v", service.quitRequests)
			}
		})
	}
}

func TestManagerReleaseDoesNotRepeatAnInFlightSuccessfulQuit(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	resume := func() { finishOnce.Do(func() { close(finish) }) }
	t.Cleanup(resume)
	service := &computerLifecycleService{}
	service.quit = func(_ context.Context, _ string, appID string, pid int32) (NativeQuit, error) {
		service.quitRequests = append(service.quitRequests, NativeQuit{AppID: appID, PID: pid})
		if len(service.quitRequests) == 1 {
			close(started)
			<-finish
		}
		return NativeQuit{AppID: appID, PID: pid, Closed: true}, nil
	}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
	if err != nil {
		t.Fatal(err)
	}
	quitDone := make(chan error, 1)
	go func() {
		_, err := manager.QuitApp(context.Background(), "session_a", *launched.LaunchID)
		quitDone <- err
	}()
	awaitComputerLifecycle(t, started)
	ctx := &computerWriteWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	releaseDone := make(chan error, 1)
	go func() { releaseDone <- manager.ReleaseSession(ctx, "session_a") }()
	awaitComputerLifecycle(t, ctx.waiting)
	if _, ok := manager.OwnedLaunchAppID("session_a", *launched.LaunchID); ok {
		t.Fatal("release did not revoke ownership while waiting")
	}
	resume()
	if err := awaitComputerLifecycle(t, quitDone); err != nil {
		t.Fatal(err)
	}
	if err := awaitComputerLifecycle(t, releaseDone); err != nil {
		t.Fatal(err)
	}
	if len(service.quitRequests) != 1 || len(manager.launches) != 0 {
		t.Fatalf("duplicate quit or retained state: %+v", service.quitRequests)
	}
}

func TestManagerLateAppEntryCannotClearANewSessionLease(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	var finishOnce sync.Once
	resume := func() { finishOnce.Do(func() { close(finish) }) }
	t.Cleanup(resume)
	service := &computerLifecycleService{}
	entries := 0
	service.use = func(context.Context, string, string, bool, AppSelection) (NativeUse, error) {
		entries++
		pid := int32(99)
		if entries == 1 {
			close(started)
			<-finish
			pid = 42
		}
		return NativeUse{AppID: "com.example.App", Name: "Example", PID: pid, NewlyLaunched: true, WindowStatus: WindowStatusNone}, nil
	}
	manager := NewManager(service)
	oldDone := make(chan error, 1)
	go func() {
		_, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{})
		oldDone <- err
	}()
	awaitComputerLifecycle(t, started)
	if err := manager.ReleaseSession(context.Background(), "session_a"); err != nil {
		t.Fatal(err)
	}
	ctx := &computerWriteWaitContext{Context: context.Background(), waiting: make(chan struct{})}
	newDone := make(chan UseResult, 1)
	newErr := make(chan error, 1)
	go func() {
		result, err := manager.UseApp(ctx, "session_a", "com.example.App", false, AppSelection{})
		newDone <- result
		newErr <- err
	}()
	awaitComputerLifecycle(t, ctx.waiting)
	resume()
	assertOperationCode(t, awaitComputerLifecycle(t, oldDone), "computer_action_cancelled")
	result := awaitComputerLifecycle(t, newDone)
	if err := awaitComputerLifecycle(t, newErr); err != nil {
		t.Fatal(err)
	}
	if result.LaunchID == nil || result.PID != 99 {
		t.Fatalf("new app entry = %+v", result)
	}
	if _, ok := manager.OwnedLaunchAppID("session_a", *result.LaunchID); !ok {
		t.Fatal("late app entry removed the new session's ownership")
	}
	if len(service.quitRequests) != 1 || service.quitRequests[0].PID != 42 {
		t.Fatalf("late cleanup targeted the new process: %+v", service.quitRequests)
	}
}

func TestManagerReleaseReportsAppsThatDidNotClose(t *testing.T) {
	for _, response := range []NativeQuit{
		{AppID: "com.example.App", PID: 42, Closed: false},
		{AppID: "com.example.App", PID: 99, Closed: true},
	} {
		service := &fakeService{quits: []NativeQuit{response}}
		manager := NewManager(service)
		if _, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false, AppSelection{}); err != nil {
			t.Fatal(err)
		}
		err := manager.ReleaseSession(context.Background(), "session_a")
		if err == nil || !strings.Contains(err.Error(), "com.example.App") || !strings.Contains(err.Error(), "PID 42") {
			t.Fatalf("release result = %v", err)
		}
		if len(service.quitRequests) != 1 || service.quitRequests[0].PID != 42 || len(manager.launches) != 0 {
			t.Fatalf("cleanup retried or retained ownership: %+v", service.quitRequests)
		}
	}
}

type computerLifecycleService struct {
	fakeService
	use  func(context.Context, string, string, bool, AppSelection) (NativeUse, error)
	quit func(context.Context, string, string, int32) (NativeQuit, error)
}

func (s *computerLifecycleService) UseApp(ctx context.Context, sessionID, appID string, foreground bool, selection AppSelection) (NativeUse, error) {
	if s.use != nil {
		return s.use(ctx, sessionID, appID, foreground, selection)
	}
	return s.fakeService.UseApp(ctx, sessionID, appID, foreground, selection)
}

func (s *computerLifecycleService) QuitApp(ctx context.Context, sessionID, appID string, pid int32) (NativeQuit, error) {
	if s.quit != nil {
		return s.quit(ctx, sessionID, appID, pid)
	}
	return s.fakeService.QuitApp(ctx, sessionID, appID, pid)
}

// Signal after an app entry has registered its lease and reaches the write
// queue, without scheduling sleeps or adding hooks to production code.
type computerWriteWaitContext struct {
	context.Context
	waiting chan struct{}
	once    sync.Once
}

func (c *computerWriteWaitContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.waiting) })
	return c.Context.Done()
}

func holdComputerWrite(t *testing.T, manager *Manager) func() {
	t.Helper()
	if err := manager.acquireWrite(context.Background(), "test"); err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	unlock := func() { once.Do(manager.releaseWrite) }
	t.Cleanup(unlock)
	return unlock
}

func awaitComputerLifecycle[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("Computer Use lifecycle operation did not finish")
		var zero T
		return zero
	}
}

func TestManagerReturnsStatelessAtomicObserveCapture(t *testing.T) {
	manager := NewManager(&fakeService{})
	result, err := manager.ObserveCapture(
		context.Background(), "session_a", "com.example.App", 42, 20, "/tmp/window.png", true,
	)
	if err != nil || result.Observation.AppID != "com.example.App" || result.Capture == nil || result.Capture.WindowID != 42 {
		t.Fatalf("unexpected observe-capture: %#v err=%v", result, err)
	}
	acted, err := manager.Act(
		context.Background(), "session_a", "com.example.App", 42,
		semanticActions("button", ActionPress, nil),
	)
	if err != nil || len(acted.Actions) != 1 || !acted.Actions[0].Completed {
		t.Fatalf("unexpected chained action: %#v err=%v", acted, err)
	}
}

func TestManagerDoesNotRetryNativeActionFailures(t *testing.T) {
	tests := []struct {
		name string
		err  *OperationError
	}{
		{name: "permission revoked", err: &OperationError{
			Code: "computer_permission_required", Message: "permission required: accessibility", Permission: "accessibility", Outcome: "not_started",
		}},
		{name: "application exited", err: &OperationError{
			Code: "computer_app_not_found", Message: "application not found", Outcome: "not_started",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeService{actErr: tt.err}
			manager := NewManager(service)
			result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, semanticActions("button", ActionPress, nil))
			if err != nil || result.Failure == nil || result.Failure.Code != tt.err.Code {
				t.Fatalf("unexpected native failure: result=%#v err=%v", result, err)
			}
			if result.Failure.Outcome != "not_started" || result.Failure.Retryable {
				t.Fatalf("unexpected native failure: %#v", result.Failure)
			}
			if service.actions != 1 || service.observes != 0 {
				t.Fatalf("native failure retried work: actions=%d observes=%d", service.actions, service.observes)
			}
		})
	}
}

func TestManagerHidesSecureTextActionsFromObservation(t *testing.T) {
	manager := NewManager(&fakeService{})
	observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, element := range observed.Elements {
		if element.Secure && len(element.Actions) != 0 {
			t.Fatalf("secure actions = %v, want none", element.Actions)
		}
	}
}

func TestManagerRejectsOversizedActionValue(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	value := strings.Repeat("🙂", MaxActionValueCharacters+1)
	_, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, semanticActions("button", ActionSetValue, &value))
	assertOperationCode(t, err, "computer_invalid_request")
	if service.actions != 0 {
		t.Fatalf("actions = %d, want 0", service.actions)
	}
	if _, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, semanticActions("button", ActionPress, nil)); err != nil {
		t.Fatalf("valid action failed after oversized value: %v", err)
	}
}

func TestManagerSerializesActionsAcrossSessions(t *testing.T) {
	service := &serialActionService{started: make(chan string, 2), release: make(chan struct{}, 2)}
	manager := NewManager(service)
	done := make(chan error, 2)
	go func() {
		_, err := manager.Act(context.Background(), "session_a", "com.example.First", 41, semanticActions("button", ActionPress, nil))
		done <- err
	}()
	go func() {
		_, err := manager.Act(context.Background(), "session_b", "com.example.Second", 42, semanticActions("button", ActionPress, nil))
		done <- err
	}()
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("first action did not start")
	}
	select {
	case appID := <-service.started:
		t.Fatalf("second action started before first completed: %s", appID)
	case <-time.After(50 * time.Millisecond):
	}
	service.release <- struct{}{}
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("second action did not start after first completed")
	}
	service.release <- struct{}{}
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

func TestManagerCancelledQueuedActionNeverReachesNativeService(t *testing.T) {
	service := &serialActionService{started: make(chan string, 2), release: make(chan struct{}, 1)}
	manager := NewManager(service)
	firstDone := make(chan error, 1)
	go func() {
		_, err := manager.Act(context.Background(), "session_a", "com.example.First", 41, semanticActions("button", ActionPress, nil))
		firstDone <- err
	}()
	select {
	case <-service.started:
	case <-time.After(time.Second):
		t.Fatal("first action did not reach the native service")
	}

	queuedContext, cancelQueued := context.WithCancel(context.Background())
	queuedDone := make(chan error, 1)
	go func() {
		_, err := manager.Act(queuedContext, "session_b", "com.example.Second", 42, semanticActions("button", ActionPress, nil))
		queuedDone <- err
	}()
	cancelQueued()
	select {
	case err := <-queuedDone:
		assertOperationCode(t, err, "computer_action_cancelled")
		if failure := ErrorFailure(err); failure.Outcome != "not_started" {
			t.Fatalf("queued cancellation outcome = %q, want not_started", failure.Outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("queued action did not cancel")
	}

	service.release <- struct{}{}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	select {
	case appID := <-service.started:
		t.Fatalf("cancelled action reached native service: %s", appID)
	case <-time.After(50 * time.Millisecond):
	}
}

type serialActionService struct {
	started chan string
	release chan struct{}
}

func (s *serialActionService) Permissions(context.Context) (Permissions, error) {
	return Permissions{}, nil
}
func (s *serialActionService) ListApps(context.Context, string) (AppList, error) {
	return AppList{}, nil
}
func (s *serialActionService) UseApp(context.Context, string, string, bool, AppSelection) (NativeUse, error) {
	return NativeUse{}, nil
}
func (s *serialActionService) QuitApp(context.Context, string, string, int32) (NativeQuit, error) {
	return NativeQuit{}, nil
}
func (s *serialActionService) Observe(_ context.Context, _ string, appID string, windowID uint32, _ int) (Observation, error) {
	return testObservation(appID, windowID), nil
}
func (s *serialActionService) ObserveCapture(_ context.Context, _ string, appID string, windowID uint32, _ int, output string, includeAccessibility bool) (NativeObservationCapture, error) {
	return NativeObservationCapture{
		Observation: observationPointer(testObservation(appID, windowID)),
		Capture:     &Capture{WindowID: windowID, Output: output, Width: 1, Height: 1, ScaleFactor: 1},
	}, nil
}
func (s *serialActionService) Act(_ context.Context, _ string, appID string, _ uint32, elementID, action string, _ *string) (NativeAction, error) {
	s.started <- appID
	<-s.release
	return NativeAction{AppID: appID, ElementID: elementID, Action: action, Completed: true}, nil
}
func (s *serialActionService) Pointer(_ context.Context, _ string, appID string, _ uint32, pointer PointerInput) (NativeAction, error) {
	s.started <- appID
	<-s.release
	return NativeAction{AppID: appID, Action: pointer.Action, Completed: true, X: &pointer.X, Y: &pointer.Y, ToX: pointer.ToX, ToY: pointer.ToY, Button: pointer.Button, ClickCount: pointer.ClickCount, DeltaX: pointer.DeltaX, DeltaY: pointer.DeltaY}, nil
}
func (s *serialActionService) ReleaseSession(context.Context, string) error { return nil }

func testObservation(appID string, windowID uint32) Observation {
	return Observation{
		AppID:    appID,
		WindowID: &windowID,
		Elements: []Element{
			{ElementID: "button", WindowID: &windowID, Actions: []string{ActionPress}},
			{ElementID: "row", WindowID: &windowID, Actions: []string{ActionSelect}},
			{ElementID: "field", WindowID: &windowID, Actions: []string{ActionSetValue, ActionSubmit}},
			{ElementID: "secure", WindowID: &windowID, Secure: true, Actions: []string{ActionPress, ActionSetValue, ActionSelect, ActionSubmit}},
		},
	}
}

func assertOperationCode(t *testing.T, err error, code string) {
	t.Helper()
	var operationErr *OperationError
	if !errors.As(err, &operationErr) || operationErr.Code != code {
		t.Fatalf("error = %#v, want code %q", err, code)
	}
}

func observationPointer(o Observation) *Observation { return &o }
func (f *fakeService) Keyboard(_ context.Context, sessionID, appID string, _ uint32, input ActionInput) (NativeAction, error) {
	f.actions++
	f.lastSession = sessionID
	if f.actErr != nil && (f.actErrAt == 0 || f.actions == f.actErrAt) {
		return NativeAction{}, f.actErr
	}
	return NativeAction{AppID: appID, Action: input.Type, Completed: true, Key: input.Key, Modifiers: input.Modifiers}, nil
}
func (s *serialActionService) Keyboard(context.Context, string, string, uint32, ActionInput) (NativeAction, error) {
	return NativeAction{}, nil
}

type channelService struct {
	fakeService
	result NativeObservationCapture
}

func (s *channelService) ObserveCapture(context.Context, string, string, uint32, int, string, bool) (NativeObservationCapture, error) {
	return s.result, nil
}
func TestObservationChannelsFailIndependently(t *testing.T) {
	observation := testObservation("com.example.App", 42)
	capture := &Capture{WindowID: 42, Output: "/tmp/test.png", Width: 2, Height: 2, ScaleFactor: 1}
	failure := &Failure{Code: "computer_permission_required", Message: "AX denied", Outcome: "not_started"}
	for _, tt := range []struct {
		name    string
		ax      bool
		result  NativeObservationCapture
		invalid bool
	}{
		{"image despite AX error", true, NativeObservationCapture{Capture: capture, ObservationError: failure}, false},
		{"AX despite image error", true, NativeObservationCapture{Observation: &observation, CaptureError: failure}, false},
		{"image only", false, NativeObservationCapture{Capture: capture}, false},
		{"both errors", true, NativeObservationCapture{ObservationError: failure, CaptureError: failure}, false},
		{"missing channel status", true, NativeObservationCapture{Capture: capture}, true},
		{"unexpected AX", false, NativeObservationCapture{Capture: capture, Observation: &observation}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			manager := NewManager(&channelService{result: tt.result})
			result, err := manager.ObserveCapture(context.Background(), "session", "com.example.App", 42, 200, "/tmp/test.png", tt.ax)
			if (err != nil) != tt.invalid {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}
func TestKeyboardBatchStopsWithoutReplayingPrefix(t *testing.T) {
	value := "你好🙂"
	service := &fakeService{actErrAt: 2, actErr: &OperationError{Code: "computer_app_not_foreground", Message: "focus changed", Outcome: "not_started"}}
	result, err := NewManager(service).Act(context.Background(), "session", "com.example.App", 42, []ActionInput{
		{Type: ActionTypeText, Value: &value}, {Type: ActionPressKey, Key: "enter"}, {Type: ActionPressKey, Key: "tab"},
	})
	if err != nil || result.CompletedCount != 1 || result.FailedIndex == nil || *result.FailedIndex != 1 || service.actions != 2 {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, service.actions)
	}
}
func TestKeyboardRejectsMixedTargetsBeforeDispatch(t *testing.T) {
	value := "text"
	x := 0.5
	for _, input := range []ActionInput{
		{Type: ActionTypeText, Value: &value, ElementID: "field"},
		{Type: ActionPressKey, Key: "a", X: &x}, {Type: ActionPressKey, Key: "a", Value: &value},
		{Type: ActionPressKey, Key: "a", Modifiers: []string{"command", "command"}},
		{Type: ActionSetValue, ElementID: "field", Value: &value, Key: "a"},
	} {
		if _, err := NormalizeActions([]ActionInput{input}); err == nil {
			t.Errorf("accepted %+v", input)
		}
	}
}

func TestKeyboardRejectsUnsupportedKeysBeforeExecutingBatchPrefix(t *testing.T) {
	for _, key := range []string{"f13", "return", "command+v", "🙂", "aa", " ", "!"} {
		t.Run(key, func(t *testing.T) {
			// Match the Helper's rejection of an unsupported physical key. A
			// static argument error must be found before the earlier click runs.
			service := &fakeService{actErrAt: 1, actErr: invalid("unsupported physical key")}
			x, y := 0.5, 0.5
			result, err := NewManager(service).Act(context.Background(), "session", "com.example.App", 42, []ActionInput{
				{Type: ActionClick, X: &x, Y: &y},
				{Type: ActionPressKey, Key: key},
			})
			var operationErr *OperationError
			if !errors.As(err, &operationErr) || operationErr.Code != "computer_invalid_request" || operationErr.Outcome != "not_started" {
				t.Errorf("expected pre-dispatch rejection: result=%+v err=%v", result, err)
			}
			if service.pointers != 0 || service.actions != 0 || service.observes != 0 || service.uses != 0 {
				t.Fatalf("invalid batch reached native service: clicks=%d actions=%d observes=%d uses=%d", service.pointers, service.actions, service.observes, service.uses)
			}
		})
	}
}

func TestKeyboardNormalizesAllSupportedPhysicalKeys(t *testing.T) {
	keys := strings.Fields("enter tab space backspace escape home pageup delete end pagedown left right down up f1 f2 f3 f4 f5 f6 f7 f8 f9 f10 f11 f12")
	for _, key := range "abcdefghijklmnopqrstuvwxyz0123456789`-=[]\\;',./" {
		keys = append(keys, string(key))
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			service := &fakeService{}
			result, err := NewManager(service).Act(context.Background(), "session", "com.example.App", 42, []ActionInput{
				{Type: ActionPressKey, Key: strings.ToUpper(key), Modifiers: []string{"command", "shift"}},
			})
			if err != nil || result.Failure != nil || result.CompletedCount != 1 || service.actions != 1 {
				t.Fatalf("supported key failed: result=%+v err=%v actions=%d", result, err, service.actions)
			}
			if result.Actions[0].Key != key || strings.Join(result.Actions[0].Modifiers, ",") != "command,shift" {
				t.Fatalf("key or modifiers changed: %+v", result.Actions[0])
			}
		})
	}
}

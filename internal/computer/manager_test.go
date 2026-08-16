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
	actions      int
	pointers     int
	lastPointer  ScreenshotPointer
	observeErr   error
	actErr       error
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

func (f *fakeService) UseApp(_ context.Context, sessionID, appID string, foreground bool) (NativeUse, error) {
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

func (f *fakeService) ObserveCapture(_ context.Context, _ string, appID string, windowID uint32, _ int, output string) (NativeObservationCapture, error) {
	return NativeObservationCapture{
		Observation: testObservation(appID, windowID),
		Capture:     &Capture{WindowID: windowID, Output: output, Width: 1, Height: 1, ScaleFactor: 1},
	}, nil
}

func (f *fakeService) Act(_ context.Context, sessionID, appID string, _ uint32, elementID, action string, _ *string) (NativeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.actions++
	f.lastSession = sessionID
	if f.actErr != nil {
		return NativeAction{}, f.actErr
	}
	return NativeAction{AppID: appID, ElementID: elementID, Action: action, Completed: true}, nil
}

func (f *fakeService) Pointer(_ context.Context, sessionID, appID string, _ uint32, pointer ScreenshotPointer) (NativeAction, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pointers++
	f.lastPointer = pointer
	f.lastSession = sessionID
	return NativeAction{
		AppID: appID, Action: pointer.Action, Completed: true, X: &pointer.X, Y: &pointer.Y,
		ToX: pointer.ToX, ToY: pointer.ToY, Button: pointer.Button,
		ClickCount: pointer.ClickCount, DeltaX: pointer.DeltaX, DeltaY: pointer.DeltaY,
	}, nil
}

func (f *fakeService) ReleaseSession(_ context.Context, sessionID string) error {
	f.lastSession = sessionID
	return nil
}

func TestManagerRoutesSessionAndConsumesObservationOnce(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Action.Completed || result.Observation == nil || service.lastSession != "session_a" {
		t.Fatalf("unexpected result: %#v session=%q", result, service.lastSession)
	}
	_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
	assertOperationCode(t, err, "computer_observation_not_found")
	if service.actions != 1 {
		t.Fatalf("actions = %d, want 1", service.actions)
	}
}

func TestManagerPointerRequiresAndConsumesScreenshotObservation(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)

	plain, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Pointer(context.Background(), "session_a", "com.example.App", 42, plain.ObservationID, PointerInput{Action: ActionClick, X: 0, Y: 0, Button: PointerButtonLeft, ClickCount: 1})
	assertOperationCode(t, err, "computer_coordinate_source_required")
	if service.pointers != 0 {
		t.Fatalf("native pointer actions = %d, want 0", service.pointers)
	}

	captured, err := manager.ObserveCapture(context.Background(), "session_a", "com.example.App", 42, 20, "/tmp/click.png")
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Pointer(context.Background(), "session_a", "com.example.App", 42, captured.Observation.ObservationID, PointerInput{Action: ActionClick, X: 0.5, Y: 0.5, Button: PointerButtonLeft, ClickCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Action.Completed || result.Action.Action != ActionClick || result.Action.ClickCount != 2 || result.Observation == nil || service.pointers != 1 {
		t.Fatalf("unexpected pointer result: %#v pointers=%d", result, service.pointers)
	}
	_, err = manager.Pointer(context.Background(), "session_a", "com.example.App", 42, captured.Observation.ObservationID, PointerInput{Action: ActionClick, X: 0.5, Y: 0.5, Button: PointerButtonLeft, ClickCount: 1})
	assertOperationCode(t, err, "computer_observation_not_found")
}

func TestManagerPointerRejectsOutOfBounds(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	captured, err := manager.ObserveCapture(context.Background(), "session_a", "com.example.App", 42, 20, "/tmp/click.png")
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Pointer(context.Background(), "session_a", "com.example.App", 42, captured.Observation.ObservationID, PointerInput{Action: ActionClick, X: 1, Y: 0, Button: PointerButtonLeft, ClickCount: 1})
	assertOperationCode(t, err, "computer_coordinate_out_of_bounds")
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
		captured, err := manager.ObserveCapture(context.Background(), "session_a", "com.example.App", 42, 20, "/tmp/pointer.png")
		if err != nil {
			t.Fatal(err)
		}
		result, err := manager.Pointer(context.Background(), "session_a", "com.example.App", 42, captured.Observation.ObservationID, pointer)
		if err != nil || result.Action.Action != pointer.Action || service.pointers != 1 {
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
			observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
			if err != nil {
				t.Fatal(err)
			}
			result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, tc.elementID, tc.action, nil)
			if err != nil {
				t.Fatal(err)
			}
			if result.Action.Action != tc.action || !result.Action.Completed {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
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
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", true)
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
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false)
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

func TestManagerRejectsWindowFromAnotherProcess(t *testing.T) {
	appID := "com.example.App"
	service := &fakeService{launch: NativeUse{
		AppID:        appID,
		Name:         "Example",
		PID:          42,
		WindowStatus: WindowStatusReady,
		Windows:      []CapturableWindow{{WindowID: 8, PID: 99, AppID: &appID, Frame: Frame{Width: 100, Height: 80}}},
	}}
	_, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false)
	assertOperationCode(t, err, "computer_invalid_response")
}

func TestManagerAcceptsUseWithoutWindowsWhenPermissionIsMissing(t *testing.T) {
	appID := "com.example.App"
	service := &fakeService{launch: NativeUse{
		AppID:         appID,
		Name:          "Example",
		PID:           42,
		NewlyLaunched: true,
		WindowStatus:  WindowStatusPermissionRequired,
	}}
	used, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false)
	if err != nil || used.LaunchID == nil || used.WindowStatus != WindowStatusPermissionRequired || len(used.Windows) != 0 {
		t.Fatalf("unexpected use result: %#v err=%v", used, err)
	}
}

func TestManagerRequiresFailureDetailsForFailedWindowDiscovery(t *testing.T) {
	appID := "com.example.App"
	service := &fakeService{launch: NativeUse{
		AppID: appID, Name: "Example", PID: 42, WindowStatus: WindowStatusFailed,
	}}
	_, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false)
	assertOperationCode(t, err, "computer_invalid_response")

	service.launch.WindowError = &Failure{Code: "computer_unavailable", Message: "window discovery failed", Outcome: "not_started"}
	used, err := NewManager(service).UseApp(context.Background(), "session_a", appID, false)
	if err != nil || used.WindowError == nil || used.WindowError.Code != "computer_unavailable" {
		t.Fatalf("unexpected failed discovery: %#v err=%v", used, err)
	}
}

func TestManagerKeepsOwnershipWhenNormalQuitNeedsAttention(t *testing.T) {
	service := &fakeService{quits: []NativeQuit{{AppID: "com.example.App", PID: 42, Closed: false}, {AppID: "com.example.App", PID: 42, Closed: true}}}
	manager := NewManager(service)
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false)
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
	launched, err := manager.UseApp(context.Background(), "session_a", "com.example.App", false)
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

func TestManagerRejectsCrossSessionAndExpiredObservation(t *testing.T) {
	manager := NewManager(&fakeService{})
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Act(context.Background(), "session_b", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
	assertOperationCode(t, err, "computer_observation_not_found")

	observed, err = manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(observationTTL)
	_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
	assertOperationCode(t, err, "computer_observation_stale")
}

func TestManagerKeepsObservationUsableBeyondThirtySeconds(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	if observed.ExpiresAt != now.Add(2*time.Minute).Format(time.RFC3339Nano) {
		t.Fatalf("expiresAt = %q, want a two-minute lifetime", observed.ExpiresAt)
	}

	now = now.Add(31 * time.Second)
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Action.Completed || result.Observation == nil {
		t.Fatalf("unexpected action result: %#v", result)
	}
}

func TestManagerExpiresScreenshotCoordinatesAfterFifteenSeconds(t *testing.T) {
	manager := NewManager(&fakeService{})
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return now }
	observed, err := manager.ObserveCapture(context.Background(), "session_a", "com.example.App", 42, 20, "/tmp/pointer.png")
	if err != nil {
		t.Fatal(err)
	}
	if observed.Observation.ExpiresAt != now.Add(captureObservationTTL).Format(time.RFC3339Nano) {
		t.Fatalf("expiresAt = %q, want a fifteen-second screenshot lifetime", observed.Observation.ExpiresAt)
	}

	now = now.Add(captureObservationTTL)
	_, err = manager.Pointer(context.Background(), "session_a", "com.example.App", 42, observed.Observation.ObservationID, PointerInput{
		Action: ActionClick, X: 0.5, Y: 0.5, Button: PointerButtonLeft, ClickCount: 1,
	})
	assertOperationCode(t, err, "computer_observation_stale")
}

func TestManagerReportsCompletedActionWhenReobserveFails(t *testing.T) {
	service := &fakeService{observeErr: &OperationError{Code: "computer_helper_crashed", Message: "crashed", Retryable: true, Outcome: "unknown"}}
	manager := NewManager(service)
	observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	result, err := manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ObservationError == nil || result.ObservationError.Outcome != "completed" || result.ObservationError.Retryable {
		t.Fatalf("unexpected observation error: %#v", result.ObservationError)
	}
}

func TestManagerStoresObservationFromAtomicObserveCapture(t *testing.T) {
	manager := NewManager(&fakeService{})
	result, err := manager.ObserveCapture(
		context.Background(), "session_a", "com.example.App", 42, 20, "/tmp/window.png",
	)
	if err != nil || result.Observation.ObservationID == "" || result.Capture == nil || result.Capture.WindowID != 42 {
		t.Fatalf("unexpected observe-capture: %#v err=%v", result, err)
	}
	acted, err := manager.Act(
		context.Background(), "session_a", "com.example.App", 42,
		result.Observation.ObservationID, "button", ActionPress, nil,
	)
	if err != nil || !acted.Action.Completed {
		t.Fatalf("unexpected chained action: %#v err=%v", acted, err)
	}
}

func TestManagerDoesNotRetryNativeActionFailures(t *testing.T) {
	tests := []struct {
		name string
		err  *OperationError
	}{
		{name: "permission revoked", err: &OperationError{
			Code: "computer_permission_required", Message: "permission required: accessibility", Outcome: "not_started",
		}},
		{name: "application exited", err: &OperationError{
			Code: "computer_app_not_found", Message: "application not found", Outcome: "not_started",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &fakeService{actErr: tt.err}
			manager := NewManager(service)
			observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
			if err != nil {
				t.Fatal(err)
			}
			_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
			assertOperationCode(t, err, tt.err.Code)
			failure := ErrorFailure(err)
			if failure.Outcome != "not_started" || failure.Retryable {
				t.Fatalf("unexpected native failure: %#v", failure)
			}
			_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil)
			assertOperationCode(t, err, "computer_observation_not_found")
			if service.actions != 1 || service.observes != 1 {
				t.Fatalf("native failure retried work: actions=%d observes=%d", service.actions, service.observes)
			}
		})
	}
}

func TestManagerBlocksSecureTextActionsBeforeNativeAction(t *testing.T) {
	for _, action := range []string{ActionPress, ActionSetValue, ActionSelect, ActionSubmit} {
		t.Run(action, func(t *testing.T) {
			service := &fakeService{}
			manager := NewManager(service)
			observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
			if err != nil {
				t.Fatal(err)
			}
			for _, element := range observed.Snapshot.Elements {
				if element.Secure && len(element.Actions) != 0 {
					t.Fatalf("secure actions = %v, want none", element.Actions)
				}
			}
			var value *string
			if action == ActionSetValue {
				secret := "secret"
				value = &secret
			}
			_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "secure", action, value)
			assertOperationCode(t, err, "computer_element_not_actionable")
			if service.actions != 0 {
				t.Fatalf("actions = %d, want 0", service.actions)
			}
		})
	}
}

func TestManagerRejectsOversizedActionValueBeforeConsumingObservation(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	observed, err := manager.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("🙂", MaxActionValueCharacters+1)
	_, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionSetValue, &value)
	assertOperationCode(t, err, "computer_invalid_request")
	if service.actions != 0 {
		t.Fatalf("actions = %d, want 0", service.actions)
	}
	if _, err = manager.Act(context.Background(), "session_a", "com.example.App", 42, observed.ObservationID, "button", ActionPress, nil); err != nil {
		t.Fatalf("oversized value consumed observation: %v", err)
	}
}

func TestManagerSerializesActionsAcrossSessions(t *testing.T) {
	service := &serialActionService{started: make(chan string, 2), release: make(chan struct{}, 2)}
	manager := NewManager(service)
	first, err := manager.Observe(context.Background(), "session_a", "com.example.First", 41, 20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Observe(context.Background(), "session_b", "com.example.Second", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 2)
	go func() {
		_, err := manager.Act(context.Background(), "session_a", "com.example.First", 41, first.ObservationID, "button", ActionPress, nil)
		done <- err
	}()
	go func() {
		_, err := manager.Act(context.Background(), "session_b", "com.example.Second", 42, second.ObservationID, "button", ActionPress, nil)
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
	first, err := manager.Observe(context.Background(), "session_a", "com.example.First", 41, 20)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Observe(context.Background(), "session_b", "com.example.Second", 42, 20)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := manager.Act(context.Background(), "session_a", "com.example.First", 41, first.ObservationID, "button", ActionPress, nil)
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
		_, err := manager.Act(queuedContext, "session_b", "com.example.Second", 42, second.ObservationID, "button", ActionPress, nil)
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
func (s *serialActionService) UseApp(context.Context, string, string, bool) (NativeUse, error) {
	return NativeUse{}, nil
}
func (s *serialActionService) QuitApp(context.Context, string, string, int32) (NativeQuit, error) {
	return NativeQuit{}, nil
}
func (s *serialActionService) Observe(_ context.Context, _ string, appID string, windowID uint32, _ int) (Observation, error) {
	return testObservation(appID, windowID), nil
}
func (s *serialActionService) ObserveCapture(_ context.Context, _ string, appID string, windowID uint32, _ int, output string) (NativeObservationCapture, error) {
	return NativeObservationCapture{
		Observation: testObservation(appID, windowID),
		Capture:     &Capture{WindowID: windowID, Output: output, Width: 1, Height: 1, ScaleFactor: 1},
	}, nil
}
func (s *serialActionService) Act(_ context.Context, _ string, appID string, _ uint32, elementID, action string, _ *string) (NativeAction, error) {
	s.started <- appID
	<-s.release
	return NativeAction{AppID: appID, ElementID: elementID, Action: action, Completed: true}, nil
}
func (s *serialActionService) Pointer(_ context.Context, _ string, appID string, _ uint32, pointer ScreenshotPointer) (NativeAction, error) {
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

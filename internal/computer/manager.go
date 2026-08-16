package computer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	observationTTL            = 2 * time.Minute
	captureObservationTTL     = 15 * time.Second
	maxObservationsPerSession = 16
)

type observationRecord struct {
	managed     ManagedObservation
	sessionID   string
	appID       string
	windowID    uint32
	maxElements int
	actions     map[string]map[string]bool
	capture     *Capture
	expiresAt   time.Time
}

type launchRecord struct {
	launchID string
	appID    string
	name     string
	pid      int32
}

type Manager struct {
	service  Service
	now      func() time.Time
	write    chan struct{}
	mu       sync.Mutex
	items    map[string]map[string]observationRecord
	launches map[string]map[string]launchRecord
}

func NewManager(service Service) *Manager {
	return &Manager{
		service:  service,
		now:      time.Now,
		write:    make(chan struct{}, 1),
		items:    map[string]map[string]observationRecord{},
		launches: map[string]map[string]launchRecord{},
	}
}

func (m *Manager) ListApps(ctx context.Context, sessionID string) (AppList, error) {
	if err := validateSessionID(sessionID); err != nil {
		return AppList{}, err
	}
	apps, err := m.service.ListApps(ctx, sessionID)
	if err != nil {
		return AppList{}, err
	}
	seen := make(map[string]struct{}, len(apps.Apps))
	for _, app := range apps.Apps {
		if strings.TrimSpace(app.AppID) == "" || len(app.AppID) > MaxAppIDBytes || strings.TrimSpace(app.Name) == "" || app.Active && !app.Running {
			return AppList{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use app inventory returned an invalid application", Outcome: "not_started"}
		}
		if _, exists := seen[app.AppID]; exists {
			return AppList{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use app inventory returned duplicate application IDs", Outcome: "not_started"}
		}
		seen[app.AppID] = struct{}{}
	}
	return apps, nil
}

func (m *Manager) UseApp(ctx context.Context, sessionID, appID string, foreground bool) (UseResult, error) {
	if err := validateApp(sessionID, appID); err != nil {
		return UseResult{}, err
	}
	if err := m.acquireWrite(ctx, "app use"); err != nil {
		return UseResult{}, err
	}
	defer m.releaseWrite()
	candidateLaunchID, err := newLaunchID()
	if err != nil {
		return UseResult{}, err
	}

	native, err := m.service.UseApp(ctx, sessionID, appID, foreground)
	if err != nil {
		return UseResult{}, err
	}
	if native.AppID != appID || native.PID <= 0 || strings.TrimSpace(native.Name) == "" {
		return UseResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use app entry returned an invalid result", Outcome: "unknown"}
	}
	if !validWindowDiscovery(native.WindowStatus, native.WindowError, len(native.Windows)) {
		return UseResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use app entry returned an invalid window status", Outcome: "unknown"}
	}
	for _, window := range native.Windows {
		if window.WindowID == 0 || window.PID != native.PID || window.AppID == nil || *window.AppID != native.AppID || window.Frame.Width <= 0 || window.Frame.Height <= 0 {
			return UseResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use app entry returned an invalid window", Outcome: "unknown"}
		}
	}
	if !native.NewlyLaunched {
		if record, ok := m.findLaunch(sessionID, native.AppID, native.PID); ok {
			existingLaunchID := record.launchID
			return UseResult{LaunchID: &existingLaunchID, AppID: record.appID, Name: record.name, PID: record.pid, WindowStatus: native.WindowStatus, WindowError: native.WindowError, Windows: native.Windows}, nil
		}
		return UseResult{AppID: native.AppID, Name: native.Name, PID: native.PID, WindowStatus: native.WindowStatus, WindowError: native.WindowError, Windows: native.Windows}, nil
	}
	launchID := candidateLaunchID
	record := launchRecord{launchID: launchID, appID: native.AppID, name: native.Name, pid: native.PID}
	m.mu.Lock()
	launches := m.launches[sessionID]
	if launches == nil {
		launches = map[string]launchRecord{}
		m.launches[sessionID] = launches
	}
	launches[launchID] = record
	m.mu.Unlock()
	return UseResult{LaunchID: &launchID, AppID: native.AppID, Name: native.Name, PID: native.PID, WindowStatus: native.WindowStatus, WindowError: native.WindowError, Windows: native.Windows}, nil
}

func (m *Manager) QuitApp(ctx context.Context, sessionID, launchID string) (QuitResult, error) {
	if err := validateSessionID(sessionID); err != nil {
		return QuitResult{}, err
	}
	launchID = strings.TrimSpace(launchID)
	if launchID == "" {
		return QuitResult{}, invalid("launchID is required")
	}
	if err := m.acquireWrite(ctx, "quit"); err != nil {
		return QuitResult{}, err
	}
	defer m.releaseWrite()

	record, ok := m.getLaunch(sessionID, launchID)
	if !ok {
		return QuitResult{}, &OperationError{Code: "computer_launch_not_owned", Message: "application launch is not owned by this session", Outcome: "not_started"}
	}
	native, err := m.service.QuitApp(ctx, sessionID, record.appID, record.pid)
	if err != nil {
		return QuitResult{}, err
	}
	if native.AppID != record.appID || native.PID != record.pid {
		return QuitResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use quit returned an invalid result", Outcome: "unknown"}
	}
	if native.Closed {
		m.deleteLaunch(sessionID, launchID)
	}
	return QuitResult{LaunchID: launchID, AppID: record.appID, Name: record.name, PID: record.pid, Closed: native.Closed}, nil
}

func (m *Manager) OwnedLaunchAppID(sessionID, launchID string) (string, bool) {
	record, ok := m.getLaunch(strings.TrimSpace(sessionID), strings.TrimSpace(launchID))
	if !ok {
		return "", false
	}
	return record.appID, true
}

func (m *Manager) Observe(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int) (ManagedObservation, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ManagedObservation{}, err
	}
	if maxElements == 0 {
		maxElements = 200
	}
	if maxElements < 1 || maxElements > 1_000 {
		return ManagedObservation{}, invalid("maxElements must be between 1 and 1000")
	}
	snapshot, err := m.service.Observe(ctx, sessionID, appID, windowID, maxElements)
	if err != nil {
		return ManagedObservation{}, err
	}
	return m.storeObservation(sessionID, appID, windowID, maxElements, snapshot, nil)
}

func (m *Manager) ObserveCapture(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int, output string) (ManagedObservationCapture, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ManagedObservationCapture{}, err
	}
	if maxElements == 0 {
		maxElements = 200
	}
	if maxElements < 1 || maxElements > 1_000 {
		return ManagedObservationCapture{}, invalid("maxElements must be between 1 and 1000")
	}
	if strings.TrimSpace(output) == "" {
		return ManagedObservationCapture{}, invalid("output is required")
	}
	native, err := m.service.ObserveCapture(ctx, sessionID, appID, windowID, maxElements, output)
	if err != nil {
		return ManagedObservationCapture{}, err
	}
	if (native.Capture == nil) == (native.CaptureError == nil) {
		return ManagedObservationCapture{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use observe-capture returned an invalid capture result", Outcome: "unknown"}
	}
	if native.Capture != nil && !validCapture(*native.Capture, windowID, output) {
		return ManagedObservationCapture{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use capture returned an invalid result", Outcome: "unknown"}
	}
	if native.CaptureError != nil && (strings.TrimSpace(native.CaptureError.Code) == "" || strings.TrimSpace(native.CaptureError.Message) == "") {
		return ManagedObservationCapture{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use observe-capture returned an invalid capture error", Outcome: "unknown"}
	}
	managed, err := m.storeObservation(sessionID, appID, windowID, maxElements, native.Observation, native.Capture)
	if err != nil {
		return ManagedObservationCapture{}, err
	}
	return ManagedObservationCapture{Observation: managed, Capture: native.Capture, CaptureError: native.CaptureError}, nil
}

func validCapture(captured Capture, windowID uint32, output string) bool {
	return captured.WindowID == windowID && filepath.Clean(captured.Output) == filepath.Clean(output) && captured.Width > 0 && captured.Height > 0 && captured.ScaleFactor > 0
}

func (m *Manager) Act(ctx context.Context, sessionID, appID string, windowID uint32, observationID, elementID, action string, value *string) (ActionResult, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ActionResult{}, err
	}
	observationID = strings.TrimSpace(observationID)
	elementID = strings.TrimSpace(elementID)
	action = strings.TrimSpace(action)
	if observationID == "" || elementID == "" {
		return ActionResult{}, invalid("observationID and elementID are required")
	}
	if !validAction(action) {
		return ActionResult{}, invalid("action must be press, set_value, select, or submit")
	}
	if action == ActionSetValue && value == nil {
		return ActionResult{}, invalid("value is required for set_value")
	}
	if action != ActionSetValue && value != nil {
		return ActionResult{}, invalid("value is allowed only for set_value")
	}
	if value != nil && utf8.RuneCountInString(*value) > MaxActionValueCharacters {
		return ActionResult{}, invalid("value is too long")
	}

	if err := m.acquireWrite(ctx, "action"); err != nil {
		return ActionResult{}, err
	}
	defer m.releaseWrite()

	record, err := m.takeObservation(sessionID, observationID)
	if err != nil {
		return ActionResult{}, err
	}
	if record.appID != appID || record.windowID != windowID {
		return ActionResult{}, &OperationError{Code: "computer_observation_stale", Message: "observation does not match the requested application and window", Outcome: "not_started"}
	}
	allowed := record.actions[elementID]
	if allowed == nil {
		return ActionResult{}, &OperationError{Code: "computer_element_not_found", Message: "element is not present in the observation", Outcome: "not_started"}
	}
	if !allowed[action] {
		return ActionResult{}, &OperationError{Code: "computer_element_not_actionable", Message: "element does not support the requested action", Outcome: "not_started"}
	}

	native, err := m.service.Act(ctx, sessionID, appID, windowID, elementID, action, value)
	if err != nil {
		return ActionResult{}, err
	}
	if !native.Completed || native.AppID != appID || native.ElementID != elementID || native.Action != action {
		return ActionResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use action returned an invalid result", Outcome: "unknown"}
	}
	return m.finishAction(ctx, sessionID, appID, windowID, record, native), nil
}

func (m *Manager) Pointer(ctx context.Context, sessionID, appID string, windowID uint32, observationID string, pointer PointerInput) (ActionResult, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ActionResult{}, err
	}
	observationID = strings.TrimSpace(observationID)
	if observationID == "" {
		return ActionResult{}, invalid("observationID is required")
	}
	if err := validatePointerInput(pointer); err != nil {
		return ActionResult{}, err
	}
	if err := m.acquireWrite(ctx, "action"); err != nil {
		return ActionResult{}, err
	}
	defer m.releaseWrite()

	record, err := m.takeObservation(sessionID, observationID)
	if err != nil {
		return ActionResult{}, err
	}
	if record.appID != appID || record.windowID != windowID {
		return ActionResult{}, &OperationError{Code: "computer_observation_stale", Message: "observation does not match the requested application and window", Outcome: "not_started"}
	}
	if record.capture == nil {
		return ActionResult{}, &OperationError{Code: "computer_coordinate_source_required", Message: "pointer input requires a screenshot from the same observation", Outcome: "not_started"}
	}
	if !pointInCapture(pointer.X, pointer.Y, record.capture) ||
		(pointer.Action == ActionDrag && !pointInCapture(*pointer.ToX, *pointer.ToY, record.capture)) {
		return ActionResult{}, &OperationError{Code: "computer_coordinate_out_of_bounds", Message: "coordinate is outside the observed screenshot", Outcome: "not_started"}
	}
	screenshotPointer := ScreenshotPointer{
		PointerInput: pointer, CaptureWidth: record.capture.Width,
		CaptureHeight: record.capture.Height, ScaleFactor: record.capture.ScaleFactor,
	}
	native, err := m.service.Pointer(ctx, sessionID, appID, windowID, screenshotPointer)
	if err != nil {
		return ActionResult{}, err
	}
	if !matchesPointerResult(native, appID, pointer) {
		return ActionResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use pointer action returned an invalid result", Outcome: "unknown"}
	}
	return m.finishAction(ctx, sessionID, appID, windowID, record, native), nil
}

func validatePointerInput(pointer PointerInput) error {
	if math.IsNaN(pointer.X) || math.IsInf(pointer.X, 0) || math.IsNaN(pointer.Y) || math.IsInf(pointer.Y, 0) || pointer.X < 0 || pointer.Y < 0 {
		return invalid("x and y must be finite non-negative screenshot pixel coordinates")
	}
	switch pointer.Action {
	case ActionClick:
		if pointer.ToX != nil || pointer.ToY != nil || pointer.DeltaX != nil || pointer.DeltaY != nil {
			return invalid("click accepts only x, y, button, and clickCount")
		}
		if pointer.Button != PointerButtonLeft && pointer.Button != PointerButtonRight {
			return invalid("click button must be left or right")
		}
		if pointer.ClickCount != 1 && !(pointer.Button == PointerButtonLeft && pointer.ClickCount == 2) {
			return invalid("clickCount must be 1, or 2 for the left button")
		}
	case ActionDrag:
		if pointer.ToX == nil || pointer.ToY == nil || !finiteNonNegative(*pointer.ToX) || !finiteNonNegative(*pointer.ToY) {
			return invalid("drag requires finite non-negative toX and toY")
		}
		if pointer.Button != "" || pointer.ClickCount != 0 || pointer.DeltaX != nil || pointer.DeltaY != nil {
			return invalid("drag accepts only start and end coordinates")
		}
	case ActionScroll:
		if pointer.ToX != nil || pointer.ToY != nil || pointer.Button != "" || pointer.ClickCount != 0 || pointer.DeltaX == nil || pointer.DeltaY == nil {
			return invalid("scroll requires deltaX and deltaY and accepts no click or drag fields")
		}
		if (*pointer.DeltaX == 0 && *pointer.DeltaY == 0) || *pointer.DeltaX < -5_000 || *pointer.DeltaX > 5_000 || *pointer.DeltaY < -5_000 || *pointer.DeltaY > 5_000 {
			return invalid("scroll deltas must be non-zero in total and between -5000 and 5000")
		}
	default:
		return invalid("pointer action must be click, drag, or scroll")
	}
	return nil
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func pointInCapture(x, y float64, capture *Capture) bool {
	return x >= 0 && y >= 0 && x < float64(capture.Width) && y < float64(capture.Height)
}

func matchesPointerResult(native NativeAction, appID string, pointer PointerInput) bool {
	if !native.Completed || native.AppID != appID || native.ElementID != "" || native.Action != pointer.Action ||
		native.X == nil || native.Y == nil || *native.X != pointer.X || *native.Y != pointer.Y {
		return false
	}
	switch pointer.Action {
	case ActionClick:
		return native.ToX == nil && native.ToY == nil && native.Button == pointer.Button &&
			native.ClickCount == pointer.ClickCount && native.DeltaX == nil && native.DeltaY == nil
	case ActionDrag:
		return native.ToX != nil && native.ToY != nil && *native.ToX == *pointer.ToX && *native.ToY == *pointer.ToY &&
			native.Button == "" && native.ClickCount == 0 && native.DeltaX == nil && native.DeltaY == nil
	case ActionScroll:
		return native.ToX == nil && native.ToY == nil && native.Button == "" && native.ClickCount == 0 &&
			native.DeltaX != nil && native.DeltaY != nil && *native.DeltaX == *pointer.DeltaX && *native.DeltaY == *pointer.DeltaY
	default:
		return false
	}
}

func (m *Manager) finishAction(ctx context.Context, sessionID, appID string, windowID uint32, record observationRecord, native NativeAction) ActionResult {
	result := ActionResult{Action: native}
	latest, observeErr := m.service.Observe(ctx, sessionID, appID, windowID, record.maxElements)
	if observeErr != nil {
		failure := ErrorFailure(observeErr)
		failure.Outcome = "completed"
		failure.Retryable = false
		result.ObservationError = &failure
		return result
	}
	managed, storeErr := m.storeObservation(sessionID, appID, windowID, record.maxElements, latest, nil)
	if storeErr != nil {
		failure := ErrorFailure(storeErr)
		failure.Outcome = "completed"
		failure.Retryable = false
		result.ObservationError = &failure
		return result
	}
	result.Observation = &managed
	return result
}

func (m *Manager) ReleaseSession(ctx context.Context, sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if err := m.acquireWrite(ctx, "release"); err != nil {
		return err
	}
	defer m.releaseWrite()
	m.mu.Lock()
	owned := m.launches[sessionID]
	delete(m.items, sessionID)
	delete(m.launches, sessionID)
	m.mu.Unlock()
	var errs []error
	for _, record := range owned {
		if _, err := m.service.QuitApp(ctx, sessionID, record.appID, record.pid); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func validWindowDiscovery(status string, failure *Failure, windowCount int) bool {
	switch status {
	case WindowStatusReady:
		return windowCount > 0 && failure == nil
	case WindowStatusNone, WindowStatusPermissionRequired:
		return windowCount == 0 && failure == nil
	case WindowStatusFailed:
		return windowCount == 0 && failure != nil && strings.TrimSpace(failure.Code) != "" && strings.TrimSpace(failure.Message) != ""
	default:
		return false
	}
}

func (m *Manager) acquireWrite(ctx context.Context, operation string) error {
	select {
	case m.write <- struct{}{}:
		return nil
	case <-ctx.Done():
		return &OperationError{Code: "computer_action_cancelled", Message: "Computer Use " + operation + " was cancelled", Outcome: "not_started", Cause: ctx.Err()}
	}
}

func (m *Manager) releaseWrite() {
	<-m.write
}

func (m *Manager) findLaunch(sessionID, appID string, pid int32) (launchRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, record := range m.launches[sessionID] {
		if record.appID == appID && record.pid == pid {
			return record, true
		}
	}
	return launchRecord{}, false
}

func (m *Manager) getLaunch(sessionID, launchID string) (launchRecord, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.launches[sessionID][launchID]
	return record, ok
}

func (m *Manager) deleteLaunch(sessionID, launchID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.launches[sessionID], launchID)
	if len(m.launches[sessionID]) == 0 {
		delete(m.launches, sessionID)
	}
}

func (m *Manager) storeObservation(sessionID, appID string, windowID uint32, maxElements int, snapshot Observation, capture *Capture) (ManagedObservation, error) {
	if snapshot.AppID != appID || snapshot.WindowID == nil || *snapshot.WindowID != windowID {
		return ManagedObservation{}, &OperationError{Code: "computer_invalid_response", Message: "observation target does not match the request", Outcome: "unknown"}
	}
	actions := make(map[string]map[string]bool, len(snapshot.Elements))
	for index := range snapshot.Elements {
		element := &snapshot.Elements[index]
		if element.ElementID == "" || element.WindowID == nil || *element.WindowID != windowID {
			return ManagedObservation{}, &OperationError{Code: "computer_invalid_response", Message: "observation contains an invalid element target", Outcome: "unknown"}
		}
		if _, exists := actions[element.ElementID]; exists {
			return ManagedObservation{}, &OperationError{Code: "computer_invalid_response", Message: "observation contains duplicate element IDs", Outcome: "unknown"}
		}
		allowed := map[string]bool{}
		if element.Secure {
			element.Actions = nil
		} else {
			for _, action := range element.Actions {
				if validAction(action) {
					allowed[action] = true
				}
			}
		}
		actions[element.ElementID] = allowed
	}
	id, err := newObservationID()
	if err != nil {
		return ManagedObservation{}, err
	}
	now := m.now()
	ttl := observationTTL
	if capture != nil {
		ttl = captureObservationTTL
	}
	expiresAt := now.Add(ttl)
	managed := ManagedObservation{ObservationID: id, ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano), Snapshot: snapshot}
	var storedCapture *Capture
	if capture != nil {
		copied := *capture
		storedCapture = &copied
	}
	record := observationRecord{managed: managed, sessionID: sessionID, appID: appID, windowID: windowID, maxElements: maxElements, actions: actions, capture: storedCapture, expiresAt: expiresAt}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleanupLocked(now)
	items := m.items[sessionID]
	if items == nil {
		items = map[string]observationRecord{}
		m.items[sessionID] = items
	}
	if len(items) >= maxObservationsPerSession {
		return ManagedObservation{}, &OperationError{Code: "computer_observation_limit", Message: "too many live Computer Use observations in this session", Outcome: "not_started"}
	}
	items[id] = record
	return managed, nil
}

func validAction(action string) bool {
	switch action {
	case ActionPress, ActionSetValue, ActionSelect, ActionSubmit:
		return true
	default:
		return false
	}
}

func (m *Manager) takeObservation(sessionID, observationID string) (observationRecord, error) {
	now := m.now()
	m.mu.Lock()
	defer m.mu.Unlock()
	items := m.items[sessionID]
	record, ok := items[observationID]
	if ok {
		delete(items, observationID)
		if len(items) == 0 {
			delete(m.items, sessionID)
		}
	}
	if !ok {
		return observationRecord{}, &OperationError{Code: "computer_observation_not_found", Message: "observation was not found or was already consumed", Outcome: "not_started"}
	}
	if !now.Before(record.expiresAt) {
		return observationRecord{}, &OperationError{Code: "computer_observation_stale", Message: "observation has expired", Outcome: "not_started"}
	}
	return record, nil
}

func (m *Manager) cleanupLocked(now time.Time) {
	for sessionID, items := range m.items {
		for id, record := range items {
			if !now.Before(record.expiresAt) {
				delete(items, id)
			}
		}
		if len(items) == 0 {
			delete(m.items, sessionID)
		}
	}
}

func validateSessionID(sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return invalid("sessionID is required")
	}
	return nil
}

func validateTarget(sessionID, appID string, windowID uint32) error {
	if err := validateApp(sessionID, appID); err != nil {
		return err
	}
	if windowID == 0 {
		return invalid("windowID is required")
	}
	return nil
}

func validateApp(sessionID, appID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	if strings.TrimSpace(appID) == "" {
		return invalid("appID is required")
	}
	if len(appID) > MaxAppIDBytes {
		return invalid("appID is too long")
	}
	return nil
}

func newObservationID() (string, error) {
	return newRandomID("obs_")
}

func newLaunchID() (string, error) {
	return newRandomID("launch_")
}

func newRandomID(prefix string) (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("create Computer Use id: %w", err)
	}
	return prefix + hex.EncodeToString(raw), nil
}

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
	"unicode/utf8"
)

type launchRecord struct {
	launchID string
	appID    string
	name     string
	pid      int32
}

type Manager struct {
	service  Service
	write    chan struct{}
	mu       sync.Mutex
	launches map[string]map[string]launchRecord
}

func NewManager(service Service) *Manager {
	return &Manager{
		service:  service,
		write:    make(chan struct{}, 1),
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

func (m *Manager) Observe(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int) (Observation, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return Observation{}, err
	}
	if maxElements == 0 {
		maxElements = 200
	}
	if maxElements < 1 || maxElements > 1_000 {
		return Observation{}, invalid("maxElements must be between 1 and 1000")
	}
	snapshot, err := m.service.Observe(ctx, sessionID, appID, windowID, maxElements)
	if err != nil {
		return Observation{}, err
	}
	return validateObservation(appID, windowID, snapshot)
}

func (m *Manager) ObserveCapture(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int, output string) (NativeObservationCapture, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return NativeObservationCapture{}, err
	}
	if maxElements == 0 {
		maxElements = 200
	}
	if maxElements < 1 || maxElements > 1_000 {
		return NativeObservationCapture{}, invalid("maxElements must be between 1 and 1000")
	}
	if strings.TrimSpace(output) == "" {
		return NativeObservationCapture{}, invalid("output is required")
	}
	native, err := m.service.ObserveCapture(ctx, sessionID, appID, windowID, maxElements, output)
	if err != nil {
		return NativeObservationCapture{}, err
	}
	if native.Capture == nil {
		return NativeObservationCapture{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use observe-capture returned an invalid capture result", Outcome: "unknown"}
	}
	if native.Capture != nil && !validCapture(*native.Capture, windowID, output) {
		return NativeObservationCapture{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use capture returned an invalid result", Outcome: "unknown"}
	}
	validated, err := validateObservation(appID, windowID, native.Observation)
	if err != nil {
		return NativeObservationCapture{}, err
	}
	return NativeObservationCapture{Observation: validated, Capture: native.Capture}, nil
}

func validCapture(captured Capture, windowID uint32, output string) bool {
	return captured.WindowID == windowID && filepath.Clean(captured.Output) == filepath.Clean(output) && captured.Width > 0 && captured.Height > 0 && captured.ScaleFactor > 0
}

func (m *Manager) Act(ctx context.Context, sessionID, appID string, windowID uint32, elementID, action string, value *string) (ActionResult, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ActionResult{}, err
	}
	semantic, err := validateSemanticAction(SemanticAction{ElementID: elementID, Action: action, Value: value})
	if err != nil {
		return ActionResult{}, err
	}

	if err := m.acquireWrite(ctx, "action"); err != nil {
		return ActionResult{}, err
	}
	defer m.releaseWrite()

	native, err := m.service.Act(ctx, sessionID, appID, windowID, semantic.ElementID, semantic.Action, semantic.Value)
	if err != nil {
		return ActionResult{}, err
	}
	if !matchesSemanticResult(native, appID, semantic) {
		return ActionResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use action returned an invalid result", Outcome: "unknown"}
	}
	return ActionResult{Action: native}, nil
}

func (m *Manager) ActSequence(ctx context.Context, sessionID, appID string, windowID uint32, actions []SemanticAction) (ActionSequenceResult, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ActionSequenceResult{}, err
	}
	if len(actions) < 2 || len(actions) > MaxActionSequenceSteps {
		return ActionSequenceResult{}, invalid("action_sequence requires between 2 and 32 actions")
	}
	validated := make([]SemanticAction, len(actions))
	for index, action := range actions {
		resolved, err := validateSemanticAction(action)
		if err != nil {
			return ActionSequenceResult{}, invalid(fmt.Sprintf("actions[%d]: %s", index, err.Error()))
		}
		validated[index] = resolved
	}

	if err := m.acquireWrite(ctx, "action sequence"); err != nil {
		return ActionSequenceResult{}, err
	}
	defer m.releaseWrite()

	result := ActionSequenceResult{Actions: make([]NativeAction, 0, len(validated))}
	for index, action := range validated {
		native, actionErr := m.service.Act(ctx, sessionID, appID, windowID, action.ElementID, action.Action, action.Value)
		if actionErr != nil {
			failure := ErrorFailure(actionErr)
			if result.CompletedCount > 0 && failure.Outcome == "not_started" {
				failure.Outcome = "completed"
				failure.Retryable = false
			}
			result.FailedIndex = &index
			result.Failure = &failure
			return result, nil
		}
		if !matchesSemanticResult(native, appID, action) {
			failure := ErrorFailure(&OperationError{Code: "computer_invalid_response", Message: "Computer Use action sequence returned an invalid result", Outcome: "unknown"})
			result.FailedIndex = &index
			result.Failure = &failure
			return result, nil
		}
		result.Actions = append(result.Actions, native)
		result.CompletedCount++
	}
	return result, nil
}

func validateSemanticAction(action SemanticAction) (SemanticAction, error) {
	action.ElementID = strings.TrimSpace(action.ElementID)
	action.Action = strings.TrimSpace(action.Action)
	if action.ElementID == "" {
		return action, invalid("elementID is required")
	}
	if !validAction(action.Action) {
		return action, invalid("action must be press, set_value, select, or submit")
	}
	if action.Action == ActionSetValue && action.Value == nil {
		return action, invalid("value is required for set_value")
	}
	if action.Action != ActionSetValue && action.Value != nil {
		return action, invalid("value is allowed only for set_value")
	}
	if action.Value != nil && utf8.RuneCountInString(*action.Value) > MaxActionValueCharacters {
		return action, invalid("value is too long")
	}
	return action, nil
}

func matchesSemanticResult(native NativeAction, appID string, action SemanticAction) bool {
	return native.Completed && native.AppID == appID && native.ElementID == action.ElementID && native.Action == action.Action
}

func (m *Manager) Pointer(ctx context.Context, sessionID, appID string, windowID uint32, pointer PointerInput) (ActionResult, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ActionResult{}, err
	}
	if err := validatePointerInput(pointer); err != nil {
		return ActionResult{}, err
	}
	if err := m.acquireWrite(ctx, "action"); err != nil {
		return ActionResult{}, err
	}
	defer m.releaseWrite()

	native, err := m.service.Pointer(ctx, sessionID, appID, windowID, pointer)
	if err != nil {
		return ActionResult{}, err
	}
	if !matchesPointerResult(native, appID, pointer) {
		return ActionResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use pointer action returned an invalid result", Outcome: "unknown"}
	}
	return ActionResult{Action: native}, nil
}

func validatePointerInput(pointer PointerInput) error {
	if !normalizedCoordinate(pointer.X) || !normalizedCoordinate(pointer.Y) {
		return invalid("x and y must be normalized window coordinates between 0 inclusive and 1 exclusive")
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
		if pointer.ToX == nil || pointer.ToY == nil || !normalizedCoordinate(*pointer.ToX) || !normalizedCoordinate(*pointer.ToY) {
			return invalid("drag requires normalized toX and toY between 0 inclusive and 1 exclusive")
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

func normalizedCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value < 1
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
	case WindowStatusNone:
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

func validateObservation(appID string, windowID uint32, snapshot Observation) (Observation, error) {
	if snapshot.AppID != appID || snapshot.WindowID == nil || *snapshot.WindowID != windowID {
		return Observation{}, &OperationError{Code: "computer_invalid_response", Message: "observation target does not match the request", Outcome: "unknown"}
	}
	counts := make(map[string]int, len(snapshot.Elements))
	for index := range snapshot.Elements {
		element := &snapshot.Elements[index]
		if element.ElementID == "" || element.WindowID == nil || *element.WindowID != windowID {
			return Observation{}, &OperationError{Code: "computer_invalid_response", Message: "observation contains an invalid element target", Outcome: "unknown"}
		}
		counts[element.ElementID]++
	}
	for index := range snapshot.Elements {
		element := &snapshot.Elements[index]
		if element.Secure || counts[element.ElementID] > 1 {
			element.Actions = nil
		} else {
			valid := element.Actions[:0]
			for _, action := range element.Actions {
				if validAction(action) {
					valid = append(valid, action)
				}
			}
			element.Actions = valid
		}
	}
	return snapshot, nil
}

func validAction(action string) bool {
	switch action {
	case ActionPress, ActionSetValue, ActionSelect, ActionSubmit:
		return true
	default:
		return false
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

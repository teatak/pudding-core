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

func (m *Manager) Act(ctx context.Context, sessionID, appID string, windowID uint32, actions []ActionInput) (ActionsResult, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return ActionsResult{}, err
	}
	validated, err := NormalizeActions(actions)
	if err != nil {
		return ActionsResult{}, err
	}

	if err := m.acquireWrite(ctx, "actions"); err != nil {
		return ActionsResult{}, err
	}
	defer m.releaseWrite()

	result := ActionsResult{Actions: make([]NativeAction, 0, len(validated))}
	for index, action := range validated {
		var native NativeAction
		var actionErr error
		if isSemanticAction(action.Type) {
			native, actionErr = m.service.Act(ctx, sessionID, appID, windowID, action.ElementID, action.Type, action.Value)
		} else {
			native, actionErr = m.service.Pointer(ctx, sessionID, appID, windowID, pointerInput(action))
		}
		if actionErr != nil {
			failure := ErrorFailure(actionErr)
			result.FailedIndex = &index
			result.Failure = &failure
			return result, nil
		}
		if !matchesActionResult(native, appID, action) {
			failure := ErrorFailure(&OperationError{Code: "computer_invalid_response", Message: "Computer Use actions returned an invalid result", Outcome: "unknown"})
			result.FailedIndex = &index
			result.Failure = &failure
			return result, nil
		}
		result.Actions = append(result.Actions, native)
		result.CompletedCount++
	}
	return result, nil
}

func NormalizeActions(actions []ActionInput) ([]ActionInput, error) {
	if len(actions) < 1 || len(actions) > MaxActionsPerCall {
		return nil, invalid("actions must contain between 1 and 32 items")
	}
	validated := make([]ActionInput, len(actions))
	for index, action := range actions {
		resolved, err := normalizeAction(action)
		if err != nil {
			return nil, invalid(fmt.Sprintf("actions[%d]: %s", index, err.Error()))
		}
		validated[index] = resolved
	}
	return validated, nil
}

func normalizeAction(action ActionInput) (ActionInput, error) {
	action.Type = strings.TrimSpace(action.Type)
	action.ElementID = strings.TrimSpace(action.ElementID)
	action.Button = strings.TrimSpace(action.Button)
	if isSemanticAction(action.Type) {
		if action.ElementID == "" {
			return action, invalid("elementID is required for semantic actions")
		}
		if hasPointerFields(action) {
			return action, invalid("pointer fields are allowed only for click, drag, and scroll")
		}
		if action.Type == ActionSetValue && action.Value == nil {
			return action, invalid("value is required for set_value")
		}
		if action.Type != ActionSetValue && action.Value != nil {
			return action, invalid("value is allowed only for set_value")
		}
		if action.Value != nil && utf8.RuneCountInString(*action.Value) > MaxActionValueCharacters {
			return action, invalid("value is too long")
		}
		return action, nil
	}
	if action.Type != ActionClick && action.Type != ActionDrag && action.Type != ActionScroll {
		return action, invalid("type must be press, set_value, select, submit, click, drag, or scroll")
	}
	if action.ElementID != "" || action.Value != nil {
		return action, invalid("elementID and value must be omitted for pointer actions")
	}
	if action.X == nil || action.Y == nil || !normalizedCoordinate(*action.X) || !normalizedCoordinate(*action.Y) {
		return action, invalid("x and y must be normalized window coordinates between 0 inclusive and 1 exclusive")
	}
	switch action.Type {
	case ActionClick:
		if action.ToX != nil || action.ToY != nil || action.DeltaX != nil || action.DeltaY != nil {
			return action, invalid("click accepts only x, y, button, and clickCount")
		}
		if action.Button == "" {
			action.Button = PointerButtonLeft
		}
		if action.Button != PointerButtonLeft && action.Button != PointerButtonRight {
			return action, invalid("click button must be left or right")
		}
		if action.ClickCount == nil {
			count := 1
			action.ClickCount = &count
		}
		if *action.ClickCount != 1 && !(action.Button == PointerButtonLeft && *action.ClickCount == 2) {
			return action, invalid("clickCount must be 1, or 2 for the left button")
		}
	case ActionDrag:
		if action.ToX == nil || action.ToY == nil || !normalizedCoordinate(*action.ToX) || !normalizedCoordinate(*action.ToY) {
			return action, invalid("drag requires normalized toX and toY between 0 inclusive and 1 exclusive")
		}
		if action.Button != "" || action.ClickCount != nil || action.DeltaX != nil || action.DeltaY != nil {
			return action, invalid("drag accepts only start and end coordinates")
		}
	case ActionScroll:
		if action.ToX != nil || action.ToY != nil || action.Button != "" || action.ClickCount != nil {
			return action, invalid("scroll accepts only x, y, deltaX, and deltaY")
		}
		if action.DeltaX == nil {
			zero := 0
			action.DeltaX = &zero
		}
		if action.DeltaY == nil {
			zero := 0
			action.DeltaY = &zero
		}
		if (*action.DeltaX == 0 && *action.DeltaY == 0) || *action.DeltaX < -5_000 || *action.DeltaX > 5_000 || *action.DeltaY < -5_000 || *action.DeltaY > 5_000 {
			return action, invalid("scroll deltas must be non-zero in total and between -5000 and 5000")
		}
	}
	return action, nil
}

func isSemanticAction(actionType string) bool {
	return actionType == ActionPress || actionType == ActionSetValue || actionType == ActionSelect || actionType == ActionSubmit
}

func hasPointerFields(action ActionInput) bool {
	return action.X != nil || action.Y != nil || action.ToX != nil || action.ToY != nil || action.Button != "" || action.ClickCount != nil || action.DeltaX != nil || action.DeltaY != nil
}

func pointerInput(action ActionInput) PointerInput {
	pointer := PointerInput{
		Action: action.Type, X: *action.X, Y: *action.Y, ToX: action.ToX, ToY: action.ToY,
		Button: action.Button, DeltaX: action.DeltaX, DeltaY: action.DeltaY,
	}
	if action.ClickCount != nil {
		pointer.ClickCount = *action.ClickCount
	}
	return pointer
}

func matchesActionResult(native NativeAction, appID string, action ActionInput) bool {
	if isSemanticAction(action.Type) {
		return native.Completed && native.AppID == appID && native.ElementID == action.ElementID && native.Action == action.Type
	}
	return matchesPointerResult(native, appID, pointerInput(action))
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

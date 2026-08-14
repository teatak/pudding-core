package computer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	observationTTL            = 30 * time.Second
	maxObservationsPerSession = 16
)

type observationRecord struct {
	managed     ManagedObservation
	sessionID   string
	appID       string
	windowID    uint32
	maxElements int
	actions     map[string]map[string]bool
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
	return m.service.ListApps(ctx, sessionID)
}

func (m *Manager) UseApp(ctx context.Context, sessionID, appID string) (UseResult, error) {
	if err := validateApp(sessionID, appID); err != nil {
		return UseResult{}, err
	}
	if err := m.acquireWrite(ctx, "app use"); err != nil {
		return UseResult{}, err
	}
	defer m.releaseWrite()

	native, err := m.service.UseApp(ctx, sessionID, appID)
	if err != nil {
		return UseResult{}, err
	}
	if native.AppID != appID || native.PID <= 0 || strings.TrimSpace(native.Name) == "" {
		return UseResult{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use app entry returned an invalid result", Outcome: "unknown"}
	}
	if !native.NewlyLaunched {
		if record, ok := m.findLaunch(sessionID, native.AppID, native.PID); ok {
			existingLaunchID := record.launchID
			return UseResult{LaunchID: &existingLaunchID, AppID: record.appID, Name: record.name, PID: record.pid}, nil
		}
		return UseResult{AppID: native.AppID, Name: native.Name, PID: native.PID}, nil
	}
	launchID, err := newLaunchID()
	if err != nil {
		return UseResult{}, err
	}
	record := launchRecord{launchID: launchID, appID: native.AppID, name: native.Name, pid: native.PID}
	m.mu.Lock()
	launches := m.launches[sessionID]
	if launches == nil {
		launches = map[string]launchRecord{}
		m.launches[sessionID] = launches
	}
	launches[launchID] = record
	m.mu.Unlock()
	return UseResult{LaunchID: &launchID, AppID: native.AppID, Name: native.Name, PID: native.PID}, nil
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
	return m.storeObservation(sessionID, appID, windowID, maxElements, snapshot)
}

func (m *Manager) Capture(ctx context.Context, sessionID, appID string, windowID uint32, output string) (Capture, error) {
	if err := validateTarget(sessionID, appID, windowID); err != nil {
		return Capture{}, err
	}
	if strings.TrimSpace(output) == "" {
		return Capture{}, invalid("output is required")
	}
	captured, err := m.service.Capture(ctx, sessionID, appID, windowID, output)
	if err != nil {
		return Capture{}, err
	}
	if captured.WindowID != windowID || filepath.Clean(captured.Output) != filepath.Clean(output) || captured.Width < 1 || captured.Height < 1 {
		return Capture{}, &OperationError{Code: "computer_invalid_response", Message: "Computer Use capture returned an invalid result", Outcome: "unknown"}
	}
	return captured, nil
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
	if action != ActionPress && action != ActionSetValue {
		return ActionResult{}, invalid("action must be press or set_value")
	}
	if action == ActionSetValue && value == nil {
		return ActionResult{}, invalid("value is required for set_value")
	}
	if action == ActionPress && value != nil {
		return ActionResult{}, invalid("value is not allowed for press")
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
	result := ActionResult{Action: native}
	latest, observeErr := m.service.Observe(ctx, sessionID, appID, windowID, record.maxElements)
	if observeErr != nil {
		failure := ErrorFailure(observeErr)
		failure.Outcome = "completed"
		failure.Retryable = false
		result.ObservationError = &failure
		return result, nil
	}
	managed, storeErr := m.storeObservation(sessionID, appID, windowID, record.maxElements, latest)
	if storeErr != nil {
		failure := ErrorFailure(storeErr)
		failure.Outcome = "completed"
		failure.Retryable = false
		result.ObservationError = &failure
		return result, nil
	}
	result.Observation = &managed
	return result, nil
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
	if err := m.service.ReleaseSession(ctx, sessionID); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
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

func (m *Manager) storeObservation(sessionID, appID string, windowID uint32, maxElements int, snapshot Observation) (ManagedObservation, error) {
	if snapshot.AppID != appID || snapshot.WindowID == nil || *snapshot.WindowID != windowID {
		return ManagedObservation{}, &OperationError{Code: "computer_invalid_response", Message: "observation target does not match the request", Outcome: "unknown"}
	}
	actions := make(map[string]map[string]bool, len(snapshot.Elements))
	for _, element := range snapshot.Elements {
		if element.ElementID == "" || element.WindowID == nil || *element.WindowID != windowID {
			return ManagedObservation{}, &OperationError{Code: "computer_invalid_response", Message: "observation contains an invalid element target", Outcome: "unknown"}
		}
		if _, exists := actions[element.ElementID]; exists {
			return ManagedObservation{}, &OperationError{Code: "computer_invalid_response", Message: "observation contains duplicate element IDs", Outcome: "unknown"}
		}
		allowed := map[string]bool{}
		for _, action := range element.Actions {
			if action == ActionPress || action == ActionSetValue {
				allowed[action] = true
			}
		}
		if element.Secure {
			delete(allowed, ActionSetValue)
		}
		actions[element.ElementID] = allowed
	}
	id, err := newObservationID()
	if err != nil {
		return ManagedObservation{}, err
	}
	now := m.now()
	expiresAt := now.Add(observationTTL)
	managed := ManagedObservation{ObservationID: id, ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano), Snapshot: snapshot}
	record := observationRecord{managed: managed, sessionID: sessionID, appID: appID, windowID: windowID, maxElements: maxElements, actions: actions, expiresAt: expiresAt}
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

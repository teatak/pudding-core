package computer

import (
	"context"
	"errors"
)

const (
	ActionPress    = "press"
	ActionSetValue = "set_value"
	ActionSelect   = "select"
	ActionSubmit   = "submit"
	ActionClick    = "click"
	ActionDrag     = "drag"
	ActionScroll   = "scroll"

	PointerButtonLeft  = "left"
	PointerButtonRight = "right"

	MaxAppIDBytes            = 512
	MaxActionValueCharacters = 20_000

	WindowStatusReady  = "ready"
	WindowStatusNone   = "none"
	WindowStatusFailed = "failed"
)

type Permissions struct {
	Accessibility   bool `json:"accessibility"`
	ScreenRecording bool `json:"screenRecording"`
}

type Frame struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type Window struct {
	Index    int     `json:"index"`
	WindowID *uint32 `json:"windowID,omitempty"`
	Title    *string `json:"title,omitempty"`
	Frame    *Frame  `json:"frame,omitempty"`
}

type Application struct {
	AppID        string `json:"appID"`
	Name         string `json:"name"`
	Running      bool   `json:"running"`
	Active       bool   `json:"active"`
	Controllable bool   `json:"controllable"`
}

type CapturableWindow struct {
	WindowID        uint32  `json:"windowID"`
	PID             int32   `json:"pid"`
	AppID           *string `json:"appID,omitempty"`
	ApplicationName *string `json:"applicationName,omitempty"`
	Title           *string `json:"title,omitempty"`
	Frame           Frame   `json:"frame"`
}

type AppList struct {
	Apps []Application `json:"apps"`
}

type NativeUse struct {
	AppID         string             `json:"appID"`
	Name          string             `json:"name"`
	PID           int32              `json:"pid"`
	NewlyLaunched bool               `json:"newlyLaunched"`
	WindowStatus  string             `json:"windowStatus"`
	WindowError   *Failure           `json:"windowError,omitempty"`
	Windows       []CapturableWindow `json:"windows"`
}

type UseResult struct {
	LaunchID     *string            `json:"launchID,omitempty"`
	AppID        string             `json:"appID"`
	Name         string             `json:"name"`
	PID          int32              `json:"pid"`
	WindowStatus string             `json:"windowStatus"`
	WindowError  *Failure           `json:"windowError,omitempty"`
	Windows      []CapturableWindow `json:"windows"`
}

type NativeQuit struct {
	AppID  string `json:"appID"`
	PID    int32  `json:"pid"`
	Closed bool   `json:"closed"`
}

type QuitResult struct {
	LaunchID string `json:"launchID"`
	AppID    string `json:"appID"`
	Name     string `json:"name"`
	PID      int32  `json:"pid"`
	Closed   bool   `json:"closed"`
}

type Element struct {
	ElementID      string   `json:"elementID"`
	WindowIndex    int      `json:"windowIndex"`
	WindowID       *uint32  `json:"windowID,omitempty"`
	Role           *string  `json:"role,omitempty"`
	Subrole        *string  `json:"subrole,omitempty"`
	Label          *string  `json:"label,omitempty"`
	Description    *string  `json:"description,omitempty"`
	Value          *string  `json:"value,omitempty"`
	ValueTruncated bool     `json:"valueTruncated"`
	Secure         bool     `json:"secure"`
	Enabled        *bool    `json:"enabled,omitempty"`
	Focused        *bool    `json:"focused,omitempty"`
	Selected       *bool    `json:"selected,omitempty"`
	Frame          *Frame   `json:"frame,omitempty"`
	Actions        []string `json:"actions"`
}

type Observation struct {
	AppID      string    `json:"appID"`
	WindowID   *uint32   `json:"windowID,omitempty"`
	Name       string    `json:"name"`
	PID        int32     `json:"pid"`
	ObservedAt string    `json:"observedAt"`
	Truncated  bool      `json:"truncated"`
	Windows    []Window  `json:"windows"`
	Elements   []Element `json:"elements"`
}

type ManagedObservation struct {
	ObservationID string      `json:"observationID"`
	ExpiresAt     string      `json:"expiresAt"`
	Snapshot      Observation `json:"snapshot"`
}

type Capture struct {
	WindowID    uint32  `json:"windowID"`
	Output      string  `json:"output"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	ScaleFactor float64 `json:"scaleFactor"`
}

type NativeObservationCapture struct {
	Observation Observation `json:"observation"`
	Capture     *Capture    `json:"capture,omitempty"`
}

type ManagedObservationCapture struct {
	Observation ManagedObservation `json:"observation"`
	Capture     *Capture           `json:"capture,omitempty"`
}

type NativeAction struct {
	AppID      string   `json:"appID"`
	ElementID  string   `json:"elementID"`
	Action     string   `json:"action"`
	Completed  bool     `json:"completed"`
	X          *float64 `json:"x,omitempty"`
	Y          *float64 `json:"y,omitempty"`
	ToX        *float64 `json:"toX,omitempty"`
	ToY        *float64 `json:"toY,omitempty"`
	Button     string   `json:"button,omitempty"`
	ClickCount int      `json:"clickCount,omitempty"`
	DeltaX     *int     `json:"deltaX,omitempty"`
	DeltaY     *int     `json:"deltaY,omitempty"`
}

type PointerInput struct {
	Action     string
	X          float64
	Y          float64
	ToX        *float64
	ToY        *float64
	Button     string
	ClickCount int
	DeltaX     *int
	DeltaY     *int
}

type ScreenshotPointer struct {
	PointerInput
	CaptureWidth  int
	CaptureHeight int
	ScaleFactor   float64
}

type Failure struct {
	Code        string   `json:"code"`
	Message     string   `json:"message"`
	Permission  string   `json:"permission,omitempty"`
	Permissions []string `json:"permissions,omitempty"`
	Retryable   bool     `json:"retryable"`
	Outcome     string   `json:"outcome"`
}

type ActionResult struct {
	Action           NativeAction        `json:"action"`
	Observation      *ManagedObservation `json:"observation,omitempty"`
	ObservationError *Failure            `json:"observationError,omitempty"`
}

type Service interface {
	Permissions(ctx context.Context) (Permissions, error)
	ListApps(ctx context.Context, sessionID string) (AppList, error)
	UseApp(ctx context.Context, sessionID, appID string, foreground bool) (NativeUse, error)
	QuitApp(ctx context.Context, sessionID, appID string, pid int32) (NativeQuit, error)
	Observe(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int) (Observation, error)
	ObserveCapture(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int, output string) (NativeObservationCapture, error)
	Act(ctx context.Context, sessionID, appID string, windowID uint32, elementID, action string, value *string) (NativeAction, error)
	Pointer(ctx context.Context, sessionID, appID string, windowID uint32, pointer ScreenshotPointer) (NativeAction, error)
}

type Controller interface {
	ListApps(ctx context.Context, sessionID string) (AppList, error)
	UseApp(ctx context.Context, sessionID, appID string, foreground bool) (UseResult, error)
	OwnedLaunchAppID(sessionID, launchID string) (string, bool)
	QuitApp(ctx context.Context, sessionID, launchID string) (QuitResult, error)
	Observe(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int) (ManagedObservation, error)
	ObserveCapture(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int, output string) (ManagedObservationCapture, error)
	Act(ctx context.Context, sessionID, appID string, windowID uint32, observationID, elementID, action string, value *string) (ActionResult, error)
	Pointer(ctx context.Context, sessionID, appID string, windowID uint32, observationID string, pointer PointerInput) (ActionResult, error)
	ReleaseSession(ctx context.Context, sessionID string) error
}

type OperationError struct {
	Code        string
	Message     string
	Permission  string
	Permissions []string
	Retryable   bool
	Outcome     string
	Cause       error
}

func (e *OperationError) Error() string { return e.Message }
func (e *OperationError) Unwrap() error { return e.Cause }

func ErrorFailure(err error) Failure {
	var operationErr *OperationError
	if errors.As(err, &operationErr) {
		return Failure{Code: operationErr.Code, Message: operationErr.Message, Permission: operationErr.Permission, Permissions: operationErr.Permissions, Retryable: operationErr.Retryable, Outcome: validOutcome(operationErr.Outcome)}
	}
	return Failure{Code: "computer_unavailable", Message: err.Error(), Outcome: "unknown"}
}

func invalid(message string) error {
	return &OperationError{Code: "computer_invalid_request", Message: message, Outcome: "not_started"}
}

func validOutcome(outcome string) string {
	switch outcome {
	case "not_started", "completed", "unknown":
		return outcome
	default:
		return "unknown"
	}
}

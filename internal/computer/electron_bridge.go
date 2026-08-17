package computer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const maximumBridgeResponseBytes = 4 << 20

type ElectronBridgeConfig struct {
	URL        string
	Token      string
	HTTPClient *http.Client
}

type ElectronBridgeService struct {
	endpoint string
	token    string
	client   *http.Client
}

type bridgeApplication struct {
	BundleID     string `json:"bundleID"`
	Name         string `json:"name"`
	Running      bool   `json:"running"`
	Active       bool   `json:"active"`
	Controllable bool   `json:"controllable"`
}

type bridgeCapturableWindow struct {
	WindowID        uint32  `json:"windowID"`
	PID             int32   `json:"pid"`
	BundleID        *string `json:"bundleID,omitempty"`
	ApplicationName *string `json:"applicationName,omitempty"`
	Title           *string `json:"title,omitempty"`
	Frame           Frame   `json:"frame"`
}

type bridgeAppList struct {
	Apps []bridgeApplication `json:"apps"`
}

type bridgeObservation struct {
	BundleID   string    `json:"bundleID"`
	WindowID   *uint32   `json:"windowID,omitempty"`
	Name       string    `json:"name"`
	PID        int32     `json:"pid"`
	ObservedAt string    `json:"observedAt"`
	Truncated  bool      `json:"truncated"`
	Windows    []Window  `json:"windows"`
	Elements   []Element `json:"elements"`
}

type bridgeObservationCapture struct {
	Observation bridgeObservation `json:"observation"`
	Capture     *Capture          `json:"capture,omitempty"`
}

type bridgeNativeAction struct {
	BundleID   string   `json:"bundleID"`
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

type bridgeNativeUse struct {
	BundleID      string                   `json:"bundleID"`
	Name          string                   `json:"name"`
	PID           int32                    `json:"pid"`
	NewlyLaunched bool                     `json:"newlyLaunched"`
	WindowStatus  string                   `json:"windowStatus"`
	WindowError   *bridgeFailure           `json:"windowError,omitempty"`
	Windows       []bridgeCapturableWindow `json:"windows"`
}

type bridgeNativeQuit struct {
	BundleID string `json:"bundleID"`
	PID      int32  `json:"pid"`
	Closed   bool   `json:"closed"`
}

type bridgeError struct {
	Error       string   `json:"error"`
	Code        string   `json:"code"`
	Permission  string   `json:"permission"`
	Permissions []string `json:"permissions"`
	Retryable   bool     `json:"retryable"`
	Outcome     string   `json:"outcome"`
}

type bridgeFailure struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Permission string `json:"permission"`
	Retryable  bool   `json:"retryable"`
	Outcome    string `json:"outcome"`
}

func NewElectronBridgeService(cfg ElectronBridgeConfig) (*ElectronBridgeService, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(cfg.URL), "/")
	token := strings.TrimSpace(cfg.Token)
	if endpoint == "" || token == "" {
		return nil, errors.New("electron Computer Use bridge url and token are required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "http" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("invalid electron Computer Use bridge url")
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return nil, errors.New("electron Computer Use bridge must use a loopback IP")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{}
	}
	return &ElectronBridgeService{endpoint: endpoint, token: token, client: client}, nil
}

func (s *ElectronBridgeService) Permissions(ctx context.Context) (Permissions, error) {
	var out Permissions
	err := s.request(ctx, http.MethodGet, "/computer/permissions", nil, &out, "not_started")
	return out, err
}

func (s *ElectronBridgeService) ListApps(ctx context.Context, sessionID string) (AppList, error) {
	var raw bridgeAppList
	if err := s.post(ctx, "/computer/apps/list", map[string]any{"sessionID": sessionID}, &raw); err != nil {
		return AppList{}, err
	}
	out := AppList{Apps: make([]Application, 0, len(raw.Apps))}
	for _, item := range raw.Apps {
		out.Apps = append(out.Apps, Application{AppID: item.BundleID, Name: item.Name, Running: item.Running, Active: item.Active, Controllable: item.Controllable})
	}
	return out, nil
}

func (s *ElectronBridgeService) UseApp(ctx context.Context, sessionID, appID string, foreground bool) (NativeUse, error) {
	var raw bridgeNativeUse
	if err := s.request(ctx, http.MethodPost, "/computer/apps/use", map[string]any{"sessionID": sessionID, "appID": appID, "foreground": foreground}, &raw, "unknown"); err != nil {
		return NativeUse{}, err
	}
	windows := make([]CapturableWindow, 0, len(raw.Windows))
	for _, item := range raw.Windows {
		windows = append(windows, CapturableWindow{WindowID: item.WindowID, PID: item.PID, AppID: item.BundleID, ApplicationName: item.ApplicationName, Title: item.Title, Frame: item.Frame})
	}
	var windowError *Failure
	if raw.WindowError != nil {
		failure := Failure{Code: raw.WindowError.Code, Message: raw.WindowError.Message, Permission: raw.WindowError.Permission, Retryable: raw.WindowError.Retryable, Outcome: validOutcome(raw.WindowError.Outcome)}
		windowError = &failure
	}
	return NativeUse{AppID: raw.BundleID, Name: raw.Name, PID: raw.PID, NewlyLaunched: raw.NewlyLaunched, WindowStatus: raw.WindowStatus, WindowError: windowError, Windows: windows}, nil
}

func (s *ElectronBridgeService) QuitApp(ctx context.Context, sessionID, appID string, pid int32) (NativeQuit, error) {
	var raw bridgeNativeQuit
	if err := s.request(ctx, http.MethodPost, "/computer/apps/quit", map[string]any{"sessionID": sessionID, "appID": appID, "pid": pid}, &raw, "unknown"); err != nil {
		return NativeQuit{}, err
	}
	return NativeQuit{AppID: raw.BundleID, PID: raw.PID, Closed: raw.Closed}, nil
}

func (s *ElectronBridgeService) Observe(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int) (Observation, error) {
	var raw bridgeObservation
	err := s.post(ctx, "/computer/observe", map[string]any{"sessionID": sessionID, "appID": appID, "windowID": windowID, "maxElements": maxElements}, &raw)
	if err != nil {
		return Observation{}, err
	}
	return Observation{AppID: raw.BundleID, WindowID: raw.WindowID, Name: raw.Name, PID: raw.PID, ObservedAt: raw.ObservedAt, Truncated: raw.Truncated, Windows: raw.Windows, Elements: raw.Elements}, nil
}

func (s *ElectronBridgeService) ObserveCapture(ctx context.Context, sessionID, appID string, windowID uint32, maxElements int, output string) (NativeObservationCapture, error) {
	var raw bridgeObservationCapture
	err := s.post(ctx, "/computer/observe-capture", map[string]any{
		"sessionID": sessionID, "appID": appID, "windowID": windowID,
		"maxElements": maxElements, "output": output,
	}, &raw)
	if err != nil {
		return NativeObservationCapture{}, err
	}
	observation := Observation{
		AppID: raw.Observation.BundleID, WindowID: raw.Observation.WindowID,
		Name: raw.Observation.Name, PID: raw.Observation.PID,
		ObservedAt: raw.Observation.ObservedAt, Truncated: raw.Observation.Truncated,
		Windows: raw.Observation.Windows, Elements: raw.Observation.Elements,
	}
	return NativeObservationCapture{Observation: observation, Capture: raw.Capture}, nil
}

func (s *ElectronBridgeService) Act(ctx context.Context, sessionID, appID string, windowID uint32, elementID, action string, value *string) (NativeAction, error) {
	body := map[string]any{"sessionID": sessionID, "appID": appID, "windowID": windowID, "elementID": elementID, "action": action}
	if value != nil {
		body["value"] = *value
	}
	var raw bridgeNativeAction
	if err := s.request(ctx, http.MethodPost, "/computer/act", body, &raw, "unknown"); err != nil {
		return NativeAction{}, err
	}
	return NativeAction{AppID: raw.BundleID, ElementID: raw.ElementID, Action: raw.Action, Completed: raw.Completed, X: raw.X, Y: raw.Y}, nil
}

func (s *ElectronBridgeService) Pointer(ctx context.Context, sessionID, appID string, windowID uint32, pointer ScreenshotPointer) (NativeAction, error) {
	body := map[string]any{
		"sessionID": sessionID, "appID": appID, "windowID": windowID,
		"action": pointer.Action, "x": pointer.X, "y": pointer.Y,
		"captureWidth": pointer.CaptureWidth, "captureHeight": pointer.CaptureHeight,
		"scaleFactor": pointer.ScaleFactor,
	}
	if pointer.ToX != nil {
		body["toX"], body["toY"] = *pointer.ToX, *pointer.ToY
	}
	if pointer.Button != "" {
		body["button"], body["clickCount"] = pointer.Button, pointer.ClickCount
	}
	if pointer.DeltaX != nil {
		body["deltaX"], body["deltaY"] = *pointer.DeltaX, *pointer.DeltaY
	}
	var raw bridgeNativeAction
	if err := s.request(ctx, http.MethodPost, "/computer/pointer", body, &raw, "unknown"); err != nil {
		return NativeAction{}, err
	}
	return NativeAction{
		AppID: raw.BundleID, ElementID: raw.ElementID, Action: raw.Action, Completed: raw.Completed,
		X: raw.X, Y: raw.Y, ToX: raw.ToX, ToY: raw.ToY, Button: raw.Button,
		ClickCount: raw.ClickCount, DeltaX: raw.DeltaX, DeltaY: raw.DeltaY,
	}, nil
}

func (s *ElectronBridgeService) post(ctx context.Context, path string, in, out any) error {
	return s.request(ctx, http.MethodPost, path, in, out, "not_started")
}

func (s *ElectronBridgeService) request(ctx context.Context, method, path string, in, out any, uncertainOutcome string) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.endpoint+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return &OperationError{Code: "computer_unavailable", Message: err.Error(), Retryable: uncertainOutcome == "not_started", Outcome: uncertainOutcome, Cause: err}
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maximumBridgeResponseBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maximumBridgeResponseBytes {
		return &OperationError{Code: "computer_invalid_response", Message: "Computer Use bridge response is too large", Outcome: uncertainOutcome}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure bridgeError
		if json.Unmarshal(data, &failure) != nil || strings.TrimSpace(failure.Code) == "" {
			return &OperationError{Code: "computer_unavailable", Message: fmt.Sprintf("Computer Use bridge returned HTTP %d", resp.StatusCode), Outcome: uncertainOutcome}
		}
		return &OperationError{Code: failure.Code, Message: failure.Error, Permission: failure.Permission, Permissions: failure.Permissions, Retryable: failure.Retryable, Outcome: validOutcome(failure.Outcome)}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &OperationError{Code: "computer_invalid_response", Message: "Computer Use bridge returned invalid JSON", Outcome: uncertainOutcome, Cause: err}
	}
	return nil
}

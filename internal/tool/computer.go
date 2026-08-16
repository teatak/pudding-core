package tool

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/computer"
	"github.com/teatak/pudding-core/internal/store"
)

type computerObserveArgs struct {
	AppID             string `json:"appID"`
	WindowID          uint32 `json:"windowID"`
	MaxElements       int    `json:"maxElements"`
	IncludeScreenshot bool   `json:"includeScreenshot"`
}

type computerUseAppArgs struct {
	AppID      string `json:"appID"`
	Foreground bool   `json:"foreground"`
}

type computerQuitAppArgs struct {
	LaunchID string `json:"launchID"`
}

type computerActArgs struct {
	AppID         string   `json:"appID"`
	WindowID      uint32   `json:"windowID"`
	ObservationID string   `json:"observationID"`
	ElementID     string   `json:"elementID"`
	Action        string   `json:"action"`
	Value         *string  `json:"value"`
	X             *float64 `json:"x"`
	Y             *float64 `json:"y"`
	ToX           *float64 `json:"toX"`
	ToY           *float64 `json:"toY"`
	Button        string   `json:"button"`
	ClickCount    *int     `json:"clickCount"`
	DeltaX        *int     `json:"deltaX"`
	DeltaY        *int     `json:"deltaY"`
}

func (r *BuiltinRunner) computerListApps(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if ready := r.computerReady(call, out); ready != nil {
		return *ready
	}
	apps, err := r.computer.ListApps(ctx, call.SessionID)
	if err != nil {
		return computerToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": apps})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(apps.Apps)
	return out
}

func (r *BuiltinRunner) computerUseApp(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if ready := r.computerReady(call, out); ready != nil {
		return *ready
	}
	args, err := decodeComputerUseAppArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	result, err := r.computer.UseApp(ctx, call.SessionID, args.AppID, args.Foreground)
	if err != nil {
		return computerToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 5
	if result.LaunchID != nil {
		out.SummaryCount++
	}
	if result.WindowError != nil {
		out.SummaryCount++
	}
	return out
}

func (r *BuiltinRunner) computerQuitApp(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if ready := r.computerReady(call, out); ready != nil {
		return *ready
	}
	args, err := decodeComputerQuitAppArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	result, err := r.computer.QuitApp(ctx, call.SessionID, args.LaunchID)
	if err != nil {
		return computerToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 5
	return out
}

func (r *BuiltinRunner) computerObserve(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if ready := r.computerReady(call, out); ready != nil {
		return *ready
	}
	args, err := decodeComputerObserveArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if !args.IncludeScreenshot {
		observed, err := r.computer.Observe(ctx, call.SessionID, args.AppID, args.WindowID, args.MaxElements)
		if err != nil {
			return computerToolError(out, err)
		}
		payload := map[string]any{"ok": true, "observation": observed}
		out.Ok = true
		out.Content = jsonString(payload)
		out.SummaryKind = SummaryReturnedItems
		out.SummaryCount = len(observed.Snapshot.Elements)
		return out
	}

	tempDir, err := os.MkdirTemp("", "pudding-computer-")
	if err != nil {
		observed, observeErr := r.computer.Observe(ctx, call.SessionID, args.AppID, args.WindowID, args.MaxElements)
		if observeErr != nil {
			return computerToolError(out, observeErr)
		}
		payload := map[string]any{"ok": true, "observation": observed}
		payload["screenshotError"] = computer.ErrorFailure(err)
		out.Ok = true
		out.Content = jsonString(payload)
		out.SummaryKind = SummaryReturnedItems
		out.SummaryCount = len(observed.Snapshot.Elements)
		return out
	}
	defer os.RemoveAll(tempDir)
	output := filepath.Join(tempDir, fmt.Sprintf("computer-window-%d.png", args.WindowID))
	combined, err := r.computer.ObserveCapture(ctx, call.SessionID, args.AppID, args.WindowID, args.MaxElements, output)
	if err != nil {
		return computerToolError(out, err)
	}
	payload := map[string]any{"ok": true, "observation": combined.Observation}
	if combined.CaptureError != nil {
		payload["screenshotError"] = combined.CaptureError
	} else {
		captured := *combined.Capture
		stored, storeErr := attachment.NewService(r.homeDir).StorePath(call.SessionID, captured.Output)
		if storeErr != nil {
			payload["screenshotError"] = computer.ErrorFailure(storeErr)
		} else {
			stored.Origin = attachment.OriginTool
			out.Attachments = []store.Attachment{stored}
			out.ContextAttachments = []store.Attachment{stored}
			payload["screenshot"] = map[string]any{
				"windowID": captured.WindowID, "width": captured.Width, "height": captured.Height,
				"scaleFactor": captured.ScaleFactor, "attachmentKey": stored.AttachmentKey, "url": stored.URL,
				"coordinateSpace": "window_screenshot_pixels_top_left",
			}
		}
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(combined.Observation.Snapshot.Elements)
	return out
}

func (r *BuiltinRunner) computerAct(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if ready := r.computerReady(call, out); ready != nil {
		return *ready
	}
	args, err := decodeComputerActArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	var result computer.ActionResult
	if isComputerPointerAction(args.Action) {
		result, err = r.computer.Pointer(ctx, call.SessionID, args.AppID, args.WindowID, args.ObservationID, args.pointerInput())
	} else {
		result, err = r.computer.Act(ctx, call.SessionID, args.AppID, args.WindowID, args.ObservationID, args.ElementID, args.Action, args.Value)
	}
	if err != nil {
		return computerToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
	return out
}

func (r *BuiltinRunner) computerReady(call Call, out Result) *Result {
	if r.computer == nil {
		result := toolJSONError(out, "computer_unavailable", "Computer Use is unavailable; launch Pudding from the macOS desktop app")
		return &result
	}
	if strings.TrimSpace(call.SessionID) == "" {
		result := toolJSONError(out, "session_required", "session id is required for Computer Use")
		return &result
	}
	return nil
}

func computerToolError(out Result, err error) Result {
	failure := computer.ErrorFailure(err)
	out.Ok = false
	out.Content = jsonString(map[string]any{
		"ok": false, "code": failure.Code, "error": failure.Message,
		"retryable": failure.Retryable, "outcome": failure.Outcome,
	})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 5
	return out
}

func decodeComputerObserveArgs(raw []byte) (computerObserveArgs, error) {
	var args computerObserveArgs
	if err := decodeStructToolArgs(raw, &args); err != nil {
		return args, err
	}
	args.AppID = strings.TrimSpace(args.AppID)
	if err := validateComputerToolAppID(args.AppID); err != nil {
		return args, err
	}
	if args.WindowID == 0 {
		return args, fmt.Errorf("appID and windowID are required")
	}
	if args.MaxElements < 0 || args.MaxElements > 1000 {
		return args, fmt.Errorf("maxElements must be between 1 and 1000 when provided")
	}
	return args, nil
}

func decodeComputerUseAppArgs(raw []byte) (computerUseAppArgs, error) {
	var args computerUseAppArgs
	if err := decodeStructToolArgs(raw, &args); err != nil {
		return args, err
	}
	args.AppID = strings.TrimSpace(args.AppID)
	if err := validateComputerToolAppID(args.AppID); err != nil {
		return args, err
	}
	return args, nil
}

func decodeComputerQuitAppArgs(raw []byte) (computerQuitAppArgs, error) {
	var args computerQuitAppArgs
	if err := decodeStructToolArgs(raw, &args); err != nil {
		return args, err
	}
	args.LaunchID = strings.TrimSpace(args.LaunchID)
	if args.LaunchID == "" {
		return args, fmt.Errorf("launchID is required")
	}
	return args, nil
}

func decodeComputerActArgs(raw []byte) (computerActArgs, error) {
	var args computerActArgs
	if err := decodeStructToolArgs(raw, &args); err != nil {
		return args, err
	}
	args.AppID = strings.TrimSpace(args.AppID)
	args.ObservationID = strings.TrimSpace(args.ObservationID)
	args.ElementID = strings.TrimSpace(args.ElementID)
	args.Action = strings.TrimSpace(args.Action)
	args.Button = strings.TrimSpace(args.Button)
	if err := validateComputerToolAppID(args.AppID); err != nil {
		return args, err
	}
	if args.WindowID == 0 || args.ObservationID == "" {
		return args, fmt.Errorf("appID, windowID, and observationID are required")
	}
	if !validComputerToolAction(args.Action) {
		return args, fmt.Errorf("action must be press, set_value, select, submit, click, drag, or scroll")
	}
	if isComputerPointerAction(args.Action) {
		if args.ElementID != "" || args.Value != nil {
			return args, fmt.Errorf("elementID and value must be omitted for pointer actions")
		}
		if args.X == nil || args.Y == nil || math.IsNaN(*args.X) || math.IsInf(*args.X, 0) || math.IsNaN(*args.Y) || math.IsInf(*args.Y, 0) || *args.X < 0 || *args.Y < 0 {
			return args, fmt.Errorf("finite non-negative x and y are required for pointer actions")
		}
		switch args.Action {
		case computer.ActionClick:
			if args.ToX != nil || args.ToY != nil || args.DeltaX != nil || args.DeltaY != nil {
				return args, fmt.Errorf("click accepts only x, y, button, and clickCount")
			}
			if args.Button == "" {
				args.Button = computer.PointerButtonLeft
			}
			if args.Button != computer.PointerButtonLeft && args.Button != computer.PointerButtonRight {
				return args, fmt.Errorf("button must be left or right")
			}
			if args.ClickCount == nil {
				count := 1
				args.ClickCount = &count
			}
			if *args.ClickCount != 1 && !(args.Button == computer.PointerButtonLeft && *args.ClickCount == 2) {
				return args, fmt.Errorf("clickCount must be 1, or 2 for the left button")
			}
		case computer.ActionDrag:
			if args.ToX == nil || args.ToY == nil || !finiteNonNegativeNumber(*args.ToX) || !finiteNonNegativeNumber(*args.ToY) {
				return args, fmt.Errorf("finite non-negative toX and toY are required for drag")
			}
			if args.Button != "" || args.ClickCount != nil || args.DeltaX != nil || args.DeltaY != nil {
				return args, fmt.Errorf("drag accepts only x, y, toX, and toY")
			}
		case computer.ActionScroll:
			if args.ToX != nil || args.ToY != nil || args.Button != "" || args.ClickCount != nil {
				return args, fmt.Errorf("scroll accepts only x, y, deltaX, and deltaY")
			}
			if args.DeltaX == nil {
				zero := 0
				args.DeltaX = &zero
			}
			if args.DeltaY == nil {
				zero := 0
				args.DeltaY = &zero
			}
			if (*args.DeltaX == 0 && *args.DeltaY == 0) || *args.DeltaX < -5_000 || *args.DeltaX > 5_000 || *args.DeltaY < -5_000 || *args.DeltaY > 5_000 {
				return args, fmt.Errorf("scroll deltas must be non-zero in total and between -5000 and 5000")
			}
		}
		return args, nil
	}
	if args.ElementID == "" {
		return args, fmt.Errorf("elementID is required for semantic actions")
	}
	if args.X != nil || args.Y != nil || args.ToX != nil || args.ToY != nil || args.Button != "" || args.ClickCount != nil || args.DeltaX != nil || args.DeltaY != nil {
		return args, fmt.Errorf("pointer fields are allowed only for click, drag, and scroll")
	}
	if args.Action == computer.ActionSetValue && args.Value == nil {
		return args, fmt.Errorf("value is required for set_value")
	}
	if args.Action != computer.ActionSetValue && args.Value != nil {
		return args, fmt.Errorf("value is allowed only for set_value")
	}
	if args.Value != nil && utf8.RuneCountInString(*args.Value) > computer.MaxActionValueCharacters {
		return args, fmt.Errorf("value is too long")
	}
	return args, nil
}

func validComputerToolAction(action string) bool {
	switch action {
	case computer.ActionPress, computer.ActionSetValue, computer.ActionSelect, computer.ActionSubmit, computer.ActionClick, computer.ActionDrag, computer.ActionScroll:
		return true
	default:
		return false
	}
}

func isComputerPointerAction(action string) bool {
	return action == computer.ActionClick || action == computer.ActionDrag || action == computer.ActionScroll
}

func finiteNonNegativeNumber(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func (args computerActArgs) pointerInput() computer.PointerInput {
	input := computer.PointerInput{
		Action: args.Action, X: *args.X, Y: *args.Y, ToX: args.ToX, ToY: args.ToY,
		Button: args.Button, DeltaX: args.DeltaX, DeltaY: args.DeltaY,
	}
	if args.ClickCount != nil {
		input.ClickCount = *args.ClickCount
	}
	return input
}

func validateComputerToolAppID(appID string) error {
	if appID == "" {
		return fmt.Errorf("appID is required")
	}
	if len(appID) > computer.MaxAppIDBytes {
		return fmt.Errorf("appID is too long")
	}
	return nil
}

func computerObserveApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeComputerObserveArgs(call.Args)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"appID": args.AppID, "windowID": args.WindowID, "includeScreenshot": args.IncludeScreenshot,
	}, nil
}

func computerUseAppApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeComputerUseAppArgs(call.Args)
	if err != nil {
		return nil, err
	}
	return map[string]any{"appID": args.AppID, "foreground": args.Foreground}, nil
}

func computerQuitAppApprovalDetails(controller computer.Controller, call Call) (map[string]any, error) {
	args, err := decodeComputerQuitAppArgs(call.Args)
	if err != nil {
		return nil, err
	}
	if controller == nil {
		return nil, fmt.Errorf("Computer Use is unavailable")
	}
	appID, ok := controller.OwnedLaunchAppID(call.SessionID, args.LaunchID)
	if !ok {
		return nil, fmt.Errorf("application launch is not owned by this session")
	}
	return map[string]any{"appID": appID, "launchID": args.LaunchID}, nil
}

func computerActApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeComputerActArgs(call.Args)
	if err != nil {
		return nil, err
	}
	details := map[string]any{
		"appID": args.AppID, "windowID": args.WindowID, "observationID": args.ObservationID,
		"action": args.Action,
	}
	if args.ElementID != "" {
		details["elementID"] = args.ElementID
	}
	if args.X != nil {
		details["x"] = *args.X
		details["y"] = *args.Y
	}
	if args.ToX != nil {
		details["toX"] = *args.ToX
		details["toY"] = *args.ToY
	}
	if args.Button != "" {
		details["button"] = args.Button
	}
	if args.ClickCount != nil {
		details["clickCount"] = *args.ClickCount
	}
	if args.DeltaX != nil {
		details["deltaX"] = *args.DeltaX
		details["deltaY"] = *args.DeltaY
	}
	if args.Value != nil {
		preview := *args.Value
		if len([]rune(preview)) > 200 {
			preview = string([]rune(preview)[:200]) + "…"
		}
		details["valuePreview"] = preview
		details["valueCharacters"] = len([]rune(*args.Value))
	}
	return details, nil
}

func (r *BuiltinRunner) releaseComputerSession(sessionID string) {
	if r.computer == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = r.computer.ReleaseSession(ctx, sessionID)
}

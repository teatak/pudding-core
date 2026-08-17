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
	AppID      string                    `json:"appID"`
	WindowID   uint32                    `json:"windowID"`
	ElementID  string                    `json:"elementID"`
	Action     string                    `json:"action"`
	Value      *string                   `json:"value"`
	Actions    []computer.SemanticAction `json:"actions"`
	X          *float64                  `json:"x"`
	Y          *float64                  `json:"y"`
	ToX        *float64                  `json:"toX"`
	ToY        *float64                  `json:"toY"`
	Button     string                    `json:"button"`
	ClickCount *int                      `json:"clickCount"`
	DeltaX     *int                      `json:"deltaX"`
	DeltaY     *int                      `json:"deltaY"`
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
		out.SummaryCount = len(observed.Elements)
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
		out.SummaryCount = len(observed.Elements)
		return out
	}
	defer os.RemoveAll(tempDir)
	output := filepath.Join(tempDir, fmt.Sprintf("computer-window-%d.png", args.WindowID))
	combined, err := r.computer.ObserveCapture(ctx, call.SessionID, args.AppID, args.WindowID, args.MaxElements, output)
	if err != nil {
		return computerToolError(out, err)
	}
	payload := map[string]any{"ok": true, "observation": combined.Observation}
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
			"coordinateSpace": "window_normalized_top_left",
		}
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(combined.Observation.Elements)
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
	if args.Action == computer.ActionSequence {
		result, sequenceErr := r.computer.ActSequence(ctx, call.SessionID, args.AppID, args.WindowID, args.Actions)
		if sequenceErr != nil {
			return computerToolError(out, sequenceErr)
		}
		if result.Failure != nil {
			return computerActionSequenceFailure(out, result)
		}
		out.Ok = true
		out.Content = jsonString(map[string]any{"ok": true, "result": result})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 2
		return out
	}

	var result computer.ActionResult
	if isComputerPointerAction(args.Action) {
		result, err = r.computer.Pointer(ctx, call.SessionID, args.AppID, args.WindowID, args.pointerInput())
	} else {
		result, err = r.computer.Act(ctx, call.SessionID, args.AppID, args.WindowID, args.ElementID, args.Action, args.Value)
	}
	if err != nil {
		return computerToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 1
	return out
}

func computerActionSequenceFailure(out Result, result computer.ActionSequenceResult) Result {
	failure := result.Failure
	payload := map[string]any{
		"ok": false, "code": failure.Code, "error": failure.Message,
		"retryable": failure.Retryable, "outcome": failure.Outcome, "result": result,
	}
	if failure.Permission != "" {
		payload["permission"] = failure.Permission
	}
	if len(failure.Permissions) > 0 {
		payload["permissions"] = failure.Permissions
	}
	out.Ok = false
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
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
	payload := map[string]any{
		"ok": false, "code": failure.Code, "error": failure.Message,
		"retryable": failure.Retryable, "outcome": failure.Outcome,
	}
	if failure.Permission != "" {
		payload["permission"] = failure.Permission
	}
	if len(failure.Permissions) > 0 {
		payload["permissions"] = failure.Permissions
	}
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
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
	args.ElementID = strings.TrimSpace(args.ElementID)
	args.Action = strings.TrimSpace(args.Action)
	args.Button = strings.TrimSpace(args.Button)
	if err := validateComputerToolAppID(args.AppID); err != nil {
		return args, err
	}
	if args.WindowID == 0 {
		return args, fmt.Errorf("appID and windowID are required")
	}
	if !validComputerToolAction(args.Action) {
		return args, fmt.Errorf("action must be press, set_value, select, submit, click, drag, scroll, or action_sequence")
	}
	if args.Action == computer.ActionSequence {
		if args.ElementID != "" || args.Value != nil || hasComputerPointerFields(args) {
			return args, fmt.Errorf("elementID, value, and pointer fields must be omitted for action_sequence")
		}
		if len(args.Actions) < 2 || len(args.Actions) > computer.MaxActionSequenceSteps {
			return args, fmt.Errorf("action_sequence requires between 2 and 32 actions")
		}
		for index := range args.Actions {
			action, actionErr := validateComputerSequenceAction(args.Actions[index])
			if actionErr != nil {
				return args, fmt.Errorf("actions[%d]: %w", index, actionErr)
			}
			args.Actions[index] = action
		}
		return args, nil
	}
	if len(args.Actions) != 0 {
		return args, fmt.Errorf("actions is allowed only for action_sequence")
	}
	if isComputerPointerAction(args.Action) {
		if args.ElementID != "" || args.Value != nil {
			return args, fmt.Errorf("elementID and value must be omitted for pointer actions")
		}
		if args.X == nil || args.Y == nil || !normalizedComputerCoordinate(*args.X) || !normalizedComputerCoordinate(*args.Y) {
			return args, fmt.Errorf("x and y must be normalized window coordinates between 0 inclusive and 1 exclusive")
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
			if args.ToX == nil || args.ToY == nil || !normalizedComputerCoordinate(*args.ToX) || !normalizedComputerCoordinate(*args.ToY) {
				return args, fmt.Errorf("toX and toY must be normalized window coordinates between 0 inclusive and 1 exclusive")
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
	case computer.ActionPress, computer.ActionSetValue, computer.ActionSelect, computer.ActionSubmit, computer.ActionClick, computer.ActionDrag, computer.ActionScroll, computer.ActionSequence:
		return true
	default:
		return false
	}
}

func validateComputerSequenceAction(action computer.SemanticAction) (computer.SemanticAction, error) {
	action.ElementID = strings.TrimSpace(action.ElementID)
	action.Action = strings.TrimSpace(action.Action)
	if action.ElementID == "" {
		return action, fmt.Errorf("elementID is required")
	}
	switch action.Action {
	case computer.ActionPress, computer.ActionSetValue, computer.ActionSelect, computer.ActionSubmit:
	default:
		return action, fmt.Errorf("action must be press, set_value, select, or submit")
	}
	if action.Action == computer.ActionSetValue && action.Value == nil {
		return action, fmt.Errorf("value is required for set_value")
	}
	if action.Action != computer.ActionSetValue && action.Value != nil {
		return action, fmt.Errorf("value is allowed only for set_value")
	}
	if action.Value != nil && utf8.RuneCountInString(*action.Value) > computer.MaxActionValueCharacters {
		return action, fmt.Errorf("value is too long")
	}
	return action, nil
}

func hasComputerPointerFields(args computerActArgs) bool {
	return args.X != nil || args.Y != nil || args.ToX != nil || args.ToY != nil || args.Button != "" || args.ClickCount != nil || args.DeltaX != nil || args.DeltaY != nil
}

func isComputerPointerAction(action string) bool {
	return action == computer.ActionClick || action == computer.ActionDrag || action == computer.ActionScroll
}

func normalizedComputerCoordinate(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0 && value < 1
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
		"appID": args.AppID, "windowID": args.WindowID, "action": args.Action,
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
	if len(args.Actions) > 0 {
		actions := make([]map[string]any, 0, len(args.Actions))
		for _, action := range args.Actions {
			item := map[string]any{"elementID": action.ElementID, "action": action.Action}
			if action.Value != nil {
				preview := *action.Value
				if len([]rune(preview)) > 200 {
					preview = string([]rune(preview)[:200]) + "…"
				}
				item["valuePreview"] = preview
				item["valueCharacters"] = len([]rune(*action.Value))
			}
			actions = append(actions, item)
		}
		details["actionCount"] = len(actions)
		details["actions"] = actions
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

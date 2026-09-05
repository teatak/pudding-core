package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	AppID    string                 `json:"appID"`
	WindowID uint32                 `json:"windowID"`
	Actions  []computer.ActionInput `json:"actions"`
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
	result, err := r.computer.Act(ctx, call.SessionID, args.AppID, args.WindowID, args.Actions)
	if err != nil {
		return computerToolError(out, err)
	}
	if result.Failure != nil {
		return computerActionsFailure(out, result)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 2
	return out
}

func computerActionsFailure(out Result, result computer.ActionsResult) Result {
	failure := result.Failure
	// The failed item's outcome is not the batch outcome. Never mark an unexecuted
	// item completed or advertise replaying a batch whose prefix already ran.
	outcome := failure.Outcome
	if result.CompletedCount > 0 && outcome == "not_started" {
		outcome = "partial"
	}
	payload := map[string]any{
		"ok": false, "code": failure.Code, "error": failure.Message,
		"retryable": result.CompletedCount == 0 && outcome == "not_started" && failure.Retryable,
		"outcome":   outcome, "result": result,
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
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return args, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return args, fmt.Errorf("arguments must contain one JSON object")
	}
	args.AppID = strings.TrimSpace(args.AppID)
	if err := validateComputerToolAppID(args.AppID); err != nil {
		return args, err
	}
	if args.WindowID == 0 {
		return args, fmt.Errorf("appID and windowID are required")
	}
	validated, err := computer.NormalizeActions(args.Actions)
	if err != nil {
		return args, err
	}
	args.Actions = validated
	return args, nil
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
	actions := make([]map[string]any, 0, len(args.Actions))
	for _, action := range args.Actions {
		item := map[string]any{"type": action.Type}
		if action.ElementID != "" {
			item["elementID"] = action.ElementID
		}
		if action.X != nil {
			item["x"], item["y"] = *action.X, *action.Y
		}
		if action.ToX != nil {
			item["toX"], item["toY"] = *action.ToX, *action.ToY
		}
		if action.Button != "" {
			item["button"] = action.Button
		}
		if action.ClickCount != nil {
			item["clickCount"] = *action.ClickCount
		}
		if action.DeltaX != nil {
			item["deltaX"], item["deltaY"] = *action.DeltaX, *action.DeltaY
		}
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
	details := map[string]any{
		"appID": args.AppID, "windowID": args.WindowID,
		"actionCount": len(actions), "actions": actions,
	}
	if len(actions) == 1 {
		for key, value := range actions[0] {
			details[key] = value
		}
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

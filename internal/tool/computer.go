package tool

import (
	"context"
	"fmt"
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

type computerLaunchAppArgs struct {
	AppID string `json:"appID"`
}

type computerQuitAppArgs struct {
	LaunchID string `json:"launchID"`
}

type computerActArgs struct {
	AppID         string  `json:"appID"`
	WindowID      uint32  `json:"windowID"`
	ObservationID string  `json:"observationID"`
	ElementID     string  `json:"elementID"`
	Action        string  `json:"action"`
	Value         *string `json:"value"`
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

func (r *BuiltinRunner) computerLaunchApp(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if ready := r.computerReady(call, out); ready != nil {
		return *ready
	}
	args, err := decodeComputerLaunchAppArgs(call.Args)
	if err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	result, err := r.computer.LaunchApp(ctx, call.SessionID, args.AppID)
	if err != nil {
		return computerToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "result": result})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 4
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
	observed, err := r.computer.Observe(ctx, call.SessionID, args.AppID, args.WindowID, args.MaxElements)
	if err != nil {
		return computerToolError(out, err)
	}
	payload := map[string]any{"ok": true, "observation": observed}
	if !args.IncludeScreenshot {
		out.Ok = true
		out.Content = jsonString(payload)
		out.SummaryKind = SummaryReturnedItems
		out.SummaryCount = len(observed.Snapshot.Elements)
		return out
	}

	tempDir, err := os.MkdirTemp("", "pudding-computer-")
	if err != nil {
		payload["screenshotError"] = computer.ErrorFailure(err)
		out.Ok = true
		out.Content = jsonString(payload)
		return out
	}
	defer os.RemoveAll(tempDir)
	output := filepath.Join(tempDir, fmt.Sprintf("computer-window-%d.png", args.WindowID))
	captured, err := r.computer.Capture(ctx, call.SessionID, args.AppID, args.WindowID, output)
	if err != nil {
		payload["screenshotError"] = computer.ErrorFailure(err)
	} else {
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
			}
		}
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(observed.Snapshot.Elements)
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
	result, err := r.computer.Act(ctx, call.SessionID, args.AppID, args.WindowID, args.ObservationID, args.ElementID, args.Action, args.Value)
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
	if args.AppID == "" || args.WindowID == 0 {
		return args, fmt.Errorf("appID and windowID are required")
	}
	if args.MaxElements < 0 || args.MaxElements > 1000 {
		return args, fmt.Errorf("maxElements must be between 1 and 1000 when provided")
	}
	return args, nil
}

func decodeComputerLaunchAppArgs(raw []byte) (computerLaunchAppArgs, error) {
	var args computerLaunchAppArgs
	if err := decodeStructToolArgs(raw, &args); err != nil {
		return args, err
	}
	args.AppID = strings.TrimSpace(args.AppID)
	if args.AppID == "" {
		return args, fmt.Errorf("appID is required")
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
	if args.AppID == "" || args.WindowID == 0 || args.ObservationID == "" || args.ElementID == "" {
		return args, fmt.Errorf("appID, windowID, observationID, and elementID are required")
	}
	if args.Action != computer.ActionPress && args.Action != computer.ActionSetValue {
		return args, fmt.Errorf("action must be press or set_value")
	}
	if args.Action == computer.ActionSetValue && args.Value == nil {
		return args, fmt.Errorf("value is required for set_value")
	}
	if args.Action == computer.ActionPress && args.Value != nil {
		return args, fmt.Errorf("value is not allowed for press")
	}
	return args, nil
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

func computerLaunchAppApprovalDetails(call Call) (map[string]any, error) {
	args, err := decodeComputerLaunchAppArgs(call.Args)
	if err != nil {
		return nil, err
	}
	return map[string]any{"appID": args.AppID}, nil
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
		"elementID": args.ElementID, "action": args.Action,
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

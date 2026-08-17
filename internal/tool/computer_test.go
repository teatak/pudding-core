package tool

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/computer"
)

func TestComputerActionGuidanceChainsTheReturnedObservation(t *testing.T) {
	for _, definition := range BuiltinDefinitions() {
		if definition.Name != ComputerAct {
			continue
		}
		if !strings.Contains(definition.Description, "use it for the next semantic action") ||
			!strings.Contains(definition.Description, "select") ||
			!strings.Contains(definition.Description, "submit") ||
			!strings.Contains(definition.Description, "exactly one Return") ||
			!strings.Contains(definition.Description, "Pointer actions") ||
			!strings.Contains(definition.Description, "double-click") {
			t.Fatalf("unexpected Computer Act guidance: %s", definition.Description)
		}
		return
	}
	t.Fatal("Computer Act definition not found")
}

func TestComputerUseAppDefaultsToBackground(t *testing.T) {
	args, err := decodeComputerUseAppArgs([]byte(`{"appID":"com.example.App"}`))
	if err != nil {
		t.Fatal(err)
	}
	if args.Foreground {
		t.Fatal("foreground must default to false")
	}

	for _, definition := range BuiltinDefinitions() {
		if definition.Name == ComputerUseApp {
			if !strings.Contains(definition.Description, "background by default") ||
				!strings.Contains(definition.Description, "before necessary pointer input") {
				t.Fatalf("unexpected Computer Use App guidance: %s", definition.Description)
			}
			return
		}
	}
	t.Fatal("Computer Use App definition not found")
}

func TestComputerPermissionFailuresRemainStructured(t *testing.T) {
	result := computerToolError(Result{}, &computer.OperationError{
		Code:        "computer_permission_denied",
		Message:     "Computer Use permission was not granted",
		Permissions: []string{"accessibility", "screenRecording"},
		Outcome:     "not_started",
	})
	var payload struct {
		Code        string   `json:"code"`
		Permissions []string `json:"permissions"`
		Outcome     string   `json:"outcome"`
	}
	if result.Ok || json.Unmarshal([]byte(result.Content), &payload) != nil ||
		payload.Code != "computer_permission_denied" || payload.Outcome != "not_started" || len(payload.Permissions) != 2 {
		t.Fatalf("unexpected permission result: %+v content=%s", result, result.Content)
	}
}

type fakeComputerController struct {
	lastSession string
	released    string
	acted       bool
	foreground  bool
}

func (f *fakeComputerController) ListApps(_ context.Context, sessionID string) (computer.AppList, error) {
	f.lastSession = sessionID
	return computer.AppList{Apps: []computer.Application{{AppID: "com.example.App", Name: "Example"}}}, nil
}

func (f *fakeComputerController) UseApp(_ context.Context, sessionID, appID string, foreground bool) (computer.UseResult, error) {
	f.lastSession = sessionID
	f.foreground = foreground
	launchID := "launch_1"
	windowAppID := appID
	return computer.UseResult{LaunchID: &launchID, AppID: appID, Name: "Example", PID: 42, WindowStatus: computer.WindowStatusReady, Windows: []computer.CapturableWindow{{WindowID: 7, PID: 42, AppID: &windowAppID}}}, nil
}

func (f *fakeComputerController) OwnedLaunchAppID(sessionID, launchID string) (string, bool) {
	f.lastSession = sessionID
	return "com.example.App", launchID == "launch_1"
}

func (f *fakeComputerController) QuitApp(_ context.Context, sessionID, launchID string) (computer.QuitResult, error) {
	f.lastSession = sessionID
	return computer.QuitResult{LaunchID: launchID, AppID: "com.example.App", Name: "Example", PID: 42, Closed: true}, nil
}

func (f *fakeComputerController) Observe(_ context.Context, sessionID, appID string, windowID uint32, _ int) (computer.ManagedObservation, error) {
	f.lastSession = sessionID
	return computer.ManagedObservation{ObservationID: "obs_1", Snapshot: computer.Observation{AppID: appID, WindowID: &windowID, Elements: []computer.Element{{ElementID: "button", WindowID: &windowID, Actions: []string{computer.ActionPress}}}}}, nil
}

func (f *fakeComputerController) ObserveCapture(_ context.Context, sessionID, appID string, windowID uint32, _ int, output string) (computer.ManagedObservationCapture, error) {
	f.lastSession = sessionID
	if err := os.WriteFile(output, tinyPNG, 0o600); err != nil {
		return computer.ManagedObservationCapture{}, err
	}
	return computer.ManagedObservationCapture{
		Observation: computer.ManagedObservation{ObservationID: "obs_capture", Snapshot: computer.Observation{AppID: appID, WindowID: &windowID}},
		Capture:     &computer.Capture{WindowID: windowID, Output: output, Width: 1, Height: 1, ScaleFactor: 1},
	}, nil
}

func (f *fakeComputerController) Act(_ context.Context, sessionID, appID string, _ uint32, _ string, elementID, action string, _ *string) (computer.ActionResult, error) {
	f.lastSession = sessionID
	f.acted = true
	return computer.ActionResult{Action: computer.NativeAction{AppID: appID, ElementID: elementID, Action: action, Completed: true}}, nil
}

func (f *fakeComputerController) Pointer(_ context.Context, sessionID, appID string, _ uint32, _ string, pointer computer.PointerInput) (computer.ActionResult, error) {
	f.lastSession = sessionID
	f.acted = true
	return computer.ActionResult{Action: computer.NativeAction{
		AppID: appID, Action: pointer.Action, Completed: true, X: &pointer.X, Y: &pointer.Y,
		ToX: pointer.ToX, ToY: pointer.ToY, Button: pointer.Button, ClickCount: pointer.ClickCount,
		DeltaX: pointer.DeltaX, DeltaY: pointer.DeltaY,
	}}, nil
}

func (f *fakeComputerController) ReleaseSession(_ context.Context, sessionID string) error {
	f.released = sessionID
	return nil
}

func TestComputerToolsRouteExplicitSessionAndScreenshot(t *testing.T) {
	fake := &fakeComputerController{}
	runner := NewBuiltinRunner(WithComputer(fake), WithHomeDir(t.TempDir()))
	listed := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "list", Name: ComputerListApps, Args: json.RawMessage(`{}`)})
	if !listed.Ok || fake.lastSession != "session_a" {
		t.Fatalf("list result=%+v session=%q", listed, fake.lastSession)
	}
	used := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "use", Name: ComputerUseApp, Args: json.RawMessage(`{"appID":"com.example.App","foreground":true}`)})
	if !used.Ok || fake.lastSession != "session_a" || !fake.foreground {
		t.Fatalf("use result=%+v session=%q", used, fake.lastSession)
	}
	var envelope struct {
		Result computer.UseResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(used.Content), &envelope); err != nil || len(envelope.Result.Windows) != 1 || envelope.Result.Windows[0].WindowID != 7 {
		t.Fatalf("use content=%s err=%v", used.Content, err)
	}
	quit := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "quit", Name: ComputerQuitApp, Args: json.RawMessage(`{"launchID":"launch_1"}`)})
	if !quit.Ok || fake.lastSession != "session_a" {
		t.Fatalf("quit result=%+v session=%q", quit, fake.lastSession)
	}
	observed := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "observe", Name: ComputerObserve, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"includeScreenshot":true}`)})
	if !observed.Ok || len(observed.Attachments) != 1 || len(observed.ContextAttachments) != 1 {
		t.Fatalf("unexpected observe result: %+v", observed)
	}
	acted := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "act", Name: ComputerAct, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","elementID":"button","action":"press"}`)})
	if !acted.Ok || !fake.acted {
		t.Fatalf("unexpected act result: %+v", acted)
	}
	clicked := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "click", Name: ComputerAct, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"observationID":"obs_capture","action":"click","x":0.5,"y":0.5}`)})
	if !clicked.Ok {
		t.Fatalf("unexpected click result: %+v", clicked)
	}
	scrolled := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "scroll", Name: ComputerAct, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"observationID":"obs_capture","action":"scroll","x":0.5,"y":0.5,"deltaY":120}`)})
	if !scrolled.Ok {
		t.Fatalf("unexpected scroll result: %+v", scrolled)
	}
	runner.CloseSession("session_a")
	if fake.released != "session_a" {
		t.Fatalf("released = %q", fake.released)
	}
}

func TestComputerLifecycleRiskAndApprovalDetails(t *testing.T) {
	useCall := Call{Name: ComputerUseApp, Args: json.RawMessage(`{"appID":"com.example.App"}`)}
	useRisk, ok := ClassifyToolCall(useCall.Name, useCall.Args)
	if !ok || useRisk.Class != RiskClassWrite || useRisk.Scope != "computer" || useRisk.Operation != "computer_use_app" {
		t.Fatalf("unexpected use risk: %+v ok=%v", useRisk, ok)
	}
	useDetails, err := NewBuiltinRunner().ApprovalDetails(context.Background(), useCall)
	if err != nil || useDetails["appID"] != "com.example.App" || useDetails["foreground"] != false {
		t.Fatalf("unexpected use details: %+v err=%v", useDetails, err)
	}

	quitCall := Call{SessionID: "session_a", Name: ComputerQuitApp, Args: json.RawMessage(`{"launchID":"launch_1"}`)}
	quitRisk, ok := ClassifyToolCall(quitCall.Name, quitCall.Args)
	if !ok || quitRisk.Class != RiskClassWrite || quitRisk.Scope != "computer" || quitRisk.Operation != "computer_quit_app" {
		t.Fatalf("unexpected quit risk: %+v ok=%v", quitRisk, ok)
	}
	quitDetails, err := NewBuiltinRunner(WithComputer(&fakeComputerController{})).ApprovalDetails(context.Background(), quitCall)
	if err != nil || quitDetails["launchID"] != "launch_1" || quitDetails["appID"] != "com.example.App" {
		t.Fatalf("unexpected quit details: %+v err=%v", quitDetails, err)
	}
}

func TestComputerActRiskAndApprovalDetails(t *testing.T) {
	call := Call{Name: ComputerAct, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","elementID":"field","action":"set_value","value":"hello"}`)}
	risk, ok := ClassifyToolCall(call.Name, call.Args)
	if !ok || risk.Class != RiskClassWrite || risk.Scope != "computer" || risk.LowRisk || risk.Operation != "computer_set_value" {
		t.Fatalf("unexpected risk: %+v ok=%v", risk, ok)
	}
	details, err := NewBuiltinRunner().ApprovalDetails(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if details["appID"] != "com.example.App" || details["valuePreview"] != "hello" || details["valueCharacters"] != 5 {
		t.Fatalf("unexpected approval details: %+v", details)
	}
	for _, action := range []string{computer.ActionSelect, computer.ActionSubmit} {
		raw, err := json.Marshal(map[string]any{
			"appID": "com.example.App", "windowID": 42, "observationID": "obs_1",
			"elementID": "target", "action": action,
		})
		if err != nil {
			t.Fatal(err)
		}
		risk, ok := ClassifyToolCall(ComputerAct, raw)
		if !ok || risk.Operation != "computer_"+action || risk.Class != RiskClassWrite {
			t.Fatalf("unexpected %s risk: %+v ok=%v", action, risk, ok)
		}
	}
	clickCall := Call{Name: ComputerAct, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":12,"y":34}`)}
	clickRisk, ok := ClassifyToolCall(clickCall.Name, clickCall.Args)
	clickDetails, err := NewBuiltinRunner().ApprovalDetails(context.Background(), clickCall)
	if !ok || clickRisk.Operation != "computer_click" || err != nil || clickDetails["x"] != float64(12) || clickDetails["y"] != float64(34) {
		t.Fatalf("unexpected click risk=%+v details=%+v err=%v", clickRisk, clickDetails, err)
	}
	dragCall := Call{Name: ComputerAct, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"drag","x":12,"y":34,"toX":56,"toY":78}`)}
	dragRisk, ok := ClassifyToolCall(dragCall.Name, dragCall.Args)
	dragDetails, err := NewBuiltinRunner().ApprovalDetails(context.Background(), dragCall)
	if !ok || dragRisk.Operation != "computer_drag" || err != nil || dragDetails["toX"] != float64(56) || dragDetails["toY"] != float64(78) {
		t.Fatalf("unexpected drag risk=%+v details=%+v err=%v", dragRisk, dragDetails, err)
	}
}

func TestComputerObserveRiskAndApprovalDetails(t *testing.T) {
	call := Call{Name: ComputerObserve, Args: json.RawMessage(`{"appID":"com.example.App","windowID":42,"includeScreenshot":true}`)}
	risk, ok := ClassifyToolCall(call.Name, call.Args)
	if !ok || risk.Class != RiskClassRead || risk.Scope != "computer" || risk.Operation != "computer_observe" {
		t.Fatalf("unexpected risk: %+v ok=%v", risk, ok)
	}
	details, err := NewBuiltinRunner().ApprovalDetails(context.Background(), call)
	if err != nil {
		t.Fatal(err)
	}
	if details["appID"] != "com.example.App" || details["windowID"] != uint32(42) || details["includeScreenshot"] != true {
		t.Fatalf("unexpected approval details: %+v", details)
	}
}

func TestComputerToolArgumentsEnforceSharedSizeLimits(t *testing.T) {
	validValue := strings.Repeat("🙂", computer.MaxActionValueCharacters)
	validRaw, err := json.Marshal(map[string]any{
		"appID": "com.example.App", "windowID": 42, "observationID": "obs_1",
		"elementID": "field", "action": computer.ActionSetValue, "value": validValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeComputerActArgs(validRaw); err != nil {
		t.Fatalf("schema-limit value rejected: %v", err)
	}

	oversizedRaw, err := json.Marshal(map[string]any{
		"appID": "com.example.App", "windowID": 42, "observationID": "obs_1",
		"elementID": "field", "action": computer.ActionSetValue,
		"value": strings.Repeat("🙂", computer.MaxActionValueCharacters+1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeComputerActArgs(oversizedRaw); err == nil {
		t.Fatal("oversized action value was accepted")
	}

	for _, action := range []string{computer.ActionSelect, computer.ActionSubmit} {
		raw, err := json.Marshal(map[string]any{
			"appID": "com.example.App", "windowID": 42, "observationID": "obs_1",
			"elementID": "target", "action": action,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = decodeComputerActArgs(raw); err != nil {
			t.Fatalf("%s action rejected: %v", action, err)
		}
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","elementID":"target","action":"submit","value":"x"}`)); err == nil {
		t.Fatal("submit action value was accepted")
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","elementID":"target","action":"confirm"}`)); err == nil {
		t.Fatal("removed confirm action was accepted")
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":1,"y":2}`)); err != nil {
		t.Fatalf("coordinate click rejected: %v", err)
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":1,"y":2,"clickCount":2}`)); err != nil {
		t.Fatalf("double click rejected: %v", err)
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":1,"y":2,"button":"right"}`)); err != nil {
		t.Fatalf("right click rejected: %v", err)
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"drag","x":1,"y":2,"toX":3,"toY":4}`)); err != nil {
		t.Fatalf("drag rejected: %v", err)
	}
	if _, err = decodeComputerActArgs([]byte(`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"scroll","x":1,"y":2,"deltaY":120}`)); err != nil {
		t.Fatalf("scroll rejected: %v", err)
	}
	for _, raw := range []string{
		`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":1}`,
		`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":1,"y":2,"elementID":"button"}`,
		`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","elementID":"button","action":"press","x":1,"y":2}`,
		`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"click","x":1,"y":2,"button":"right","clickCount":2}`,
		`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"drag","x":1,"y":2,"toX":3}`,
		`{"appID":"com.example.App","windowID":42,"observationID":"obs_1","action":"scroll","x":1,"y":2}`,
	} {
		if _, err = decodeComputerActArgs([]byte(raw)); err == nil {
			t.Fatalf("invalid coordinate action accepted: %s", raw)
		}
	}

	oversizedAppID, err := json.Marshal(map[string]string{
		"appID": strings.Repeat("a", computer.MaxAppIDBytes+1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = decodeComputerUseAppArgs(oversizedAppID); err == nil {
		t.Fatal("oversized appID was accepted")
	}
}

var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
	0x89, 0x00, 0x00, 0x00, 0x0d, 0x49, 0x44, 0x41,
	0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0xf0, 0x1f,
	0x00, 0x05, 0x00, 0x01, 0xff, 0x89, 0x99, 0x3d, 0x1d,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
	0x42, 0x60, 0x82,
}

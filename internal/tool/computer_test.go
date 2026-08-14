package tool

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/teatak/pudding-core/internal/computer"
)

type fakeComputerController struct {
	lastSession string
	released    string
	acted       bool
}

func (f *fakeComputerController) ListApps(_ context.Context, sessionID string) (computer.AppList, error) {
	f.lastSession = sessionID
	return computer.AppList{Apps: []computer.Application{{AppID: "com.example.App", Name: "Example"}}}, nil
}

func (f *fakeComputerController) UseApp(_ context.Context, sessionID, appID string) (computer.UseResult, error) {
	f.lastSession = sessionID
	launchID := "launch_1"
	return computer.UseResult{LaunchID: &launchID, AppID: appID, Name: "Example", PID: 42}, nil
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

func (f *fakeComputerController) Capture(_ context.Context, sessionID, _ string, windowID uint32, output string) (computer.Capture, error) {
	f.lastSession = sessionID
	if err := os.WriteFile(output, tinyPNG, 0o600); err != nil {
		return computer.Capture{}, err
	}
	return computer.Capture{WindowID: windowID, Output: output, Width: 1, Height: 1, ScaleFactor: 1}, nil
}

func (f *fakeComputerController) Act(_ context.Context, sessionID, appID string, _ uint32, _ string, elementID, action string, _ *string) (computer.ActionResult, error) {
	f.lastSession = sessionID
	f.acted = true
	return computer.ActionResult{Action: computer.NativeAction{AppID: appID, ElementID: elementID, Action: action, Completed: true}}, nil
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
	used := runner.Call(context.Background(), Call{SessionID: "session_a", CallID: "use", Name: ComputerUseApp, Args: json.RawMessage(`{"appID":"com.example.App"}`)})
	if !used.Ok || fake.lastSession != "session_a" {
		t.Fatalf("use result=%+v session=%q", used, fake.lastSession)
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
	if err != nil || useDetails["appID"] != "com.example.App" {
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

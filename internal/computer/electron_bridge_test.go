package computer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestElectronBridgeMapsBundleIDsAndRoutesSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bridge-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["sessionID"] != "session_a" {
			t.Fatalf("sessionID = %#v", request["sessionID"])
		}
		writeJSON(w, map[string]any{
			"apps": []map[string]any{{"bundleID": "com.example.App", "name": "Example", "running": true, "active": true, "controllable": true}},
		})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	apps, err := service.ListApps(context.Background(), "session_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(apps.Apps) != 1 || apps.Apps[0].AppID != "com.example.App" || !apps.Apps[0].Running || !apps.Apps[0].Controllable {
		t.Fatalf("unexpected apps: %#v", apps)
	}
}

func TestElectronBridgePreservesActionOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]any{"error": "native outcome unknown", "code": "computer_action_failed", "retryable": false, "outcome": "unknown"})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Act(context.Background(), "session_a", "com.example.App", 42, "button", ActionPress, nil)
	assertOperationCode(t, err, "computer_action_failed")
	failure := ErrorFailure(err)
	if failure.Retryable || failure.Outcome != "unknown" {
		t.Fatalf("unexpected failure: %#v", failure)
	}
}

func TestElectronBridgePreservesStructuredPermissionFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		writeJSON(w, map[string]any{
			"error": "Computer Use permission was not granted", "code": "computer_permission_denied",
			"permissions": []string{"accessibility", "screenRecording"}, "outcome": "not_started",
		})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Observe(context.Background(), "session_a", "com.example.App", 42, 20)
	failure := ErrorFailure(err)
	if failure.Code != "computer_permission_denied" || failure.Outcome != "not_started" || len(failure.Permissions) != 2 {
		t.Fatalf("unexpected permission failure: %#v", failure)
	}
}

func TestElectronBridgeRoutesApplicationLifecycle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["sessionID"] != "session_a" || request["appID"] != "com.example.App" {
			t.Fatalf("unexpected request: %#v", request)
		}
		switch r.URL.Path {
		case "/computer/apps/use":
			if request["foreground"] != true {
				t.Fatalf("foreground = %#v", request["foreground"])
			}
			writeJSON(w, map[string]any{"bundleID": "com.example.App", "name": "Example", "pid": 42, "newlyLaunched": true, "windowStatus": WindowStatusReady, "windows": []map[string]any{{"windowID": 7, "pid": 42, "bundleID": "com.example.App", "frame": map[string]float64{"x": 0, "y": 0, "width": 100, "height": 80}}}})
		case "/computer/apps/quit":
			if request["pid"] != float64(42) {
				t.Fatalf("pid = %#v", request["pid"])
			}
			writeJSON(w, map[string]any{"bundleID": "com.example.App", "pid": 42, "closed": true})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	launched, err := service.UseApp(context.Background(), "session_a", "com.example.App", true)
	if err != nil || launched.PID != 42 || !launched.NewlyLaunched || len(launched.Windows) != 1 || launched.Windows[0].WindowID != 7 {
		t.Fatalf("unexpected launch: %#v err=%v", launched, err)
	}
	quit, err := service.QuitApp(context.Background(), "session_a", "com.example.App", 42)
	if err != nil || !quit.Closed {
		t.Fatalf("unexpected quit: %#v err=%v", quit, err)
	}
}

func TestElectronBridgeRoutesAtomicObserveCapture(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/computer/observe-capture" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["sessionID"] != "session_a" || request["appID"] != "com.example.App" || request["windowID"] != float64(7) || request["maxElements"] != float64(50) {
			t.Fatalf("unexpected request: %#v", request)
		}
		writeJSON(w, map[string]any{
			"observation": map[string]any{"bundleID": "com.example.App", "windowID": 7, "pid": 42, "elements": []any{}, "windows": []any{}},
			"capture":     map[string]any{"windowID": 7, "output": "/tmp/window.png", "width": 100, "height": 80, "scaleFactor": 2},
		})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ObserveCapture(context.Background(), "session_a", "com.example.App", 7, 50, "/tmp/window.png")
	if err != nil || result.Observation.PID != 42 || result.Capture == nil || result.Capture.Width != 100 {
		t.Fatalf("unexpected observe-capture: %#v err=%v", result, err)
	}
}

func TestElectronBridgeRoutesPointerAction(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/computer/pointer" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request["action"] != ActionDrag || request["x"] != float64(12) || request["y"] != float64(34) || request["toX"] != float64(56) || request["toY"] != float64(78) || request["captureWidth"] != float64(200) || request["captureHeight"] != float64(100) || request["scaleFactor"] != float64(2) {
			t.Fatalf("unexpected request: %#v", request)
		}
		writeJSON(w, map[string]any{"bundleID": "com.example.App", "elementID": "", "action": ActionDrag, "completed": true, "x": 12, "y": 34, "toX": 56, "toY": 78})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	toX, toY := 56.0, 78.0
	result, err := service.Pointer(context.Background(), "session_a", "com.example.App", 7, ScreenshotPointer{PointerInput: PointerInput{Action: ActionDrag, X: 12, Y: 34, ToX: &toX, ToY: &toY}, CaptureWidth: 200, CaptureHeight: 100, ScaleFactor: 2})
	if err != nil || result.Action != ActionDrag || result.X == nil || *result.X != 12 || result.ToX == nil || *result.ToX != 56 {
		t.Fatalf("unexpected pointer action: %#v err=%v", result, err)
	}
}

func TestElectronBridgeRejectsNonLoopbackURL(t *testing.T) {
	if _, err := NewElectronBridgeService(ElectronBridgeConfig{URL: "http://192.0.2.1:9000", Token: "token"}); err == nil {
		t.Fatal("expected non-loopback URL rejection")
	}
}

func TestElectronBridgeMarksActionTransportFailureUnknown(t *testing.T) {
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: "http://127.0.0.1:1", Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Act(context.Background(), "session_a", "com.example.App", 42, "button", ActionPress, nil)
	failure := ErrorFailure(err)
	if failure.Outcome != "unknown" || failure.Retryable {
		t.Fatalf("unexpected action transport failure: %#v", failure)
	}
}

func TestElectronBridgeMarksUseTransportFailureUnknown(t *testing.T) {
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: "http://127.0.0.1:1", Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.UseApp(context.Background(), "session_a", "com.example.App", false)
	failure := ErrorFailure(err)
	if failure.Outcome != "unknown" || failure.Retryable {
		t.Fatalf("unexpected use transport failure: %#v", failure)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

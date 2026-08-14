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
			"permissions":       map[string]bool{"accessibility": true, "screenRecording": false},
			"apps":              []map[string]any{{"bundleID": "com.example.App", "name": "Example", "pid": 7, "active": true, "controllable": true, "windows": []any{}}},
			"capturableWindows": []map[string]any{{"windowID": 42, "bundleID": "com.example.App", "frame": map[string]float64{"x": 0, "y": 0, "width": 100, "height": 80}}},
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
	if len(apps.Apps) != 1 || apps.Apps[0].AppID != "com.example.App" || !apps.Apps[0].Controllable || len(apps.CapturableWindows) != 1 || *apps.CapturableWindows[0].AppID != "com.example.App" {
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
		case "/computer/apps/launch":
			writeJSON(w, map[string]any{"bundleID": "com.example.App", "name": "Example", "pid": 42, "newlyLaunched": true})
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
	launched, err := service.LaunchApp(context.Background(), "session_a", "com.example.App")
	if err != nil || launched.PID != 42 || !launched.NewlyLaunched {
		t.Fatalf("unexpected launch: %#v err=%v", launched, err)
	}
	quit, err := service.QuitApp(context.Background(), "session_a", "com.example.App", 42)
	if err != nil || !quit.Closed {
		t.Fatalf("unexpected quit: %#v err=%v", quit, err)
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

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

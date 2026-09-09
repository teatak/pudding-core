package computer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBackgroundDeliveryUsesExistingActionsAndSessionWithoutObservation(t *testing.T) {
	service := &fakeService{}
	manager := NewManager(service)
	actions := pointerActions(PointerInput{Action: ActionClick, X: 0.5, Y: 0.4, Delivery: "background"})
	result, err := manager.Act(context.Background(), "session_background", "com.apple.iCal", 7, actions)
	if err != nil || result.Failure != nil || result.CompletedCount != 1 || service.pointers != 1 || service.observes != 0 || service.lastSession != "session_background" || service.lastPointer.Delivery != "background" || result.Actions[0].Delivery != "background" {
		t.Fatalf("result=%+v err=%v service=%+v", result, err, service)
	}
}

func TestBackgroundDeliveryRejectsUnsupportedActionShapesBeforeExecution(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"click","x":0.5,"y":0.5,"delivery":"auto"}]`,
		`[{"type":"click","x":0.5,"y":0.5,"delivery":"background","clickCount":2}]`,
		`[{"type":"click","x":0.5,"y":0.5,"delivery":"background","button":"right"}]`,
		`[{"type":"drag","x":0.5,"y":0.5,"toX":0.6,"toY":0.6,"delivery":"background"}]`,
		`[{"type":"scroll","x":0.5,"y":0.5,"deltaY":40,"delivery":"background"}]`,
		`[{"type":"press","elementID":"e_one","delivery":"background"}]`,
		`[{"type":"press_key","key":"enter","delivery":"background"}]`,
	} {
		var actions []ActionInput
		if err := json.Unmarshal([]byte(raw), &actions); err != nil {
			t.Fatal(err)
		}
		if _, err := NormalizeActions(actions); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestBackgroundDeliveryRoundTripsAcrossElectronBridge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if r.URL.Path != "/computer/pointer" || body["delivery"] != "background" || body["sessionID"] != "session_background" || body["action"] != "click" {
			t.Fatalf("%+v", body)
		}
		writeJSON(w, map[string]any{"bundleID": "com.apple.iCal", "elementID": "", "action": "click", "completed": true, "x": 0.5, "y": 0.4, "button": "left", "clickCount": 1, "delivery": "background"})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	pointer := PointerInput{Action: ActionClick, X: 0.5, Y: 0.4, Button: "left", ClickCount: 1, Delivery: "background"}
	result, err := service.Pointer(context.Background(), "session_background", "com.apple.iCal", 7, pointer)
	if err != nil || !matchesPointerResult(result, "com.apple.iCal", pointer) {
		t.Fatalf("%+v %v", result, err)
	}
	result.Delivery = "foreground"
	if matchesPointerResult(result, "com.apple.iCal", pointer) {
		t.Fatal("accepted a silent foreground fallback")
	}
}

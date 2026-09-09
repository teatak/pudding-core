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
	var actions []ActionInput
	for _, pointer := range backgroundGestures() {
		actions = append(actions, pointerActions(pointer)...)
	}
	result, err := manager.Act(context.Background(), "session_background", "com.example.Custom", 7, actions)
	if err != nil || result.Failure != nil || result.CompletedCount != 5 || service.pointers != 5 || service.observes != 0 || service.lastSession != "session_background" || service.lastPointer.Delivery != "background" || result.Actions[0].Delivery != "background" {
		t.Fatalf("result=%+v err=%v service=%+v", result, err, service)
	}
}

func backgroundGestures() []PointerInput {
	toX, toY, deltaX, deltaY := 0.7, 0.6, -40, 120
	return []PointerInput{
		{Action: ActionClick, X: 0.5, Y: 0.4, Button: "left", ClickCount: 1, Delivery: "background"},
		{Action: ActionClick, X: 0.5, Y: 0.4, Button: "left", ClickCount: 2, Delivery: "background"},
		{Action: ActionClick, X: 0.5, Y: 0.4, Button: "right", ClickCount: 1, Delivery: "background"},
		{Action: ActionDrag, X: 0.5, Y: 0.4, ToX: &toX, ToY: &toY, Delivery: "background"},
		{Action: ActionScroll, X: 0.5, Y: 0.4, DeltaX: &deltaX, DeltaY: &deltaY, Delivery: "background"},
	}
}

func TestBackgroundDeliveryRejectsUnsupportedActionShapesBeforeExecution(t *testing.T) {
	for _, raw := range []string{
		`[{"type":"click","x":0.5,"y":0.5,"delivery":"auto"}]`,
		`[{"type":"click","x":0.5,"y":0.5,"delivery":"background","button":"right","clickCount":2}]`,
		`[{"type":"click","x":1,"y":0.5,"delivery":"background"}]`,
		`[{"type":"drag","x":0.5,"y":0.5,"toX":0.6,"delivery":"background"}]`,
		`[{"type":"scroll","x":0.5,"y":0.5,"deltaY":0,"delivery":"background"}]`,
		`[{"type":"scroll","x":0.5,"y":0.5,"deltaY":5001,"delivery":"background"}]`,
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
		if r.URL.Path != "/computer/pointer" || body["delivery"] != "background" || body["sessionID"] != "session_background" || body["appID"] != "com.example.Custom" {
			t.Fatalf("%+v", body)
		}
		body["bundleID"] = body["appID"]
		body["completed"] = true
		writeJSON(w, body)
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	for _, pointer := range backgroundGestures() {
		result, err := service.Pointer(context.Background(), "session_background", "com.example.Custom", 7, pointer)
		if err != nil || !matchesPointerResult(result, "com.example.Custom", pointer) {
			t.Fatalf("%+v %v", result, err)
		}
		result.Delivery = "foreground"
		if matchesPointerResult(result, "com.example.Custom", pointer) {
			t.Fatal("accepted a silent foreground fallback")
		}
	}
}

package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestElectronBridgeServiceOpenListAndRelease(t *testing.T) {
	const token = "bridge-token"
	tabsBySession := map[string][]electronBridgeSnapshot{}
	var closedTabID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var req electronBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/browser/tabs/list":
			writeElectronBridgeTestJSON(w, electronBridgeTabsResponse{Tabs: tabsBySession[req.SessionID], ProcessMode: "webview"})
		case "/browser/tabs/open":
			if req.SessionID == "" || req.TabID == "" || req.URL != "https://example.com" {
				t.Fatalf("unexpected open request: %+v", req)
			}
			tab := electronBridgeSnapshot{
				SessionID: req.SessionID,
				TabID:     req.TabID,
				Status:    "detached",
				URL:       req.URL,
				Title:     "Example",
				RuntimeID: "webContents:1",
			}
			tabsBySession[req.SessionID] = []electronBridgeSnapshot{tab}
			writeElectronBridgeTestJSON(w, tab)
		case "/browser/tabs/close":
			closedTabID = req.TabID
			writeElectronBridgeTestJSON(w, map[string]bool{"ok": true})
		default:
			http.Error(w, `{"error":"browser bridge route not found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := service.Open(context.Background(), "sess_a", "", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if tab.SessionID != "sess_a" || tab.ID == "" || tab.URL != "https://example.com" || tab.Mode != "webview" {
		t.Fatalf("unexpected tab snapshot: %+v", tab)
	}
	tabs, err := service.ListTabs(context.Background(), "sess_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].ID != tab.ID {
		t.Fatalf("unexpected tabs: %+v", tabs)
	}
	if err := service.ReleaseTab(context.Background(), "sess_a", tab.ID); err != nil {
		t.Fatal(err)
	}
	if closedTabID != tab.ID {
		t.Fatalf("expected closed tab %q, got %q", tab.ID, closedTabID)
	}
}

func TestElectronBridgeServiceCreateTabReservesNativeSlot(t *testing.T) {
	var created electronBridgeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/browser/tabs/create" {
			t.Fatalf("unexpected bridge path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
			t.Fatal(err)
		}
		writeElectronBridgeTestJSON(w, electronBridgeSnapshot{
			SessionID: created.SessionID,
			TabID:     created.TabID,
			Status:    "pending",
			URL:       created.URL,
		})
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := service.CreateTab(context.Background(), "sess_blank")
	if err != nil {
		t.Fatal(err)
	}
	if created.SessionID != "sess_blank" || created.TabID != tab.ID || created.URL != "about:blank" {
		t.Fatalf("unexpected create request: %+v", created)
	}
	if tab.SessionID != "sess_blank" || tab.ID == "" || tab.URL != "about:blank" || tab.Title != "" {
		t.Fatalf("unexpected blank tab metadata: %+v", tab)
	}
}

func TestElectronBridgeServiceOpenNewTabUsesSingleOpenRequest(t *testing.T) {
	var opened electronBridgeRequest
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path != "/browser/tabs/open" {
			t.Fatalf("unexpected bridge path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&opened); err != nil {
			t.Fatal(err)
		}
		writeElectronBridgeTestJSON(w, electronBridgeSnapshot{
			SessionID: opened.SessionID,
			TabID:     opened.TabID,
			Status:    "detached",
			URL:       opened.URL,
			Title:     "Example",
			RuntimeID: "webContents:3",
		})
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := service.OpenNewTab(context.Background(), "sess_new", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/browser/tabs/open" {
		t.Fatalf("new tab should use one atomic open request, got %v", paths)
	}
	if opened.SessionID != "sess_new" || opened.TabID == "" || opened.URL != "https://example.com" {
		t.Fatalf("unexpected open request: %+v", opened)
	}
	if tab.ID != opened.TabID || tab.URL != opened.URL {
		t.Fatalf("unexpected opened tab: %+v", tab)
	}
}

func TestElectronBridgeServiceRecoverEnsuresWithoutAutomation(t *testing.T) {
	var ensured electronBridgeRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/browser/tabs/ensure" {
			t.Fatalf("recovery must not use the automation open path: %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&ensured); err != nil {
			t.Fatal(err)
		}
		writeElectronBridgeTestJSON(w, electronBridgeSnapshot{
			SessionID: ensured.SessionID,
			TabID:     ensured.TabID,
			Status:    "detached",
			URL:       ensured.URL,
			Title:     "Restored",
			RuntimeID: "webContents:4",
		})
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "bridge-token"})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := service.Recover(context.Background(), "sess_restore", RecoverHint{
		TabID: "tab_restore",
		URL:   "https://restore.example/",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ensured.SessionID != "sess_restore" || ensured.TabID != "tab_restore" || ensured.URL != "https://restore.example/" {
		t.Fatalf("unexpected ensure request: %+v", ensured)
	}
	if ensured.Activate == nil || *ensured.Activate {
		t.Fatalf("recovery must keep the restored tab in the background: %+v", ensured)
	}
	if tab.SessionID != ensured.SessionID || tab.ID != ensured.TabID || tab.URL != ensured.URL {
		t.Fatalf("unexpected restored tab: %+v", tab)
	}
}

func TestElectronBridgeServiceListTabsFiltersLostAndCrossSessionSnapshots(t *testing.T) {
	const token = "bridge-token"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var req electronBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/browser/tabs/list" || req.SessionID != "sess_a" {
			t.Fatalf("unexpected request path=%s req=%+v", r.URL.Path, req)
		}
		writeElectronBridgeTestJSON(w, electronBridgeTabsResponse{Tabs: []electronBridgeSnapshot{
			{SessionID: "sess_a", TabID: "tab_live", Status: "detached", URL: "https://a.example/", Title: "A", RuntimeID: "webContents:1"},
			{SessionID: "sess_a", TabID: "tab_lost", Status: "lost", URL: "https://lost.example/", Title: "Lost", RuntimeID: "webContents:2"},
			{SessionID: "sess_b", TabID: "tab_wrong", Status: "detached", URL: "https://b.example/", Title: "B", RuntimeID: "webContents:3"},
		}})
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	tabs, err := service.ListTabs(context.Background(), "sess_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].ID != "tab_live" || tabs[0].SessionID != "sess_a" {
		t.Fatalf("unexpected filtered tabs: %+v", tabs)
	}
}

func TestElectronBridgeServiceMapsMissingTab(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"browser tab not found"}`))
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	err = service.ReleaseTab(context.Background(), "sess_a", "tab_missing")
	if !errors.Is(err, ErrTabNotFound) {
		t.Fatalf("expected ErrTabNotFound, got %v", err)
	}
}

func TestElectronBridgeServicePreservesStructuredOperationErrors(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		body      string
		code      string
		retryable bool
		cause     error
	}{
		{
			name:   "element failure is not a missing tab",
			status: http.StatusUnprocessableEntity,
			body:   `{"error":"target element not found","code":"element_not_found","retryable":false}`,
			code:   "element_not_found",
		},
		{
			name:   "project file scope is preserved",
			status: http.StatusForbidden,
			body:   `{"error":"file URL is outside the session project","code":"file_url_not_allowed","retryable":false}`,
			code:   "file_url_not_allowed",
			cause:  ErrFileURLNotAllowed,
		},
		{
			name:   "persistent webview limit is preserved",
			status: http.StatusTooManyRequests,
			body:   `{"error":"browser tab limit reached","code":"browser_tab_limit_reached","retryable":false}`,
			code:   "browser_tab_limit_reached",
			cause:  ErrTabLimit,
		},
		{
			name:      "webview readiness is retryable",
			status:    http.StatusServiceUnavailable,
			body:      `{"error":"browser_webview_not_ready","code":"browser_webview_not_ready","retryable":true}`,
			code:      "browser_webview_not_ready",
			retryable: true,
			cause:     ErrUnavailable,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = service.Click(context.Background(), "sess_a", "tab_a", ClickInput{Selector: "#missing"})
			if err == nil || ErrorCode(err) != tt.code || ErrorRetryable(err) != tt.retryable {
				t.Fatalf("unexpected structured error: err=%v code=%q retryable=%v", err, ErrorCode(err), ErrorRetryable(err))
			}
			if tt.cause != nil && !errors.Is(err, tt.cause) {
				t.Fatalf("expected cause %v, got %v", tt.cause, err)
			}
			if tt.cause == nil && errors.Is(err, ErrTabNotFound) {
				t.Fatalf("element failure must not map to ErrTabNotFound: %v", err)
			}
		})
	}
}

func TestElectronBridgeServiceToolEndpoints(t *testing.T) {
	const token = "bridge-token"
	tab := electronBridgeSnapshot{
		SessionID: "sess_a",
		TabID:     "tab_a",
		Status:    "detached",
		URL:       "https://example.com",
		Title:     "Example",
		RuntimeID: "webContents:1",
	}
	paths := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		if req["sessionID"] != "sess_a" || req["tabID"] != "tab_a" {
			t.Fatalf("unexpected request for %s: %+v", r.URL.Path, req)
		}
		paths[r.URL.Path] = true
		switch r.URL.Path {
		case "/browser/tabs/list":
			writeElectronBridgeTestJSON(w, electronBridgeTabsResponse{Tabs: []electronBridgeSnapshot{tab}})
		case "/browser/tabs/observe":
			if req["maxTextChars"] != float64(123) || req["maxElements"] != float64(4) || req["offset"] != float64(100) {
				t.Fatalf("unexpected observe request: %+v", req)
			}
			next := 104
			writeElectronBridgeTestJSON(w, electronBridgeObserveResponse{
				Tab:               tab,
				Title:             "Example",
				URL:               "https://example.com",
				ReadyState:        "complete",
				Viewport:          Viewport{Width: 800, Height: 600, DevicePixelRatio: 2, ScrollY: 120},
				Text:              "hello",
				TextChars:         5,
				Elements:          []ObservedElement{{Index: 0, Ref: "pudding-ref:test:1", Tag: "a", Text: "link", Bounds: Bounds{X: 30, Y: 40, Width: 100, Height: 25}}},
				UnavailableFrames: []string{"https://frame.example.com"},
				TotalElements:     125,
				NextOffset:        &next,
			})
		case "/browser/tabs/screenshot":
			if req["fullPage"] != nil {
				t.Fatalf("unexpected screenshot request: %+v", req)
			}
			writeElectronBridgeTestJSON(w, electronBridgeScreenshotResponse{
				Tab:           tab,
				MIME:          "image/png",
				DataBase64:    "iVBORw0KGgo=",
				Size:          8,
				Width:         1,
				Height:        1,
				CaptureBounds: Bounds{Width: 800, Height: 600},
			})
		case "/browser/tabs/click":
			if req["selector"] != "#go" || req["method"] != "pointer" {
				t.Fatalf("unexpected click request: %+v", req)
			}
			writeElectronBridgeTestJSON(w, electronBridgeActionResponse{
				Tab:    tab,
				Action: "click",
				Result: map[string]any{"ok": true},
			})
		case "/browser/tabs/type":
			if req["selector"] != "#q" || req["text"] != "hello" || req["clear"] != true {
				t.Fatalf("unexpected type request: %+v", req)
			}
			writeElectronBridgeTestJSON(w, electronBridgeActionResponse{
				Tab:    tab,
				Action: "type",
				Result: map[string]any{"textLength": 5},
			})
		case "/browser/tabs/scroll":
			if req["deltaY"] != float64(600) {
				t.Fatalf("unexpected scroll request: %+v", req)
			}
			writeElectronBridgeTestJSON(w, electronBridgeActionResponse{
				Tab:    tab,
				Action: "scroll",
				Result: map[string]any{"ok": true},
			})
		default:
			http.Error(w, `{"error":"browser bridge route not found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := service.Observe(context.Background(), "sess_a", "tab_a", ObserveOptions{MaxTextChars: 123, MaxElements: 4, Offset: 100}); err != nil || got.Text != "hello" || len(got.UnavailableFrames) != 1 || got.UnavailableFrames[0] != "https://frame.example.com" || len(got.Elements) != 1 || got.Viewport.ScrollY != 120 || got.Viewport.DevicePixelRatio != 2 || got.Elements[0].Ref != "pudding-ref:test:1" || got.Elements[0].Bounds.X != 30 || got.TotalElements != 125 || got.NextOffset == nil || *got.NextOffset != 104 {
		t.Fatalf("unexpected observe result: got=%+v err=%v", got, err)
	}
	if got, err := service.Screenshot(context.Background(), "sess_a", "tab_a", ScreenshotOptions{}); err != nil || got.MIME != "image/png" || got.Width != 1 || got.CaptureBounds.Y != 0 {
		t.Fatalf("unexpected screenshot result: got=%+v err=%v", got, err)
	}
	if got, err := service.Click(context.Background(), "sess_a", "tab_a", ClickInput{Selector: "#go", Method: "pointer"}); err != nil || got.Action != "click" {
		t.Fatalf("unexpected click result: got=%+v err=%v", got, err)
	}
	if got, err := service.Type(context.Background(), "sess_a", "tab_a", TypeInput{Selector: "#q", Text: "hello", Clear: true}); err != nil || got.Action != "type" {
		t.Fatalf("unexpected type result: got=%+v err=%v", got, err)
	}
	if got, err := service.Scroll(context.Background(), "sess_a", "tab_a", ScrollInput{}); err != nil || got.Action != "scroll" {
		t.Fatalf("unexpected scroll result: got=%+v err=%v", got, err)
	}
	for _, path := range []string{"/browser/tabs/observe", "/browser/tabs/screenshot", "/browser/tabs/click", "/browser/tabs/type", "/browser/tabs/scroll"} {
		if !paths[path] {
			t.Fatalf("expected request to %s", path)
		}
	}
}

func TestElectronBridgeServiceRevokesFileAccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/browser/session/revoke-file-access" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		var req electronBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		if req.SessionID != "sess_revoke" {
			t.Fatalf("unexpected request: %+v", req)
		}
		writeElectronBridgeTestJSON(w, electronBridgeRevokeFileAccessResponse{ClosedTabIDs: []string{"tab_file"}})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := service.RevokeFileAccess(context.Background(), "sess_revoke")
	if err != nil || len(closed) != 1 || closed[0] != "tab_file" {
		t.Fatalf("closed=%v err=%v", closed, err)
	}
}

func writeElectronBridgeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil && !strings.Contains(err.Error(), "broken pipe") {
		panic(err)
	}
}

func TestElectronBridgeFormActionsPreserveInputsAndObservation(t *testing.T) {
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		bodies = append(bodies, body)
		writeElectronBridgeTestJSON(w, electronBridgeActionResponse{Result: map[string]any{"delivered": true, "effect": "unverified"}})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	value := ""
	if _, err := service.Select(ctx, "session", "tab", SelectInput{Selector: "pudding-ref:select", Value: &value}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Type(ctx, "session", "tab", TypeInput{Selector: "pudding-ref:input", Text: "", Clear: true, Commit: true}); err != nil {
		t.Fatal(err)
	}
	x, y := 10.0, 20.0
	if _, err := service.Click(ctx, "session", "tab", ClickInput{X: &x, Y: &y, ObservationID: "capture"}); err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 3 || bodies[0]["value"] != "" || bodies[0]["label"] != nil || bodies[0]["sessionID"] != "session" || bodies[0]["tabID"] != "tab" || bodies[1]["commit"] != true || bodies[1]["clear"] != true || bodies[2]["observationID"] != "capture" {
		t.Fatalf("lost action fields: %+v", bodies)
	}
	for _, in := range []SelectInput{{Selector: "#s"}, {Selector: "#s", Value: &value, Label: &value}, {Value: &value}} {
		if _, err := service.Select(ctx, "session", "tab", in); err == nil {
			t.Errorf("accepted invalid selection: %+v", in)
		}
	}
	if len(bodies) != 3 {
		t.Fatal("invalid selection reached the bridge")
	}
	observed := (electronBridgeObserveResponse{ObservationID: "observed", CoordinateSpace: "viewport_css_pixels"}).result()
	shot := (electronBridgeScreenshotResponse{ObservationID: "captured"}).result()
	if observed.ObservationID != "observed" || observed.CoordinateSpace != "viewport_css_pixels" || shot.ObservationID != "captured" {
		t.Fatal("lost observation metadata")
	}
}

func TestElectronBridgeKeyboardAndWaitContracts(t *testing.T) {
	var paths []string
	var bodies []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, body)
		if r.URL.Path == "/browser/tabs/wait" && body["text"] == "missing" {
			w.WriteHeader(http.StatusRequestTimeout)
			writeElectronBridgeTestJSON(w, map[string]any{"error": "browser wait timed out: text", "code": "browser_wait_timeout", "retryable": false})
			return
		}
		writeElectronBridgeTestJSON(w, electronBridgeActionResponse{Result: map[string]any{"matched": true, "delivered": true}})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	key, err := service.PressKey(context.Background(), "session", "", KeyInput{TabID: "tab", Selector: "#input", Key: "Tab", Modifiers: []string{"Shift"}})
	if err != nil || key.Action != "press-key" {
		t.Fatalf("key action: %+v %v", key, err)
	}
	timeoutMS := 750
	wait, err := service.Wait(context.Background(), "session", "tab", WaitInput{Condition: "text", Selector: "#result", Text: "Saved", TimeoutMS: &timeoutMS})
	if err != nil || wait.Action != "wait" || wait.Result["matched"] != true {
		t.Fatalf("wait action: %+v %v", wait, err)
	}
	if paths[0] != "/browser/tabs/press-key" || paths[1] != "/browser/tabs/wait" || bodies[0]["sessionID"] != "session" || bodies[0]["tabID"] != "tab" || bodies[0]["key"] != "Tab" || bodies[0]["modifiers"].([]any)[0] != "Shift" || bodies[1]["timeoutMs"] != float64(750) || bodies[1]["text"] != "Saved" {
		t.Fatalf("lost wire fields: %v %+v", paths, bodies)
	}
	_, err = service.Wait(context.Background(), "session", "tab", WaitInput{Condition: "text", Text: "missing"})
	if ErrorCode(err) != "browser_wait_timeout" {
		t.Fatalf("lost timeout classification: %v", err)
	}
}

func TestElectronBridgeCancellationReachesHost(t *testing.T) {
	received := make(chan struct{})
	disconnected := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		close(received)
		select {
		case <-r.Context().Done():
			close(disconnected)
		case <-time.After(3 * time.Second):
			t.Error("bridge request was not cancelled")
		}
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := service.PressKey(ctx, "session", "tab", KeyInput{Key: "Enter"}); result <- err }()
	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("request did not reach host")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not see disconnect")
	}
}

func TestElectronBridgeFrameAndPopupTargets(t *testing.T) {
	var requests []map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request map[string]any
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		request["path"] = r.URL.Path
		requests = append(requests, request)
		writeElectronBridgeTestJSON(w, map[string]any{
			"tab":     electronBridgeSnapshot{SessionID: "session", TabID: "tab"},
			"frameID": "frame", "windowID": "popup", "coordinateSpace": "frame_viewport_css_pixels",
			"frames":  []FrameInfo{{FrameID: "frame", URL: "https://cross.example", Main: false}},
			"windows": []WindowInfo{{WindowID: "popup", URL: "https://popup.example"}},
			"result":  map[string]any{"matched": true},
		})
	}))
	defer server.Close()
	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	observed, err := service.Observe(ctx, "session", "tab", ObserveOptions{FrameID: "frame", WindowID: "popup"})
	if err != nil || observed.FrameID != "frame" || observed.WindowID != "popup" || len(observed.Frames) != 1 || len(observed.Windows) != 1 || observed.CoordinateSpace != "frame_viewport_css_pixels" {
		t.Fatalf("target metadata lost: %+v %v", observed, err)
	}
	actions := []func() error{
		func() error {
			_, e := service.Click(ctx, "session", "tab", ClickInput{FrameID: "frame", WindowID: "popup", Selector: "#go"})
			return e
		},
		func() error {
			_, e := service.Type(ctx, "session", "tab", TypeInput{FrameID: "frame", WindowID: "popup", Selector: "#q", Text: "hello"})
			return e
		},
		func() error {
			value := "yes"
			_, e := service.Select(ctx, "session", "tab", SelectInput{FrameID: "frame", WindowID: "popup", Selector: "select", Value: &value})
			return e
		},
		func() error {
			_, e := service.Scroll(ctx, "session", "tab", ScrollInput{FrameID: "frame", WindowID: "popup", DeltaY: 50})
			return e
		},
		func() error {
			_, e := service.PressKey(ctx, "session", "tab", KeyInput{FrameID: "frame", WindowID: "popup", Key: "Enter"})
			return e
		},
		func() error {
			_, e := service.Wait(ctx, "session", "tab", WaitInput{FrameID: "frame", WindowID: "popup", Condition: "text", Text: "done"})
			return e
		},
	}
	for _, action := range actions {
		if err := action(); err != nil {
			t.Fatal(err)
		}
	}
	for _, request := range requests {
		if request["frameID"] != "frame" || request["windowID"] != "popup" || request["sessionID"] != "session" || request["tabID"] != "tab" {
			t.Fatalf("target lost: %+v", request)
		}
	}
	shot, err := service.Screenshot(ctx, "session", "tab", ScreenshotOptions{WindowID: "popup"})
	if err != nil || shot.WindowID != "popup" {
		t.Fatalf("screenshot target lost: %+v %v", shot, err)
	}
	if err := service.CloseWindow(ctx, "session", "tab", "popup"); err != nil {
		t.Fatal(err)
	}
	for _, request := range requests[len(requests)-2:] {
		if request["windowID"] != "popup" || request["frameID"] != nil {
			t.Fatalf("unexpected window target: %+v", request)
		}
	}
	if err := service.CloseWindow(ctx, "session", "tab", ""); err == nil {
		t.Fatal("missing popup id must not close the parent")
	}
}

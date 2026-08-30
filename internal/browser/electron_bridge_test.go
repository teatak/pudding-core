package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
			if req["maxTextChars"] != float64(123) || req["maxElements"] != float64(4) {
				t.Fatalf("unexpected observe request: %+v", req)
			}
			writeElectronBridgeTestJSON(w, electronBridgeObserveResponse{
				Tab:        tab,
				Title:      "Example",
				URL:        "https://example.com",
				ReadyState: "complete",
				Text:       "hello",
				TextChars:  5,
				Elements:   []ObservedElement{{Index: 0, Tag: "a", Text: "link"}},
			})
		case "/browser/tabs/screenshot":
			if req["fullPage"] != true {
				t.Fatalf("unexpected screenshot request: %+v", req)
			}
			writeElectronBridgeTestJSON(w, electronBridgeScreenshotResponse{
				Tab:        tab,
				MIME:       "image/png",
				DataBase64: "iVBORw0KGgo=",
				Size:       8,
				Width:      1,
				Height:     1,
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
	if got, err := service.Observe(context.Background(), "sess_a", "tab_a", ObserveOptions{MaxTextChars: 123, MaxElements: 4}); err != nil || got.Text != "hello" || len(got.Elements) != 1 {
		t.Fatalf("unexpected observe result: got=%+v err=%v", got, err)
	}
	if got, err := service.Screenshot(context.Background(), "sess_a", "tab_a", ScreenshotOptions{FullPage: true}); err != nil || got.MIME != "image/png" || got.Width != 1 {
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

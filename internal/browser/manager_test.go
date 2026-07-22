package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestNormalizeURLDefaultsHTTPS(t *testing.T) {
	got, err := normalizeURL("example.com/path")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com/path" {
		t.Fatalf("url = %q", got)
	}
}

func TestNormalizeURLDefaultsLocalhostToHTTP(t *testing.T) {
	got, err := normalizeURL("localhost:5173")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://localhost:5173" {
		t.Fatalf("url = %q", got)
	}
}

func TestNormalizeURLRejectsUnsupportedSchemes(t *testing.T) {
	if _, err := normalizeURL("file:///tmp/demo.html"); err == nil {
		t.Fatal("file URL should be rejected")
	}
}

func TestManagerPersistentTabLimits(t *testing.T) {
	manager := NewManager(Config{})
	for i := 0; i < maxTabsPerSession; i++ {
		id := "tab_" + strconv.Itoa(i)
		manager.tabs[id] = &tabBinding{id: id, sessionID: "session_limit", commandMu: &sync.Mutex{}}
		if manager.sessions["session_limit"] == nil {
			manager.sessions["session_limit"] = map[string]bool{}
		}
		manager.sessions["session_limit"][id] = true
	}
	if manager.canCreateTab("session_limit") {
		t.Fatal("session tab limit should reject another tab")
	}
	if !manager.canCreateTab("session_other") {
		t.Fatal("global capacity should still allow another session")
	}
}

func TestManagerRevokesCachedFileAccess(t *testing.T) {
	manager := NewManager(Config{})
	fileBinding := &tabBinding{id: "tab_file", sessionID: "session_revoke", url: "file:///project/index.html", fileRoots: []string{"/project"}, commandMu: &sync.Mutex{}}
	webBinding := &tabBinding{id: "tab_web", sessionID: "session_revoke", url: "https://example.com/", fileRoots: []string{"/project"}, commandMu: &sync.Mutex{}}
	manager.tabs[fileBinding.id] = fileBinding
	manager.tabs[webBinding.id] = webBinding
	manager.sessions["session_revoke"] = map[string]bool{fileBinding.id: true, webBinding.id: true}

	closed, err := manager.RevokeFileAccess(context.Background(), "session_revoke")
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0] != fileBinding.id {
		t.Fatalf("closed tabs = %v", closed)
	}
	if manager.tabs[fileBinding.id] != nil {
		t.Fatal("file tab should be removed")
	}
	if got := manager.tabs[webBinding.id]; got == nil || len(got.fileRoots) != 0 {
		t.Fatalf("web tab grant was not cleared: %+v", got)
	}
	if !errors.Is(validatePageURL("file:///project/index.html", nil), ErrFileURLNotAllowed) {
		t.Fatal("history navigation must reject revoked file URLs")
	}
}

func TestAttachExistingDevToolsPort(t *testing.T) {
	devtools := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/version" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Browser":"Chrome/test"}`))
	}))
	defer devtools.Close()
	parsed, err := url.Parse(devtools.URL)
	if err != nil {
		t.Fatal(err)
	}
	port := parsed.Port()
	if port == "" || !strings.HasPrefix(parsed.Host, "127.0.0.1:") {
		t.Fatalf("test server must use loopback port, got %s", devtools.URL)
	}
	home := t.TempDir()
	profile := filepath.Join(home, "browser-profiles", "default")
	if err := os.MkdirAll(profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "DevToolsActivePort"), []byte(port+"\n/devtools/browser/test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	proc, err := attachExisting(context.Background(), Config{HomeDir: home}, "sess_attach", devtools.Client())
	if err != nil {
		t.Fatal(err)
	}
	if proc.cmd != nil || proc.port == 0 || proc.endpoint != "http://127.0.0.1:"+port {
		t.Fatalf("unexpected proc: %+v", proc)
	}
}

func TestProfileDirIsGlobal(t *testing.T) {
	home := t.TempDir()
	got, err := profileDir(Config{HomeDir: home}, "sess/a b")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "browser-profiles", "default")
	if got != want {
		t.Fatalf("profile dir = %q, want %q", got, want)
	}
}

func TestSingletonLockPID(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink("MacBookPro.lan-62668", filepath.Join(dir, "SingletonLock")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	pid, err := singletonLockPID(dir)
	if err != nil {
		t.Fatal(err)
	}
	if pid != 62668 {
		t.Fatalf("pid = %d", pid)
	}
}

func TestTypeUsesTargetScopedCDPInput(t *testing.T) {
	evaluateCount := 0
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			evaluateCount++
			if evaluateCount == 1 {
				return jsonValueReply(`{"ok":true,"tag":"input","expectedValueLength":1,"expectedValueHash":"12345678"}`)
			}
			return jsonValueReply(`{"ok":true,"tag":"input","textLength":1,"valueLength":1,"matchesExpected":true,"cursorX":10,"cursorY":10,"method":"target"}`)
		case "Page.getNavigationHistory":
			return navigationHistoryReply()
		default:
			t.Errorf("unexpected CDP method: %s", request.Method)
			return cdpTestReply{}
		}
	})
	result, err := manager.Type(context.Background(), "sess_type", "tab_type", TypeInput{Selector: "#name", Text: "P", Clear: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["method"] != "target" || result.Result["valueLength"] != float64(1) {
		t.Fatalf("unexpected type result: %+v", result.Result)
	}
	if _, ok := result.Result["matchesExpected"]; ok {
		t.Fatalf("internal verification field leaked into result: %+v", result.Result)
	}
	gotCalls := calls()
	if got := strings.Join(cdpTestMethods(gotCalls), ","); got != "Runtime.evaluate,Runtime.evaluate,Runtime.evaluate,Runtime.evaluate,Runtime.evaluate,Page.getNavigationHistory" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
	proc := manager.processes[globalProcessKey]
	proc.cdpMu.Lock()
	session := proc.cdp["target"]
	proc.cdpMu.Unlock()
	if session == nil {
		t.Fatal("persistent CDP session was not retained")
	}
	session.mu.Lock()
	nextID, connected := session.nextID, session.conn != nil
	session.mu.Unlock()
	if !connected || nextID != len(gotCalls) {
		t.Fatalf("CDP session was not reused: connected=%v nextID=%d calls=%d", connected, nextID, len(gotCalls))
	}
	if expression, _ := gotCalls[2].Params["expression"].(string); !strings.Contains(expression, `inputEvent("beforeinput", true)`) || !strings.Contains(expression, `setter.call(el, nextValue)`) || !strings.Contains(expression, `if (inserted && !sawInput) dispatchInput()`) {
		t.Fatalf("unexpected target input script: %+v", gotCalls[2].Params)
	}
	if expression, _ := gotCalls[1].Params["expression"].(string); !strings.Contains(expression, `if (!clear) range.collapse(false)`) {
		t.Fatalf("unexpected contenteditable selection script: %+v", gotCalls[1].Params)
	}
	if expression, _ := gotCalls[1].Params["expression"].(string); !strings.Contains(expression, `replace(/\uFEFF/g, "")`) {
		t.Fatalf("contenteditable expectation did not normalize Slate placeholders: %+v", gotCalls[1].Params)
	}
	if expression, _ := gotCalls[3].Params["expression"].(string); !strings.Contains(expression, `replace(/\uFEFF/g, "")`) {
		t.Fatalf("contenteditable result did not normalize Slate placeholders: %+v", gotCalls[3].Params)
	}
}

func TestTypeTargetInputFailureDoesNotFallBack(t *testing.T) {
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			expression, _ := request.Params["expression"].(string)
			if strings.Contains(expression, "setter.call(el, nextValue)") {
				return cdpTestReply{ErrorMessage: "target input failed"}
			}
			return jsonValueReply(`{"ok":true,"tag":"input","expectedValueLength":7,"expectedValueHash":"12345678"}`)
		default:
			t.Errorf("unexpected CDP method after input failure: %s", request.Method)
			return cdpTestReply{}
		}
	})
	_, err := manager.Type(context.Background(), "sess_type", "tab_type", TypeInput{Selector: "#name", Text: "Pudding", Clear: true})
	if err == nil || !strings.Contains(err.Error(), "browser target input failed") {
		t.Fatalf("expected direct CDP input error, got %v", err)
	}
	if got := strings.Join(cdpTestMethods(calls()), ","); got != "Runtime.evaluate,Runtime.evaluate,Runtime.evaluate" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

func TestTypeRejectsUnchangedControlledValue(t *testing.T) {
	evaluateCount := 0
	manager, _ := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			evaluateCount++
			if evaluateCount == 1 {
				return jsonValueReply(`{"ok":true,"tag":"input","expectedValueLength":1,"expectedValueHash":"12345678"}`)
			}
			return jsonValueReply(`{"ok":true,"tag":"input","valueLength":1,"matchesExpected":false,"method":"target"}`)
		default:
			t.Errorf("unexpected CDP method: %s", request.Method)
			return cdpTestReply{}
		}
	})

	_, err := manager.Type(context.Background(), "sess_type", "tab_type", TypeInput{Selector: "#name", Text: "P"})
	if err == nil || !strings.Contains(err.Error(), "did not produce the expected value") {
		t.Fatalf("expected controlled input verification error, got %v", err)
	}
}

func TestClickCDPFailureDoesNotFallBackToDOM(t *testing.T) {
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			expression, _ := request.Params["expression"].(string)
			if strings.Contains(expression, "elementFromPoint") {
				if strings.Contains(expression, "el.click()") || strings.Contains(expression, "focusTarget.focus") {
					t.Errorf("click target resolution dispatched a synthetic click: %+v", request.Params)
				}
				if _, ok := request.Params["userGesture"]; ok {
					t.Errorf("click target resolution unexpectedly carried a user gesture: %+v", request.Params)
				}
				return jsonValueReply(`{"ok":true,"tag":"button","x":20,"y":30,"method":"pointer"}`)
			}
			return jsonValueReply(`{"url":"https://example.test/","readyState":"complete","timeOrigin":1}`)
		case "Input.dispatchMouseEvent":
			if request.Params["type"] == "mousePressed" {
				return cdpTestReply{ErrorMessage: "pointer click failed"}
			}
			return cdpTestReply{}
		default:
			t.Errorf("unexpected CDP method after click failure: %s", request.Method)
			return cdpTestReply{}
		}
	})
	_, err := manager.Click(context.Background(), "sess_type", "tab_type", ClickInput{Selector: "#save"})
	if err == nil || !strings.Contains(err.Error(), "pointer click failed") {
		t.Fatalf("expected direct CDP click error, got %v", err)
	}
	if got := strings.Join(cdpTestMethods(calls()), ","); got != "Runtime.evaluate,Runtime.evaluate,Input.dispatchMouseEvent,Input.dispatchMouseEvent" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

func TestNavigateAndWaitRequiresACommittedDocumentTransition(t *testing.T) {
	evaluateCount := 0
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			evaluateCount++
			if evaluateCount < 3 {
				return jsonValueReply(`{"url":"https://example.test/","readyState":"complete","timeOrigin":1}`)
			}
			return jsonValueReply(`{"url":"https://example.test/","readyState":"complete","timeOrigin":2}`)
		case "Page.reload":
			return cdpTestReply{}
		default:
			t.Errorf("unexpected CDP method: %s", request.Method)
			return cdpTestReply{}
		}
	})

	proc := manager.processes[globalProcessKey]
	if err := proc.navigateAndWait(context.Background(), manager.client, "target", "Page.reload", map[string]any{"ignoreCache": false}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cdpTestMethods(calls()), ","); got != "Runtime.evaluate,Page.reload,Runtime.evaluate,Runtime.evaluate" {
		t.Fatalf("navigation returned before a document transition: %s", got)
	}
}

func TestNavigateAndWaitRejectsPageNavigateFailure(t *testing.T) {
	manager, _ := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			return jsonValueReply(`{"url":"about:blank","readyState":"complete","timeOrigin":1}`)
		case "Page.navigate":
			return cdpTestReply{Result: map[string]any{"errorText": "net::ERR_NAME_NOT_RESOLVED"}}
		default:
			t.Errorf("unexpected CDP method: %s", request.Method)
			return cdpTestReply{}
		}
	})

	proc := manager.processes[globalProcessKey]
	err := proc.navigateAndWait(context.Background(), manager.client, "target", "Page.navigate", map[string]any{"url": "https://bad.example/"})
	if err == nil || !strings.Contains(err.Error(), "ERR_NAME_NOT_RESOLVED") {
		t.Fatalf("expected Page.navigate failure, got %v", err)
	}
}

func TestScrollUsesTargetScopedCDP(t *testing.T) {
	evaluateCount := 0
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Runtime.evaluate":
			evaluateCount++
			if evaluateCount == 1 {
				return jsonValueReply(`{"ok":true,"x":400,"y":300}`)
			}
			return jsonValueReply(`{"ok":true,"x":0,"y":600,"targetX":0,"targetY":600,"cursorX":400,"cursorY":300,"method":"target"}`)
		case "Page.getNavigationHistory":
			return navigationHistoryReply()
		default:
			t.Errorf("unexpected CDP method: %s", request.Method)
			return cdpTestReply{}
		}
	})
	result, err := manager.Scroll(context.Background(), "sess_type", "tab_type", ScrollInput{DeltaY: 600})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["method"] != "target" || result.Result["y"] != float64(600) {
		t.Fatalf("unexpected scroll result: %+v", result.Result)
	}
	gotCalls := calls()
	if got := strings.Join(cdpTestMethods(gotCalls), ","); got != "Runtime.evaluate,Runtime.evaluate,Runtime.evaluate,Runtime.evaluate,Page.getNavigationHistory" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
	if expression, _ := gotCalls[1].Params["expression"].(string); !strings.Contains(expression, "target.scrollBy") {
		t.Fatalf("unexpected target scroll script: %+v", gotCalls[1].Params)
	}
}

func TestScreenshotUsesCDPPageCapture(t *testing.T) {
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Page.getLayoutMetrics":
			return cdpTestReply{Result: map[string]any{
				"cssVisualViewport": map[string]any{"clientWidth": 1, "clientHeight": 1},
				"cssContentSize":    map[string]any{"x": 0, "y": 0, "width": 1, "height": 1},
			}}
		case "Page.captureScreenshot":
			clip, _ := request.Params["clip"].(map[string]any)
			if clip["width"] != float64(1) || request.Params["captureBeyondViewport"] != true {
				t.Errorf("unexpected screenshot params: %+v", request.Params)
			}
			return cdpTestReply{Result: map[string]any{"data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}}
		case "Page.getNavigationHistory":
			return navigationHistoryReply()
		default:
			t.Errorf("unexpected CDP method: %s", request.Method)
			return cdpTestReply{}
		}
	})
	result, err := manager.Screenshot(context.Background(), "sess_type", "tab_type", ScreenshotOptions{FullPage: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Width != 1 || result.Height != 1 || result.ViewportWidth != 1 || result.ViewportHeight != 1 {
		t.Fatalf("unexpected screenshot dimensions: %+v", result)
	}
	if got := strings.Join(cdpTestMethods(calls()), ","); got != "Runtime.evaluate,Page.getLayoutMetrics,Page.captureScreenshot,Runtime.evaluate,Page.getNavigationHistory" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

func TestFullPageScreenshotRejectsOversizedContentBeforeCapture(t *testing.T) {
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Page.getLayoutMetrics":
			return cdpTestReply{Result: map[string]any{
				"cssVisualViewport": map[string]any{"clientWidth": 1280, "clientHeight": 720},
				"cssContentSize":    map[string]any{"x": 0, "y": 0, "width": 20000, "height": 20000},
			}}
		default:
			t.Errorf("unexpected CDP method after oversized metrics: %s", request.Method)
			return cdpTestReply{}
		}
	})
	_, err := manager.Screenshot(context.Background(), "sess_type", "tab_type", ScreenshotOptions{FullPage: true})
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("expected screenshot size limit error, got %v", err)
	}
	if got := strings.Join(cdpTestMethods(calls()), ","); got != "Runtime.evaluate,Page.getLayoutMetrics" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

func TestScreenshotRejectsInvalidPNG(t *testing.T) {
	manager, calls := newCDPTestManager(t, func(request cdpTestRequest) cdpTestReply {
		switch request.Method {
		case "Page.getLayoutMetrics":
			return cdpTestReply{Result: map[string]any{
				"cssVisualViewport": map[string]any{"clientWidth": 1280, "clientHeight": 720},
			}}
		case "Page.captureScreenshot":
			return cdpTestReply{Result: map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("not a png"))}}
		default:
			t.Errorf("unexpected CDP method after invalid screenshot: %s", request.Method)
			return cdpTestReply{}
		}
	})
	_, err := manager.Screenshot(context.Background(), "sess_type", "tab_type", ScreenshotOptions{})
	if err == nil || !strings.Contains(err.Error(), "invalid png") {
		t.Fatalf("expected invalid PNG error, got %v", err)
	}
	if got := strings.Join(cdpTestMethods(calls()), ","); got != "Runtime.evaluate,Page.getLayoutMetrics,Page.captureScreenshot" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

type cdpTestRequest struct {
	Method string
	Params map[string]any
}

type cdpTestReply struct {
	Result       any
	ErrorMessage string
}

func newCDPTestManager(t *testing.T, responder func(cdpTestRequest) cdpTestReply) (*Manager, func() []cdpTestRequest) {
	t.Helper()
	var server *httptest.Server
	var mu sync.Mutex
	var calls []cdpTestRequest
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"Browser":"HeadlessChrome/test"}`))
		case "/json/list":
			wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/devtools/page/target"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode([]targetInfo{{
				ID:                   "target",
				Type:                 "page",
				URL:                  "https://example.test/",
				Title:                "Example",
				WebSocketDebuggerURL: wsURL,
			}})
		case "/devtools/page/target":
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Errorf("accept websocket: %v", err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "")
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			for {
				_, data, err := conn.Read(ctx)
				if err != nil {
					return
				}
				var request struct {
					ID     int            `json:"id"`
					Method string         `json:"method"`
					Params map[string]any `json:"params"`
				}
				if err := json.Unmarshal(data, &request); err != nil {
					t.Errorf("decode websocket request: %v", err)
					return
				}
				call := cdpTestRequest{Method: request.Method, Params: request.Params}
				mu.Lock()
				calls = append(calls, call)
				mu.Unlock()
				var reply cdpTestReply
				if isPageMetadataCall(call) {
					reply = jsonValueReply(`{"url":"https://example.test/","title":"Example","faviconURL":""}`)
				} else {
					reply = responder(call)
				}
				response := map[string]any{"id": request.ID, "result": reply.Result}
				if reply.Result == nil {
					response["result"] = map[string]any{}
				}
				if reply.ErrorMessage != "" {
					delete(response, "result")
					response["error"] = map[string]any{"code": -32000, "message": reply.ErrorMessage}
				}
				encoded, _ := json.Marshal(response)
				if err := conn.Write(ctx, websocket.MessageText, encoded); err != nil {
					return
				}
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	manager := NewManager(Config{HomeDir: t.TempDir(), Headless: true})
	manager.client = server.Client()
	now := time.Now().UTC()
	manager.processes[globalProcessKey] = &browserProcess{endpoint: server.URL, headless: true}
	manager.tabs["tab_type"] = &tabBinding{
		commandMu: &sync.Mutex{}, id: "tab_type", sessionID: "sess_type", targetID: "target", url: "https://example.test/", title: "Example", createdAt: now, updatedAt: now,
	}
	manager.sessions["sess_type"] = map[string]bool{"tab_type": true}
	return manager, func() []cdpTestRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]cdpTestRequest(nil), calls...)
	}
}

func jsonValueReply(value string) cdpTestReply {
	return cdpTestReply{Result: map[string]any{"result": map[string]any{"type": "string", "value": value}}}
}

func navigationHistoryReply() cdpTestReply {
	return cdpTestReply{Result: map[string]any{
		"currentIndex": 0,
		"entries":      []map[string]any{{"id": 1, "url": "https://example.test/", "title": "Example"}},
	}}
}

func cdpTestMethods(calls []cdpTestRequest) []string {
	methods := make([]string, 0, len(calls))
	for _, call := range calls {
		methods = append(methods, call.Method)
	}
	return methods
}

func isPageMetadataCall(call cdpTestRequest) bool {
	if call.Method != "Runtime.evaluate" {
		return false
	}
	expression, _ := call.Params["expression"].(string)
	return strings.Contains(expression, "faviconURL") && strings.Contains(expression, "document.title")
}

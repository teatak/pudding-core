package browser

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

func TestTypeUsesCDPInsertText(t *testing.T) {
	manager, calls := newTypeTestManager(t, false)
	result, err := manager.Type(context.Background(), "sess_type", "tab_type", TypeInput{Selector: "#name", Text: "Pudding", Clear: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["method"] != "keyboard" || result.Result["valueLength"] != float64(7) {
		t.Fatalf("unexpected type result: %+v", result.Result)
	}
	if got := strings.Join(calls(), ","); got != "Runtime.evaluate,Input.insertText,Runtime.evaluate,Page.getNavigationHistory" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

func TestTypeFallsBackToReactCompatibleDOMSetter(t *testing.T) {
	manager, calls := newTypeTestManager(t, true)
	result, err := manager.Type(context.Background(), "sess_type", "tab_type", TypeInput{Selector: "#name", Text: "Pudding", Clear: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["method"] != "dom" || result.Result["valueLength"] != float64(7) {
		t.Fatalf("unexpected fallback result: %+v", result.Result)
	}
	if got := strings.Join(calls(), ","); got != "Runtime.evaluate,Input.insertText,Runtime.evaluate,Page.getNavigationHistory" {
		t.Fatalf("unexpected CDP calls: %s", got)
	}
}

func newTypeTestManager(t *testing.T, failKeyboard bool) (*Manager, func() []string) {
	t.Helper()
	var server *httptest.Server
	var mu sync.Mutex
	var calls []string
	evaluateCount := 0
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
			_, data, err := conn.Read(ctx)
			if err != nil {
				t.Errorf("read websocket: %v", err)
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
			mu.Lock()
			calls = append(calls, request.Method)
			if request.Method == "Runtime.evaluate" {
				evaluateCount++
			}
			currentEvaluate := evaluateCount
			mu.Unlock()
			response := map[string]any{"id": request.ID, "result": map[string]any{}}
			switch request.Method {
			case "Runtime.evaluate":
				value := `{"ok":true,"tag":"input"}`
				if currentEvaluate > 1 {
					method := "keyboard"
					if failKeyboard {
						method = "dom"
					}
					value = `{"ok":true,"tag":"input","textLength":7,"valueLength":7,"cursorX":10,"cursorY":10,"method":"` + method + `"}`
				}
				response["result"] = map[string]any{"result": map[string]any{"type": "string", "value": value}}
			case "Input.insertText":
				if request.Params["text"] != "Pudding" {
					t.Errorf("unexpected inserted text: %+v", request.Params)
				}
				if failKeyboard {
					delete(response, "result")
					response["error"] = map[string]any{"code": -32601, "message": "Input.insertText unsupported"}
				}
			case "Page.getNavigationHistory":
				response["result"] = map[string]any{"currentIndex": 0, "entries": []map[string]any{{"id": 1, "url": "https://example.test/", "title": "Example"}}}
			default:
				t.Errorf("unexpected CDP method: %s", request.Method)
			}
			encoded, _ := json.Marshal(response)
			if err := conn.Write(ctx, websocket.MessageText, encoded); err != nil {
				t.Errorf("write websocket: %v", err)
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
		id: "tab_type", sessionID: "sess_type", targetID: "target", url: "https://example.test/", title: "Example", createdAt: now, updatedAt: now,
	}
	manager.sessions["sess_type"] = map[string]bool{"tab_type": true}
	return manager, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), calls...)
	}
}

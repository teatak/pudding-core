package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func browserToolFixture(t *testing.T) (*httptest.Server, store.Store, *fakeBrowserService, *tool.BuiltinRunner) {
	t.Helper()
	st := storetest.New(t)
	service := &fakeBrowserService{tabs: map[string]browser.TabSnapshot{}}
	runner := tool.NewBuiltinRunner(tool.WithBrowser(service), tool.WithBrowserState(st))
	hub := event.NewHub()
	eng := engine.New(st, hub, registry.Static(mock.New()), st, engine.WithTools(runner))
	s := New(eng, st, st, hub).WithBrowser(service)
	runner.SetBrowserLifecycle(s)
	srv := httptest.NewServer(s.Handler(testToken, nil))
	t.Cleanup(srv.Close)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "lifecycle", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	return srv, st, service, runner
}

func browserToolCall(runner *tool.BuiltinRunner, name, args string) tool.Result {
	return runner.Call(context.Background(), tool.Call{SessionID: "lifecycle", Name: name, Args: json.RawMessage(args)})
}

func TestBrowserToolOpenPublishesGateBeforeEarlySingleTabRequests(t *testing.T) {
	for _, endpoint := range []string{"sync", "get", "observe", "tabs", "state"} {
		t.Run(endpoint, func(t *testing.T) {
			srv, _, service, runner := browserToolFixture(t)
			closed := req(t, http.MethodPost, srv.URL+"/sessions/lifecycle/browser/close", nil)
			closed.Body.Close()
			if closed.StatusCode != http.StatusNoContent {
				t.Fatalf("close=%d", closed.StatusCode)
			}
			started, proceed := make(chan browser.TabSnapshot, 1), make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(proceed) }) }
			t.Cleanup(resume)
			listed := make(chan struct{})
			var listedOnce sync.Once
			service.openHook = func(tab browser.TabSnapshot) {
				pending := tab
				pending.URL = "about:blank"
				service.tabs[tab.ID] = pending
				service.listTabsHook = func() { listedOnce.Do(func() { close(listed) }) }
				started <- tab
				<-proceed
				service.tabs[tab.ID] = tab
			}
			toolDone := make(chan tool.Result, 1)
			go func() { toolDone <- browserToolCall(runner, tool.BrowserOpen, `{"url":"https://tool.example/"}`) }()
			var tab browser.TabSnapshot
			select {
			case tab = <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("open did not start")
			}
			method, path := http.MethodPost, "/sessions/lifecycle/browser/tabs/"+tab.ID+"/"+endpoint
			if endpoint == "get" {
				method = http.MethodGet
				path = "/sessions/lifecycle/browser/tabs/" + tab.ID
			}
			if endpoint == "tabs" || endpoint == "state" {
				method = http.MethodGet
				path = "/sessions/lifecycle/browser/" + endpoint
			}
			request := httptest.NewRequest(method, path, strings.NewReader(`{"url":"https://tool.example/","maxElements":20}`))
			request.Header.Set("Authorization", "Bearer "+testToken)
			recorder, done := httptest.NewRecorder(), make(chan struct{})
			go func() { defer close(done); srv.Config.Handler.ServeHTTP(recorder, request) }()
			if endpoint == "tabs" || endpoint == "state" {
				waitBrowserTestSignal(t, listed, "pre-commit summary snapshot")
			}
			select {
			case <-done:
				t.Fatalf("request did not wait for tool commit: %d", recorder.Code)
			case <-time.After(30 * time.Millisecond):
			}
			resume()
			if result := <-toolDone; !result.Ok {
				t.Fatalf("tool open failed: %+v", result)
			}
			waitBrowserTestSignal(t, done, "early request")
			if recorder.Code != http.StatusOK {
				t.Fatalf("early %s=%d body=%s", endpoint, recorder.Code, recorder.Body.String())
			}
			if endpoint == "tabs" || endpoint == "state" {
				if !strings.Contains(recorder.Body.String(), tab.ID) || !strings.Contains(recorder.Body.String(), tab.URL) {
					t.Fatalf("summary hid committed tab: %s", recorder.Body.String())
				}
			}
		})
	}
}

func TestBrowserToolCloseInvalidatesInFlightHTTPSync(t *testing.T) {
	for _, sessionClose := range []bool{false, true} {
		for _, reopen := range []bool{false, true} {
			name := "tab"
			if sessionClose {
				name = "session"
			}
			if reopen {
				name += "-reopen"
			}
			t.Run(name, func(t *testing.T) {
				srv, st, _, runner := browserToolFixture(t)
				if result := browserToolCall(runner, tool.BrowserOpen, `{"url":"https://old.example/","tabID":"tab"}`); !result.Ok {
					t.Fatal(result)
				}
				body := &blockedBrowserSyncBody{reader: strings.NewReader(`{"url":"https://late.example/","historyVisit":true}`), entered: make(chan struct{}), resume: make(chan struct{})}
				var once sync.Once
				resume := func() { once.Do(func() { close(body.resume) }) }
				t.Cleanup(resume)
				request := httptest.NewRequest(http.MethodPost, "/sessions/lifecycle/browser/tabs/tab/sync", body)
				request.Header.Set("Authorization", "Bearer "+testToken)
				recorder, done := httptest.NewRecorder(), make(chan struct{})
				go func() { defer close(done); srv.Config.Handler.ServeHTTP(recorder, request) }()
				waitBrowserTestSignal(t, body.entered, "sync decode")
				args := `{"tabID":"tab"}`
				if sessionClose {
					args = `{}`
				}
				if result := browserToolCall(runner, tool.BrowserClose, args); !result.Ok {
					t.Fatal(result)
				}
				if reopen {
					if result := browserToolCall(runner, tool.BrowserOpen, `{"url":"https://fresh.example/","tabID":"tab"}`); !result.Ok {
						t.Fatal(result)
					}
				}
				resume()
				waitBrowserTestSignal(t, done, "old sync")
				if recorder.Code != http.StatusNotFound {
					t.Fatalf("stale sync=%d", recorder.Code)
				}
				state, err := st.GetBrowserTabState(context.Background(), "lifecycle", "tab")
				if reopen {
					if err != nil || state.URL != "https://fresh.example/" {
						t.Fatalf("new state overwritten: %+v %v", state, err)
					}
				} else if !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("closed state restored: %+v %v", state, err)
				}
			})
		}
	}
}

func TestBrowserOpenWaitWakesOnAnyCommitAndHonorsCancellation(t *testing.T) {
	st := storetest.New(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	s := &Server{store: st}
	s.closeBrowserTabGate("s")
	_, slow := s.BeginBrowserOpen(context.Background(), "s")
	defer slow()
	ctx, fast := s.BeginBrowserOpen(context.Background(), "s")
	defer fast()
	ready := make(chan struct{})
	go func() { defer close(ready); _ = s.awaitBrowserToolOpen(context.Background(), "s", "new") }()
	if err := s.BrowserTabOpened(ctx, "s", browser.TabSnapshot{SessionID: "s", ID: "new", URL: "https://new.example/"}); err != nil {
		t.Fatal(err)
	}
	fast()
	waitBrowserTestSignal(t, ready, "fast open publication")
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.awaitBrowserToolOpen(cancelCtx, "s", "missing"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	oldCtx, finish := s.BeginBrowserOpen(context.Background(), "s")
	defer finish()
	if err := s.BrowserSessionClosed(context.Background(), "s", nil); err != nil {
		t.Fatal(err)
	}
	if err := s.BrowserTabOpened(oldCtx, "s", browser.TabSnapshot{SessionID: "s", ID: "old", URL: "https://old.example/"}); !errors.Is(err, browser.ErrTabNotFound) {
		t.Fatalf("late open revived closed session: %v", err)
	}
	oldTabCtx, finishTab := s.BeginBrowserOpen(context.Background(), "s")
	defer finishTab()
	if err := s.BrowserTabClosed(context.Background(), "s", "old-tab"); err != nil {
		t.Fatal(err)
	}
	if err := s.BrowserTabOpened(oldTabCtx, "s", browser.TabSnapshot{SessionID: "s", ID: "old-tab", URL: "https://old.example/"}); !errors.Is(err, browser.ErrTabNotFound) {
		t.Fatalf("late open revived closed tab: %v", err)
	}
}

func TestRejectedBrowserToolOpenDiscardsOnlyItsNativeOpen(t *testing.T) {
	for _, kind := range []string{"session", "tab", "new-tab", "reopened", "cleanup-failed"} {
		t.Run(kind, func(t *testing.T) {
			srv, st, service, runner := browserToolFixture(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			service.beforeOpenHook = func() {
				method, path := http.MethodPost, "/sessions/lifecycle/browser/close"
				if kind == "tab" {
					method, path = http.MethodPost, "/sessions/lifecycle/browser/tabs/tab/release"
				}
				response := req(t, method, srv.URL+path, nil)
				response.Body.Close()
				if response.StatusCode != http.StatusNoContent {
					t.Fatalf("close=%d", response.StatusCode)
				}
				cancel() // Cleanup must not inherit the cancelled open's deadline.
			}
			var discarded browser.TabSnapshot
			service.discardOpenHook = func(cleanupCtx context.Context, tab browser.TabSnapshot) error {
				discarded = tab
				deadline, ok := cleanupCtx.Deadline()
				if cleanupCtx.Err() != nil || !ok || time.Until(deadline) <= 0 || time.Until(deadline) > 2*time.Second {
					t.Fatalf("invalid independent cleanup budget: %v %v", deadline, cleanupCtx.Err())
				}
				if kind == "reopened" {
					// A later native open reaches the host before rollback, but its
					// Core commit has not yet made the ID visible again.
					current := service.tabs[tab.ID]
					current.OpenID, current.URL = "newer-open", "https://fresh.example/"
					service.tabs[tab.ID] = current
				}
				if kind == "cleanup-failed" {
					return errors.New("native cleanup unavailable")
				}
				return nil
			}
			args := `{"tabID":"tab","url":"https://old.example/"}`
			if kind == "new-tab" {
				args = `{"newTab":true,"url":"https://old.example/"}`
			}
			result := runner.Call(ctx, tool.Call{SessionID: "lifecycle", Name: tool.BrowserOpen, Args: json.RawMessage(args)})
			if result.Ok || discarded.ID == "" || discarded.OpenID == "" {
				t.Fatalf("stale open not rolled back: result=%+v discarded=%+v", result, discarded)
			}
			_, remains := service.tabs[discarded.ID]
			if remains != (kind == "reopened" || kind == "cleanup-failed") {
				t.Fatalf("incorrect native lifetime after discard: %+v", service.tabs)
			}
			if _, err := st.GetBrowserTabState(context.Background(), "lifecycle", discarded.ID); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected open was persisted: %v", err)
			}
			status := browserToolCall(runner, tool.BrowserStatus, `{}`)
			var data struct {
				TabCount int `json:"tab_count"`
			}
			if err := json.Unmarshal([]byte(status.Content), &data); err != nil || !status.Ok || data.TabCount != 0 {
				t.Fatalf("ghost tab exposed to tools: %+v %v", status, err)
			}
		})
	}
}

func TestRejectedBrowserOpenPreservesAcceptedReplacement(t *testing.T) {
	st := storetest.New(t)
	if err := st.CreateSession(context.Background(), &store.Session{ID: "s", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	service := &fakeBrowserService{tabs: map[string]browser.TabSnapshot{}}
	s := &Server{store: st, browser: service}
	oldCtx, finishOld := s.BeginBrowserOpen(context.Background(), "s")
	defer finishOld()
	if err := s.BrowserSessionClosed(context.Background(), "s", nil); err != nil {
		t.Fatal(err)
	}
	freshCtx, finishFresh := s.BeginBrowserOpen(context.Background(), "s")
	defer finishFresh()
	fresh := browser.TabSnapshot{ID: "tab", SessionID: "s", OpenID: "fresh", URL: "https://fresh.example/"}
	service.tabs[fresh.ID] = fresh
	if err := s.BrowserTabOpened(freshCtx, "s", fresh); err != nil {
		t.Fatal(err)
	}
	service.discardOpenHook = func(context.Context, browser.TabSnapshot) error {
		t.Fatal("accepted replacement must not be discarded")
		return nil
	}
	if err := s.BrowserTabOpened(oldCtx, "s", browser.TabSnapshot{ID: "tab", SessionID: "s", OpenID: "old", URL: "https://old.example/"}); !errors.Is(err, browser.ErrTabNotFound) {
		t.Fatal(err)
	}
	if !s.browserTabAllowed("s", "tab") {
		t.Fatal("old open tombstoned replacement")
	}
	state, err := st.GetBrowserTabState(context.Background(), "s", "tab")
	if err != nil || state.URL != fresh.URL {
		t.Fatalf("replacement state lost: %+v %v", state, err)
	}
}

func TestBrowserSummaryDoesNotWaitForClosedTabs(t *testing.T) {
	service := &fakeBrowserService{tabs: map[string]browser.TabSnapshot{
		"closed": {ID: "closed", SessionID: "s", URL: "https://closed.example/"},
	}}
	s := &Server{browser: service}
	s.closeBrowserTabGate("s")
	s.rememberClosedBrowserTab("s", "closed")
	_, finish := s.BeginBrowserOpen(context.Background(), "s")
	defer finish()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tabs, err := s.listLiveBrowserTabsAfterToolOpens(ctx, "s")
	if err != nil || len(s.filterBrowserTabs("s", tabs)) != 0 {
		t.Fatalf("closed tab waited or resurfaced: %+v %v", tabs, err)
	}
	// An unpublished live tab, unlike a known closed tab, must honor request cancellation.
	service.tabs["pending"] = browser.TabSnapshot{ID: "pending", SessionID: "s", URL: "https://pending.example/"}
	if _, err := s.listLiveBrowserTabsAfterToolOpens(ctx, "s"); !errors.Is(err, context.Canceled) {
		t.Fatalf("summary wait ignored cancellation: %v", err)
	}
}

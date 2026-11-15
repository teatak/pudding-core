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
	for _, endpoint := range []string{"sync", "get", "observe"} {
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
			service.openHook = func(tab browser.TabSnapshot) { started <- tab; <-proceed }
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
			request := httptest.NewRequest(method, path, strings.NewReader(`{"url":"https://tool.example/","maxElements":20}`))
			request.Header.Set("Authorization", "Bearer "+testToken)
			recorder, done := httptest.NewRecorder(), make(chan struct{})
			go func() { defer close(done); srv.Config.Handler.ServeHTTP(recorder, request) }()
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

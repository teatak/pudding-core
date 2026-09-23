package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

type cleanupToolGate struct {
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	finished  chan struct{}
	err       error
	once      sync.Once
}

func newCleanupToolGate() *cleanupToolGate {
	return &cleanupToolGate{
		entered: make(chan struct{}), cancelled: make(chan struct{}),
		release: make(chan struct{}), finished: make(chan struct{}),
	}
}

func (g *cleanupToolGate) unblock() { g.once.Do(func() { close(g.release) }) }

type cleanupCall struct {
	sessionID string
	premature bool
}

// The synchronous tool deliberately finishes one filesystem write after
// cancellation. Cleanup must join that invocation before removing its area.
type cleanupToolRunner struct {
	home    string
	gates   map[string]*cleanupToolGate
	cleaned chan cleanupCall
}

func (r *cleanupToolRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return tool.BuiltinDefinitions(), nil
}

func (r *cleanupToolRunner) Call(ctx context.Context, call tool.Call) tool.Result {
	g := r.gates[call.SessionID]
	close(g.entered)
	<-ctx.Done()
	close(g.cancelled)
	<-g.release
	root, _, err := home.OpenSessionArtifacts(r.home, call.SessionID)
	if err == nil {
		err = root.WriteFile("late.txt", []byte("finished after cancellation"), 0o600)
		root.Close()
	}
	g.err = err
	close(g.finished)
	return tool.Result{CallID: call.CallID, Name: call.Name, Content: "cancelled"}
}

func (r *cleanupToolRunner) CloseSession(sessionID string) {
	premature := false
	if gate := r.gates[sessionID]; gate != nil {
		select {
		case <-gate.finished:
		default:
			premature = true
		}
	}
	r.cleaned <- cleanupCall{sessionID: sessionID, premature: premature}
}

func newCleanupWaitServer(t *testing.T) (*Server, *memstore.Memstore, *cleanupToolRunner) {
	t.Helper()
	ctx := context.Background()
	ms := memstore.New()
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "m"}},
	}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"parent", "unrelated"} {
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Title: id, Provider: "mock", Model: "m"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := ms.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Title: "child", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	runner := &cleanupToolRunner{home: t.TempDir(), gates: make(map[string]*cleanupToolGate), cleaned: make(chan cleanupCall, 16)}
	for _, id := range []string{"parent", "child", "unrelated"} {
		runner.gates[id] = newCleanupToolGate()
	}
	hub := event.NewHub()
	client := mock.New(mock.WithDelay(0), mock.WithChunks([]provider.Chunk{
		{Tool: &provider.ToolCallChunk{Index: 0, CallID: "blocked", Name: tool.TimeGetCurrent, ArgsDelta: `{}`}},
		{Done: true, Finish: provider.FinishToolCalls},
	}))
	eng := engine.New(ms, hub, registry.Static(client), ms, engine.WithTools(runner))
	t.Cleanup(func() {
		for id, gate := range runner.gates {
			_ = eng.Cancel(id)
			gate.unblock()
		}
		eng.Stop()
		eng.Wait()
	})
	for _, id := range []string{"parent", "child", "unrelated"} {
		root, _, err := home.OpenSessionArtifacts(runner.home, id)
		if err != nil {
			t.Fatal(err)
		}
		root.Close()
		if _, err := eng.Submit(ctx, engine.SubmitInput{SessionID: id, ClientMessageID: "start", Text: "run tool"}); err != nil {
			t.Fatal(err)
		}
		waitCleanupSignal(t, runner.gates[id].entered, id+" tool start")
	}
	return New(eng, ms, ms, hub).WithHome(runner.home), ms, runner
}

func waitCleanupSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

type cleanupArchiveStore struct {
	store.Store
	archived chan struct{}
}

func (s *cleanupArchiveStore) ArchiveSession(ctx context.Context, id string) (*store.Session, error) {
	session, err := s.Store.ArchiveSession(ctx, id)
	close(s.archived)
	return session, err
}

func TestSessionCleanupWaitsForParentAndChildTools(t *testing.T) {
	for _, name := range []string{"purge", "archive", "archive_retry"} {
		archive := name != "purge"
		method, path, status := http.MethodDelete, "/sessions/parent", http.StatusNoContent
		if archive {
			method, path, status = http.MethodPost, "/sessions/parent/archive", http.StatusOK
		}
		t.Run(name, func(t *testing.T) {
			srv, ms, runner := newCleanupWaitServer(t)
			handler := srv.Handler(testToken, nil)
			if name == "archive_retry" {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				firstRequest := httptest.NewRequest(method, path, nil).WithContext(ctx)
				firstRequest.Header.Set("Authorization", "Bearer "+testToken)
				firstRecorder := httptest.NewRecorder()
				firstReturned := make(chan struct{})
				go func() { handler.ServeHTTP(firstRecorder, firstRequest); close(firstReturned) }()
				waitCleanupSignal(t, runner.gates["parent"].cancelled, "parent cancellation before retry")
				cancel()
				waitCleanupSignal(t, firstReturned, "cancelled archive before retry")
				if firstRecorder.Code != http.StatusInternalServerError {
					t.Fatalf("cancelled archive status = %d: %s", firstRecorder.Code, firstRecorder.Body.String())
				}
			}
			archived := make(chan struct{})
			if archive {
				srv.store = &cleanupArchiveStore{Store: ms, archived: archived}
			}
			request := httptest.NewRequest(method, path, nil)
			request.Header.Set("Authorization", "Bearer "+testToken)
			recorder := httptest.NewRecorder()
			returned := make(chan struct{})
			go func() { handler.ServeHTTP(recorder, request); close(returned) }()
			if archive {
				waitCleanupSignal(t, archived, "archive holding the lifecycle lock")
			}
			for _, id := range []string{"parent", "child"} {
				waitCleanupSignal(t, runner.gates[id].cancelled, id+" cancellation")
			}
			restoreRequest := httptest.NewRequest(http.MethodPost, "/sessions/parent/restore", nil)
			restoreRequest.Header.Set("Authorization", "Bearer "+testToken)
			restoreRecorder := httptest.NewRecorder()
			restored := make(chan struct{})
			go func() { handler.ServeHTTP(restoreRecorder, restoreRequest); close(restored) }()
			// A child lifecycle operation shares its parent's gate and can stop
			// waiting without affecting the ongoing cleanup.
			waitCtx, stopWaiting := context.WithTimeout(context.Background(), 25*time.Millisecond)
			unlock, err := srv.lockSessionLifecycle(waitCtx, "child")
			stopWaiting()
			if unlock != nil {
				unlock()
				t.Fatal("child lifecycle operation bypassed parent cleanup")
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("lifecycle wait error = %v", err)
			}
			independentCtx, stopIndependent := context.WithTimeout(context.Background(), time.Second)
			unlock, err = srv.lockSessionLifecycle(independentCtx, "unrelated")
			stopIndependent()
			if err != nil {
				t.Fatalf("unrelated lifecycle operation blocked: %v", err)
			}
			unlock()
			assertPending := func(stage string) {
				t.Helper()
				select {
				case <-returned:
					t.Fatalf("%s returned before %s completed", name, stage)
				case cleanup := <-runner.cleaned:
					t.Fatalf("resources cleaned before %s completed: %+v", stage, cleanup)
				case <-restored:
					t.Fatalf("restore reopened admission before %s completed", stage)
				case <-time.After(25 * time.Millisecond):
				}
			}
			assertPending("parent tool")
			runner.gates["parent"].unblock()
			waitCleanupSignal(t, runner.gates["parent"].finished, "parent tool finish")
			assertPending("child tool")
			runner.gates["child"].unblock()
			waitCleanupSignal(t, returned, "cleanup response while unrelated tool is still blocked")
			waitCleanupSignal(t, restored, "restore after cleanup")
			restoreStatus := http.StatusNotFound
			if archive {
				restoreStatus = http.StatusOK
			}
			if restoreRecorder.Code != restoreStatus {
				t.Fatalf("restore status = %d, want %d: %s", restoreRecorder.Code, restoreStatus, restoreRecorder.Body.String())
			}
			if recorder.Code != status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, status, recorder.Body.String())
			}
			for i := 0; i < 2; i++ {
				cleanup := <-runner.cleaned
				if cleanup.premature || cleanup.sessionID == "unrelated" {
					t.Fatalf("incorrect resource cleanup: %+v", cleanup)
				}
			}
			for _, id := range []string{"parent", "child"} {
				gate := runner.gates[id]
				waitCleanupSignal(t, gate.finished, id+" tool finish")
				if gate.err != nil {
					t.Fatalf("%s final artifact write: %v", id, gate.err)
				}
				artifactPath, exists, err := home.ExistingSessionArtifacts(runner.home, id)
				if err != nil || exists != archive {
					t.Fatalf("%s artifact area exists = %v, want %v, err = %v", id, exists, archive, err)
				}
				if archive {
					if _, err := os.Stat(filepath.Join(artifactPath, "late.txt")); err != nil {
						t.Fatalf("archive must preserve completed artifact: %v", err)
					}
				} else if _, err := ms.ParentSessionID(context.Background(), id); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("purged %s still exists: %v", id, err)
				}
			}
			select {
			case <-runner.gates["unrelated"].cancelled:
				t.Fatal("cleanup cancelled an unrelated session")
			default:
			}
			assertNoSessionLifecycleEntries(t, srv)
		})
	}
}

func TestSessionCleanupCancelledWaitKeepsResources(t *testing.T) {
	srv, ms, runner := newCleanupWaitServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan error, 1)
	go func() { returned <- srv.purgeSession(ctx, "parent") }()
	waitCleanupSignal(t, runner.gates["parent"].cancelled, "parent cancellation")
	cancel()
	select {
	case err := <-returned:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("purge error = %v, want context cancellation", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled request did not stop waiting")
	}
	if _, err := ms.ParentSessionID(context.Background(), "parent"); err != nil {
		t.Fatalf("cancelled cleanup deleted the session: %v", err)
	}
	select {
	case cleanup := <-runner.cleaned:
		t.Fatalf("cancelled wait released resources: %+v", cleanup)
	default:
	}
	if _, exists, err := home.ExistingSessionArtifacts(runner.home, "parent"); err != nil || !exists {
		t.Fatalf("cancelled wait removed artifact area: exists=%v err=%v", exists, err)
	}
	assertNoSessionLifecycleEntries(t, srv)
}

func TestSessionArchiveCancelledWaitCanRetryCleanup(t *testing.T) {
	srv, ms, runner := newCleanupWaitServer(t)
	handler := srv.Handler(testToken, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	request := httptest.NewRequest(http.MethodPost, "/sessions/parent/archive", nil).WithContext(ctx)
	request.Header.Set("Authorization", "Bearer "+testToken)
	first := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() { handler.ServeHTTP(first, request); close(returned) }()
	for _, id := range []string{"parent", "child"} {
		waitCleanupSignal(t, runner.gates[id].cancelled, id+" cancellation")
	}
	cancel()
	waitCleanupSignal(t, returned, "cancelled archive request")
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("cancelled archive status = %d: %s", first.Code, first.Body.String())
	}
	if _, err := ms.GetSession(context.Background(), "parent"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("archive was not committed: %v", err)
	}
	select {
	case cleanup := <-runner.cleaned:
		t.Fatalf("cancelled wait released resources: %+v", cleanup)
	default:
	}
	assertNoSessionLifecycleEntries(t, srv)
	for _, id := range []string{"parent", "child"} {
		runner.gates[id].unblock()
		waitCleanupSignal(t, runner.gates[id].finished, id+" tool finish")
		waitCtx, stopWaiting := context.WithTimeout(context.Background(), time.Second)
		err := srv.engine.WaitSession(waitCtx, id)
		stopWaiting()
		if err != nil {
			t.Fatal(err)
		}
	}
	retry := httptest.NewRequest(http.MethodPost, "/sessions/parent/archive", nil)
	retry.Header.Set("Authorization", "Bearer "+testToken)
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, retry)
	if second.Code != http.StatusOK {
		t.Fatalf("archive retry status = %d: %s", second.Code, second.Body.String())
	}
	for _, id := range []string{"parent", "child"} {
		select {
		case cleanup := <-runner.cleaned:
			if cleanup.sessionID != id || cleanup.premature {
				t.Fatalf("retry resource cleanup = %+v, want %s after its tool finished", cleanup, id)
			}
		default:
			t.Fatalf("archive retry did not release %s resources", id)
		}
		if _, exists, err := home.ExistingSessionArtifacts(runner.home, id); err != nil || !exists {
			t.Fatalf("archive retry removed %s artifacts: exists=%v err=%v", id, exists, err)
		}
	}
	select {
	case <-runner.gates["unrelated"].cancelled:
		t.Fatal("archive retry cancelled an unrelated session")
	default:
	}
	assertNoSessionLifecycleEntries(t, srv)
}

func TestSessionCleanupDeletesOnlyRequestedChild(t *testing.T) {
	srv, ms, runner := newCleanupWaitServer(t)
	ctx := context.Background()
	if result, err := srv.engine.Submit(ctx, engine.SubmitInput{SessionID: "child", ClientMessageID: "queued", Text: "next"}); err != nil || !result.Queued {
		t.Fatalf("child queue setup: %+v %v", result, err)
	}
	returned := make(chan error, 1)
	go func() { returned <- srv.purgeSession(ctx, "child") }()
	waitCleanupSignal(t, runner.gates["child"].cancelled, "child cancellation")
	if _, err := srv.engine.Submit(ctx, engine.SubmitInput{SessionID: "child", ClientMessageID: "late", Text: "late"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleting child accepted new input: %v", err)
	}
	select {
	case err := <-returned:
		t.Fatalf("child deletion did not join its active tool: %v", err)
	default:
	}
	runner.gates["child"].unblock()
	select {
	case err := <-returned:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child deletion waited for another session")
	}
	cleanup := <-runner.cleaned
	if cleanup.sessionID != "child" || cleanup.premature {
		t.Fatalf("child cleanup = %+v", cleanup)
	}
	if _, exists, err := home.ExistingSessionArtifacts(runner.home, "child"); err != nil || exists {
		t.Fatalf("deleted child artifacts: exists=%v err=%v", exists, err)
	}
	if _, err := ms.ParentSessionID(ctx, "child"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("child still exists: %v", err)
	}
	for _, id := range []string{"parent", "unrelated"} {
		if _, err := ms.GetSession(ctx, id); err != nil {
			t.Fatalf("child deletion affected %s: %v", id, err)
		}
		select {
		case <-runner.gates[id].cancelled:
			t.Fatalf("child deletion cancelled %s", id)
		default:
		}
	}
	assertNoSessionLifecycleEntries(t, srv)
}

func assertNoSessionLifecycleEntries(t *testing.T, srv *Server) {
	t.Helper()
	srv.sessionLifecycleMu.Lock()
	defer srv.sessionLifecycleMu.Unlock()
	if len(srv.sessionLifecycles) != 0 {
		t.Fatalf("completed lifecycle operations retained %d entries", len(srv.sessionLifecycles))
	}
}

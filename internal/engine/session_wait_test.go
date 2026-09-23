package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

type turnRegistrationStore struct {
	store.Store
	began   chan struct{}
	release chan struct{}
	once    sync.Once
}

type queuedFailureResolver struct {
	entered chan struct{}
	release chan struct{}
}

func (r *queuedFailureResolver) Resolve(context.Context, string) (provider.Client, error) {
	close(r.entered)
	<-r.release
	return nil, errors.New("provider unavailable")
}

type failingTurnFinishStore struct {
	store.Store
	err error
}

func (s *failingTurnFinishStore) FinishTurn(ctx context.Context, in store.FinishTurnInput) (*store.FinishTurnResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.Store.FinishTurn(ctx, in)
}

func TestWaitSessionJoinsFailureBeforeRunTurn(t *testing.T) {
	for _, finishErr := range []error{nil, store.ErrNotFound, errors.New("finish failed")} {
		name := "finalized"
		if finishErr != nil {
			name = finishErr.Error()
		}
		t.Run(name, func(t *testing.T) {
			eng, ms, _, sessionID := newTestEngine(t)
			resolver := &queuedFailureResolver{entered: make(chan struct{}), release: make(chan struct{})}
			eng.resolver = resolver
			eng.store = &failingTurnFinishStore{Store: ms, err: finishErr}
			if _, err := ms.QueueInput(context.Background(), store.QueueInputInput{
				SessionID: sessionID, ClientMessageID: "queued", Text: "start", Provider: "mock", Model: "mock-model",
			}); err != nil {
				t.Fatal(err)
			}
			drained := make(chan struct{})
			go func() { eng.TryDrainQueued(sessionID); close(drained) }()
			var release sync.Once
			t.Cleanup(func() {
				release.Do(func() { close(resolver.release) })
				<-drained
				eng.Stop()
			})
			waitSessionSignal(t, resolver.entered, "queued runtime registration before provider failure")
			eng.mu.Lock()
			active := eng.running[sessionID]
			eng.mu.Unlock()
			if active == nil {
				t.Fatal("promoted turn is not registered")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
			defer cancel()
			if err := eng.WaitSession(ctx, sessionID); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("WaitSession before queued failure = %v", err)
			}
			// Clearing an obsolete turn cannot release a newer turn's waiter.
			eng.clearRunning(sessionID, "older-turn")
			select {
			case <-active.done:
				t.Fatal("obsolete turn closed the active completion signal")
			default:
			}
			release.Do(func() { close(resolver.release) })
			waitSessionSignal(t, active.done, "pre-run failure cleanup")
			if err := eng.WaitSession(context.Background(), sessionID); err != nil {
				t.Fatal(err)
			}
			// A repeated clear must not double-close the completion signal.
			eng.clearRunning(sessionID, active.turnID)
		})
	}
}

type turnFinishBarrierStore struct {
	store.Store
	finished chan struct{}
	release  chan struct{}
	once     sync.Once
}

func (s *turnFinishBarrierStore) unblock() { s.once.Do(func() { close(s.release) }) }

func (s *turnFinishBarrierStore) FinishTurn(ctx context.Context, in store.FinishTurnInput) (*store.FinishTurnResult, error) {
	result, err := s.Store.FinishTurn(ctx, in)
	if in.TurnID == "first" {
		close(s.finished)
		<-s.release
	}
	return result, err
}

func TestFinishingTurnCannotLoseCompletionToNextTurn(t *testing.T) {
	ctx := context.Background()
	eng, ms, _, sessionID := newTestEngine(t, mock.WithDelay(time.Hour))
	title := "session"
	if _, err := ms.UpdateSession(ctx, sessionID, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{SessionID: sessionID, TurnID: "first", UserMessageID: "first", ClientMessageID: "first", UserText: "first"}); err != nil {
		t.Fatal(err)
	}
	_, cancelFirst := context.WithCancel(ctx)
	defer cancelFirst()
	active := newActiveTurn("first", cancelFirst)
	eng.running[sessionID] = active
	st := &turnFinishBarrierStore{Store: ms, finished: make(chan struct{}), release: make(chan struct{})}
	eng.store = st
	finalized := make(chan struct{})
	go func() {
		eng.finishTurn(sessionID, "first", store.ModeChat, store.TurnCancelled, "", nil)
		close(finalized)
	}()
	waitSessionSignal(t, st.finished, "canonical finish before runtime cleanup")
	submitted := make(chan error, 1)
	submitDone := make(chan struct{})
	go func() {
		_, err := eng.Submit(ctx, SubmitInput{SessionID: sessionID, ClientMessageID: "second", Text: "second"})
		submitted <- err
		close(submitDone)
	}()
	t.Cleanup(func() {
		st.unblock()
		<-finalized
		<-submitDone
		_ = eng.Cancel(sessionID)
		eng.Stop()
		eng.Wait()
	})
	select {
	case err := <-submitted:
		t.Fatalf("next turn replaced an uncleared runtime: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	st.unblock()
	waitSessionSignal(t, active.done, "original turn completion")
	select {
	case err := <-submitted:
		if err != nil {
			t.Fatalf("next turn after finalization: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("next turn did not start after finalization")
	}
}

func (s *turnRegistrationStore) unblock() { s.once.Do(func() { close(s.release) }) }

func (s *turnRegistrationStore) BeginTurn(ctx context.Context, in store.BeginTurnInput) (*store.BeginTurnResult, error) {
	result, err := s.Store.BeginTurn(ctx, in)
	if err == nil && !result.Duplicate {
		close(s.began)
		<-s.release
	}
	return result, err
}

func waitSessionSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func TestCancelCannotMissTurnBeingRegistered(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	if err := ms.CreateSession(ctx, &store.Session{ID: "session", Title: "session", Provider: "mock", Model: "m"}); err != nil {
		t.Fatal(err)
	}
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{
		ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "m"}},
	}); err != nil {
		t.Fatal(err)
	}
	st := &turnRegistrationStore{Store: ms, began: make(chan struct{}), release: make(chan struct{})}
	eng := New(st, event.NewHub(), registry.Static(mock.New(mock.WithDelay(time.Hour))), ms)
	submitted := make(chan error, 1)
	go func() {
		_, err := eng.Submit(ctx, SubmitInput{SessionID: "session", ClientMessageID: "start", Text: "run"})
		submitted <- err
	}()
	t.Cleanup(func() {
		st.unblock()
		<-submitted
		_ = eng.Cancel("session")
		eng.Stop()
		eng.Wait()
	})
	waitSessionSignal(t, st.began, "canonical turn creation before runtime registration")
	// Keep the session active so the delayed provider holds the turn until
	// Cancel. Archiving here could fail context building and finish the turn
	// before Cancel runs, obscuring the registration window being tested.
	cancelled := make(chan error, 1)
	go func() { cancelled <- eng.Cancel("session") }()
	select {
	case err := <-cancelled:
		t.Fatalf("cancel passed an unregistered running turn: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	st.unblock()
	select {
	case err := <-cancelled:
		if err != nil {
			t.Fatalf("cancel after registration: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not proceed after runtime registration")
	}
}

package engine

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
)

// Delay the return after committing collaboration.changed. A steer can commit
// while the caller is doing follow-up work, before that caller publishes.
type delayedCollaborationStore struct {
	store.Store
	committed chan struct{}
	release   chan struct{}
}

func (s *delayedCollaborationStore) StopCollaboration(ctx context.Context, id string) ([]event.Event, error) {
	events, err := s.Store.StopCollaboration(ctx, id)
	close(s.committed)
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return events, err
}

func TestSteerAndCollaborationPublishInCommitOrder(t *testing.T) {
	eng, st, hub, sid := newTestEngine(t, mock.WithScript([]string{"slow"}), mock.WithDelay(5*time.Second))
	delayed := &delayedCollaborationStore{Store: st, committed: make(chan struct{}), release: make(chan struct{})}
	defer close(delayed.release)
	eng.store = delayed
	ctx := context.Background()
	title := "Order"
	if _, err := st.UpdateSession(ctx, sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	live, unsub := hub.Subscribe(sid)
	defer unsub()
	first, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: "initial", Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = eng.cancelOne(sid); eng.Wait(); eng.Stop() })
	steer, err := st.AppendTurnSteer(ctx, store.AppendTurnSteerInput{
		SessionID: sid, TurnID: first.TurnID, ClientMessageID: "guide", UserMessageID: "guide", UserText: "guide",
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- eng.StopCollaboration(ctx, sid) }()
	select {
	case <-delayed.committed:
	case <-time.After(5 * time.Second):
		t.Fatal("collaboration did not commit")
	}
	if err := eng.applyTurnSteers(first.TurnID, []pendingSteer{{messageID: "guide", event: steer.Event}}); err != nil {
		t.Fatal(err)
	}
	// Unblock without relying on scheduler timing or a repeated probabilistic test.
	delayed.release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	canonical, err := st.EventsAfter(ctx, sid, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var received []event.Event
	for len(received) < len(canonical) {
		select {
		case ev, ok := <-live:
			if !ok {
				t.Fatal("live subscription closed")
			}
			if ev.Seq > 0 {
				received = append(received, ev)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("missing live events: %+v", received)
		}
	}
	if !reflect.DeepEqual(received, canonical) {
		t.Fatalf("live events must equal canonical commit order:\nlive: %+v\nstore: %+v", received, canonical)
	}
}

func TestCancelFromCommittedTurnStartedWaitsForRegistration(t *testing.T) {
	eng, _, hub, sid := newTestEngine(t, mock.WithDelay(5*time.Second))
	live, unsub := hub.Subscribe(sid)
	defer unsub()
	submitted := make(chan error, 1)
	go func() {
		_, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "initial", Text: "first"})
		submitted <- err
	}()
	defer eng.Stop()
	select {
	case ev := <-live:
		if ev.Kind != event.TurnStarted {
			t.Fatalf("first event=%+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("started event was not published")
	}
	if err := eng.Cancel(sid); err != nil {
		t.Fatalf("cancel from started must wait for registration: %v", err)
	}
	if err := <-submitted; err != nil {
		t.Fatal(err)
	}
	eng.Wait()
}

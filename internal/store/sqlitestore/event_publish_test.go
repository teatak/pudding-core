package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func TestEventPublisherDeliversCommittedCanonicalEvents(t *testing.T) {
	s, _ := openTestStore(t)
	createTestSession(t, s, "a")
	createTestSession(t, s, "b")
	var published []event.Event
	s.SetEventPublisher(func(ev event.Event) {
		// Query the database directly: a pre-commit callback would hold its
		// only connection and time out. Store methods are forbidden here.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		var payload string
		if err := s.db.QueryRowContext(ctx, `SELECT payload FROM events WHERE session_id=? AND seq=?`, ev.SessionID, ev.Seq).Scan(&payload); err != nil {
			t.Errorf("event published before commit: %v", err)
		}
		published = append(published, ev)
	})
	err := s.tx(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		for _, ev := range []event.Event{
			{SessionID: "a", Kind: event.InputQueued, Text: "canonical", Parts: []byte(`[{"type":"text","text":"canonical"}]`)},
			{SessionID: "a", Kind: event.InputUpdated},
			{SessionID: "b", Kind: event.WidgetNotice},
		} {
			if err := insertEventTx(ctx, tx, &ev); err != nil {
				return err
			}
			ev.Text = "changed after insertion"
			if len(ev.Parts) > 0 {
				ev.Parts[0] = '!'
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.EventsAfter(context.Background(), "a", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.EventsAfter(context.Background(), "b", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if want := append(a, b...); !reflect.DeepEqual(published, want) {
		t.Fatalf("publisher differs from canonical data: got %+v want %+v", published, want)
	}
}

func TestEventPublisherSkipsRollbackAndFailedCommit(t *testing.T) {
	s, _ := openTestStore(t)
	createTestSession(t, s, "s")
	var published []event.Event
	s.SetEventPublisher(func(ev event.Event) { published = append(published, ev) })
	rollback := errors.New("rollback")
	for _, failCommit := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
			if err := insertEventTx(ctx, tx, &event.Event{SessionID: "s", Kind: event.InputQueued}); err != nil {
				return err
			}
			if failCommit {
				cancel()
				return nil
			}
			return rollback
		})
		cancel()
		if err == nil {
			t.Fatal("transaction should fail")
		}
		if len(published) != 0 {
			t.Fatalf("failed transaction published events: %+v", published)
		}
	}
	queued, err := s.QueueInput(context.Background(), store.QueueInputInput{SessionID: "s", ClientMessageID: "queued", Text: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	if len(published) != 1 || published[0].Seq != 1 || !reflect.DeepEqual(published[0], *queued.QueuedEvent) {
		t.Fatalf("rollback must leave no sequence gap: %+v", published)
	}
	duplicate, err := s.QueueInput(context.Background(), store.QueueInputInput{SessionID: "s", ClientMessageID: "queued", Text: "queued"})
	if err != nil || !duplicate.Duplicate || len(published) != 1 {
		t.Fatalf("idempotent input must not publish again: %+v err=%v events=%+v", duplicate, err, published)
	}
}

func TestEventPublisherSerializesConcurrentWriters(t *testing.T) {
	s, _ := openTestStore(t)
	createTestSession(t, s, "s")
	var published []event.Event
	s.SetEventPublisher(func(ev event.Event) { published = append(published, ev) })
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.QueueInput(context.Background(), store.QueueInputInput{SessionID: "s", ClientMessageID: store.NewID("input"), Text: "queued"})
			if err != nil {
				t.Errorf("queue input: %v", err)
			}
		}()
	}
	wg.Wait()
	canonical, err := s.EventsAfter(context.Background(), "s", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(published) != 32 || !reflect.DeepEqual(published, canonical) {
		t.Fatalf("concurrent live sequence differs from store: live=%+v store=%+v", published, canonical)
	}
}

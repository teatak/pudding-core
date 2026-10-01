package event

import "testing"

// A full subscriber is closed rather than skipped: a skipped persisted event
// would be passed by the next event's Last-Event-ID and never replayed.
func TestHubClosesFullSubscriberInsteadOfDroppingEvents(t *testing.T) {
	hub := NewHub()
	slow, cancelSlow := hub.Subscribe("session")
	fast, cancelFast := hub.Subscribe("session")
	defer cancelFast()

	for seq := int64(1); seq <= subscriberBuffer; seq++ {
		hub.Publish(Event{SessionID: "session", Seq: seq})
		if ev := <-fast; ev.Seq != seq {
			t.Fatalf("fast subscriber got seq %d, want %d", ev.Seq, seq)
		}
	}
	hub.Publish(Event{SessionID: "session", Seq: subscriberBuffer + 1})

	for seq := int64(1); seq <= subscriberBuffer; seq++ {
		ev, ok := <-slow
		if !ok || ev.Seq != seq {
			t.Fatalf("slow subscriber must keep every buffered event in order: got %d ok=%v, want %d", ev.Seq, ok, seq)
		}
	}
	if ev, ok := <-slow; ok {
		t.Fatalf("full subscriber must be closed instead of skipping seq %d, got %+v", subscriberBuffer+1, ev)
	}
	if ev := <-fast; ev.Seq != subscriberBuffer+1 {
		t.Fatalf("other subscribers keep receiving: got seq %d", ev.Seq)
	}

	// Cancelling an already closed subscriber and publishing again are safe.
	cancelSlow()
	cancelSlow()
	hub.Publish(Event{SessionID: "session", Seq: subscriberBuffer + 2})
	if ev := <-fast; ev.Seq != subscriberBuffer+2 {
		t.Fatalf("fast subscriber got seq %d after cancel", ev.Seq)
	}
}

func TestHubCancelClosesAndRemovesSubscriber(t *testing.T) {
	hub := NewHub()
	ch, cancel := hub.Subscribe("session")
	cancel()
	if _, ok := <-ch; ok {
		t.Fatal("cancelled subscriber must be closed")
	}
	if len(hub.subs) != 0 {
		t.Fatalf("cancelled subscriber must be removed: %+v", hub.subs)
	}
	hub.Publish(Event{SessionID: "session", Seq: 1})
}

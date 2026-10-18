package event

import (
	"strings"
	"testing"
)

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

func TestNoteArgumentSnapshotsJoinReconnectAndTermination(t *testing.T) {
	h := NewHub()
	ev := Event{Kind: TurnTool, SessionID: "s", TurnID: "t", CallID: "c", Name: "builtin_page_edit", Phase: "streaming_args", ArgsDelta: `{"replacement":"`}
	h.Publish(ev)
	ev.ArgsDelta = "partial"
	h.Publish(ev)
	ch, cancel := h.Subscribe("s")
	snapshot := <-ch
	if snapshot.ArgsText != `{"replacement":"partial` || snapshot.ArgsDelta != "" || snapshot.Seq != 0 {
		t.Fatalf("bad snapshot: %+v", snapshot)
	}
	ev.ArgsDelta = " more"
	h.Publish(ev)
	if delta := <-ch; delta.ArgsDelta != " more" || delta.ArgsText != "" {
		t.Fatalf("bad delta: %+v", delta)
	}
	cancel()
	ch, cancel = h.Subscribe("s")
	defer cancel()
	if snapshot = <-ch; snapshot.ArgsText != `{"replacement":"partial more` {
		t.Fatal("reconnect lost prefix")
	}
	h.Publish(Event{Kind: TurnCancelled, SessionID: "s", TurnID: "t"})
	next, stop := h.Subscribe("s")
	defer stop()
	select {
	case ev := <-next:
		t.Fatalf("terminal preview replayed %+v", ev)
	default:
	}
	if len(h.noteStreams) != 0 {
		t.Fatal("terminal stream retained")
	}
}

func TestNoteArgumentSnapshotsBoundMemoryAndIgnoreOtherTools(t *testing.T) {
	h := NewHub()
	ev := Event{Kind: TurnTool, SessionID: "s", TurnID: "t", CallID: "c", Name: "unrelated", Phase: "streaming_args", ArgsDelta: "x"}
	h.Publish(ev)
	if len(h.noteStreams) != 0 {
		t.Fatal("captured unrelated tool")
	}
	ev.Name = "builtin_note_edit"
	ev.ArgsDelta = strings.Repeat("x", maxNoteArgumentBytes+1)
	h.Publish(ev)
	ev.ArgsDelta = "suffix"
	h.Publish(ev)
	ch, cancel := h.Subscribe("s")
	defer cancel()
	select {
	case <-ch:
		t.Fatal("replayed incomplete oversized prefix")
	default:
	}
	ev.Phase = "error"
	h.Publish(ev)
	if len(h.noteStreams) != 0 {
		t.Fatal("failed stream retained")
	}
}

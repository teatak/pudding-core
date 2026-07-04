package queue

import (
	"context"
	"testing"
	"time"
)

func TestQueueEnqueueNextAndClear(t *testing.T) {
	q := New()
	if !q.Enqueue(Item{SessionID: "sess_a", TurnID: "turn_a", SegmentID: "seg_a", Text: "hello"}) {
		t.Fatal("enqueue failed")
	}
	if !q.Enqueue(Item{SessionID: "sess_a", TurnID: "turn_b", SegmentID: "seg_b", Text: "drop turn"}) {
		t.Fatal("enqueue failed")
	}
	if !q.Enqueue(Item{SessionID: "sess_b", TurnID: "turn_c", SegmentID: "seg_c", Text: "drop session"}) {
		t.Fatal("enqueue failed")
	}
	if got := q.ClearTurn("turn_b"); got != 1 {
		t.Fatalf("ClearTurn removed %d, want 1", got)
	}
	if got := q.ClearSession("sess_b"); got != 1 {
		t.Fatalf("ClearSession removed %d, want 1", got)
	}

	item, ok := q.Next(context.Background())
	if !ok || item.Text != "hello" {
		t.Fatalf("Next = %+v %v", item, ok)
	}
	if got := q.Len(); got != 0 {
		t.Fatalf("Len = %d, want 0", got)
	}
}

func TestQueueNextStopsOnContext(t *testing.T) {
	q := New()
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	if item, ok := q.Next(ctx); ok {
		t.Fatalf("Next should stop, got %+v", item)
	}
}

func TestQueueCloseStopsNext(t *testing.T) {
	q := New()
	q.Close()
	if item, ok := q.Next(context.Background()); ok {
		t.Fatalf("Next should stop after close, got %+v", item)
	}
	if q.Enqueue(Item{Text: "late"}) {
		t.Fatal("enqueue after close should fail")
	}
}

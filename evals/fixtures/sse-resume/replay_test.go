package sseresume

import (
	"reflect"
	"testing"
)

func TestReplayAfterIsExclusive(t *testing.T) {
	events := []Event{{1, "started"}, {2, "delta"}, {3, "completed"}}
	if got, want := ReplayAfter(events, 2), []Event{{3, "completed"}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ReplayAfter = %v, want %v", got, want)
	}
	if got := ReplayAfter(events, 0); !reflect.DeepEqual(got, events) {
		t.Fatalf("zero cursor = %v, want %v", got, events)
	}
	if got := ReplayAfter(events, 3); len(got) != 0 {
		t.Fatalf("latest cursor should return nothing: %v", got)
	}
}

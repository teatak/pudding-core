package event

import "testing"

func TestBackgroundProcessEventsAreTransient(t *testing.T) {
	for _, kind := range []Kind{ProcessStarted, ProcessFinished, ProcessStopped, ProcessRemoved} {
		if (Event{Kind: kind}).Persistent() {
			t.Fatalf("%s must not be persisted", kind)
		}
	}
}

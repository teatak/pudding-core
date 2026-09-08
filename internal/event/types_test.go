package event

import (
	"encoding/json"
	"testing"
)

func TestAttachmentOnlyInputEventsOmitEmptyText(t *testing.T) {
	for _, kind := range []Kind{InputQueued, InputUpdated, InputSteered} {
		t.Run(string(kind), func(t *testing.T) {
			ev := Event{Kind: kind, Seq: 1, SessionID: "session", ClientMessageID: "input"}
			data, err := json.Marshal(ev)
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(data, &payload); err != nil {
				t.Fatal(err)
			}
			if _, present := payload["text"]; present {
				t.Fatalf("empty input text must be omitted: %s", data)
			}
			if payload["clientMessageID"] != "input" || payload["seq"] != float64(1) {
				t.Fatalf("input identity must survive: %s", data)
			}
		})
	}
}

func TestBackgroundProcessEventsAreTransient(t *testing.T) {
	for _, kind := range []Kind{ProcessStarted, ProcessFinished, ProcessStopped, ProcessRemoved} {
		if (Event{Kind: kind}).Persistent() {
			t.Fatalf("%s must not be persisted", kind)
		}
	}
}

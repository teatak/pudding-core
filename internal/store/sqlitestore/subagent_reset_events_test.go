package sqlitestore

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func TestSubagentResetPublishesLifecycleChangesWithoutTaskCompletion(t *testing.T) {
	for _, send := range []bool{false, true} {
		t.Run(map[bool]string{false: "reset", true: "reset-and-send"}[send], func(t *testing.T) {
			ctx := context.Background()
			st, in := resetLifecycleFixture(t)
			if send {
				in.Input = &store.QueueInputInput{SessionID: "child", ClientMessageID: "fresh", Text: "fresh task"}
			}
			before := make(map[string]int64)
			for _, id := range []string{"parent", "child"} {
				events, err := st.EventsAfter(ctx, id, 0, 0)
				if err != nil {
					t.Fatal(err)
				}
				if len(events) > 0 {
					before[id] = events[len(events)-1].Seq
				}
			}
			reset, err := st.ResetSubagent(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"parent", "child"} {
				events, err := st.EventsAfter(ctx, id, before[id], 0)
				if err != nil {
					t.Fatal(err)
				}
				changed := false
				for _, ev := range events {
					if ev.Kind == event.CollaborationChanged {
						changed = true
					}
					if ev.TurnID == reset.ResetTurnID && (ev.Kind == event.TurnStarted || ev.Kind == event.TurnCompleted) {
						t.Errorf("maintenance emitted a task event: %+v", ev)
					}
				}
				if !changed {
					t.Errorf("%s did not receive a lifecycle refresh: %+v", id, events)
				}
				if len(events) > 0 {
					before[id] = events[len(events)-1].Seq
				}
			}
			if replay, err := st.ResetSubagent(ctx, in); err != nil || !replay.Duplicate {
				t.Fatalf("replay: %+v %v", replay, err)
			}
			for _, id := range []string{"parent", "child"} {
				if events, err := st.EventsAfter(ctx, id, before[id], 0); err != nil || len(events) != 0 {
					t.Fatalf("replay emitted more events: %+v %v", events, err)
				}
			}
		})
	}
}

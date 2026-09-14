package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestCompactBoundaryChecksSnapshotAndRunningOwnerAtomically(t *testing.T) {
	for _, backend := range []string{"memory", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			ctx := context.Background()
			var db store.Store = memstore.New()
			path := filepath.Join(t.TempDir(), "compact.sqlite")
			if backend == "sqlite" {
				sqlite, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				db = sqlite
				defer func() {
					if db != nil {
						_ = db.(*Store).Close()
					}
				}()
			}
			sid := "session"
			if err := db.CreateSession(ctx, &store.Session{ID: sid, Provider: "mock", Model: "mock"}); err != nil {
				t.Fatal(err)
			}
			_, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "old", UserMessageID: "old-input", ClientMessageID: "old-client", UserText: "old"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "old", Status: store.TurnCompleted, AssistantParts: store.TextPart("old answer")})
			if err != nil {
				t.Fatal(err)
			}
			old, _ := db.ListMessages(ctx, sid, 0)
			last := old[len(old)-1].ID
			_, err = db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "active", UserMessageID: "current", ClientMessageID: "current-client", UserText: "CURRENT_TASK"})
			if err != nil {
				t.Fatal(err)
			}
			in := store.AppendCompactSummaryInput{SessionID: sid, TurnID: "compact", MessageID: "summary", ClientMessageID: "compact-client",
				Provider: "mock", Model: "mock", Text: "summary", ExpectedLastMessageID: last,
				Metadata: store.CompactMessageMetadata([]string{old[0].ID, last}, []string{"current"})}
			meta, _ := store.CompactMetadataFromMessage(&store.Message{Metadata: in.Metadata})
			meta.BeforeInputEstimate, meta.AfterInputEstimate, meta.InputBudget = 10000, 7000, 6000
			in.Metadata, _ = json.Marshal(store.MessageMetadata{Compact: meta})
			beforeMessages, _ := db.ListMessages(ctx, sid, 0)
			beforeEvents, _ := db.EventsAfter(ctx, sid, 0, 0)
			if _, err := db.AppendCompactSummary(ctx, in); !errors.Is(err, store.ErrTurnRunning) {
				t.Fatalf("manual compact during turn: %v", err)
			}
			in.RunningTurnID = "other-session-turn"
			if _, err := db.AppendCompactSummary(ctx, in); !errors.Is(err, store.ErrTurnRunning) {
				t.Fatalf("wrong owner: %v", err)
			}
			in.RunningTurnID = "active"
			in.TurnID = "active"
			if _, err := db.AppendCompactSummary(ctx, in); !errors.Is(err, store.ErrHistoryChanged) {
				t.Fatalf("stale snapshot: %v", err)
			}
			afterMessages, _ := db.ListMessages(ctx, sid, 0)
			afterEvents, _ := db.EventsAfter(ctx, sid, 0, 0)
			if len(beforeMessages) != len(afterMessages) || len(beforeEvents) != len(afterEvents) {
				t.Fatal("rejected write changed messages/events")
			}
			in.ExpectedLastMessageID = "current"
			res, err := db.AppendCompactSummary(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if res.Turn.ID != "active" || res.Turn.Status != store.TurnRunning || res.Message.TurnID != "active" || res.Message.TurnIndex != 1 || res.Event.Kind != "turn.compacted" {
				t.Fatalf("compaction must be part of the active turn: %+v", res)
			}
			if running, err := db.RunningTurn(ctx, sid); err != nil || running.ID != "active" {
				t.Fatalf("summary ended active turn: %+v %v", running, err)
			}
			if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "active", Parts: store.TextPart("CONTINUED_OUTPUT")}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "active", Status: store.TurnCompleted}); err != nil {
				t.Fatal(err)
			}
			if backend == "sqlite" {
				if err := db.(*Store).Close(); err != nil {
					t.Fatal(err)
				}
				db = nil
				reopened, err := Open(path)
				if err != nil {
					t.Fatal(err)
				}
				db = reopened
			}
			all, _ := db.ListMessages(ctx, sid, 0)
			effective := contextbuilder.EffectiveMessages(all)
			visible := map[string]bool{}
			for _, msg := range effective {
				visible[msg.Text] = true
				if msg.Text == "summary" {
					got, ok := store.CompactMetadataFromMessage(msg)
					if !ok || got.BeforeInputEstimate != 10000 || got.AfterInputEstimate != 7000 || got.InputBudget != 6000 {
						t.Fatalf("lost canonical compaction estimates after restart: %+v", got)
					}
				}
			}
			if !visible["summary"] || !visible["CURRENT_TASK"] || !visible["CONTINUED_OUTPUT"] || visible["old"] {
				t.Fatalf("bad context after restart: %+v", visible)
			}
			events, _ := db.EventsAfter(ctx, sid, 0, 0)
			for i := 1; i < len(events); i++ {
				if events[i].Seq <= events[i-1].Seq {
					t.Fatal("non-monotonic event sequence")
				}
			}
		})
	}
}

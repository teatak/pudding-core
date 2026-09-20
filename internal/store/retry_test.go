package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestRetryTurnStores(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var st store.Store = memstore.New()
			var dbPath string
			if kind == "sqlite" {
				dbPath = filepath.Join(t.TempDir(), "retry.db")
				db, err := sqlitestore.Open(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				st = db
				t.Cleanup(func() { st.(*sqlitestore.Store).Close() })
			}
			for _, sid := range []string{"s", "other"} {
				if err := st.CreateSession(ctx, &store.Session{ID: sid, Title: "test", Provider: "mock", Model: "model"}); err != nil {
					t.Fatal(err)
				}
			}
			root, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "root", ClientMessageID: "input", UserMessageID: "user", UserText: "original"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = st.FinishTurn(ctx, store.FinishTurnInput{TurnID: root.Turn.ID, Status: store.TurnFailed, Error: "503"}); err != nil {
				t.Fatal(err)
			}
			in := store.BeginSystemTurnInput{SessionID: "s", RetryOfTurnID: "root", TurnID: "attempt", ClientMessageID: "retry", SystemMessageID: "instruction", Text: "retry"}
			wrong := in
			wrong.SessionID = "other"
			if _, err := st.BeginSystemTurn(ctx, wrong); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("cross session: %v", err)
			}
			if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "s", ClientMessageID: "queued", Text: "later"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.BeginSystemTurn(ctx, in); !errors.Is(err, store.ErrInvalidRetry) {
				t.Fatalf("retry bypassed pending queue: %v", err)
			}
			cancelled := store.QueuedInputCancelled
			if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "s", ClientMessageID: "queued", Status: &cancelled}); err != nil {
				t.Fatal(err)
			}
			before, err := st.ListMessages(ctx, "s", 0)
			if err != nil || len(before) != 1 {
				t.Fatalf("rejected retries wrote messages: %d %v", len(before), err)
			}
			var wg sync.WaitGroup
			results := make(chan *store.BeginSystemTurnResult, 12)
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					result, err := st.BeginSystemTurn(ctx, in)
					if err != nil {
						t.Error(err)
						return
					}
					results <- result
				}()
			}
			wg.Wait()
			close(results)
			started := 0
			for result := range results {
				if !result.Duplicate {
					started++
					if result.StartedEvent.RetryOfTurnID != "root" || result.StartedEvent.UserMessageID != "" {
						t.Fatal("retry event must link the parent without user input")
					}
				}
			}
			if started != 1 {
				t.Fatalf("started %d attempts", started)
			}
			otherRequest := in
			otherRequest.ClientMessageID = "another-request"
			otherRequest.TurnID = "another-attempt"
			if _, err := st.BeginSystemTurn(ctx, otherRequest); !errors.Is(err, store.ErrTurnRunning) {
				t.Fatalf("parallel retry started while running: %v", err)
			}
			msgs, err := st.ListMessages(ctx, "s", 0)
			if err != nil {
				t.Fatal(err)
			}
			users := 0
			for _, msg := range msgs {
				if msg.Role == store.RoleUser {
					users++
				}
			}
			if users != 1 {
				t.Fatalf("duplicate user messages: %d", users)
			}
			original, _ := st.GetConversationTurn(ctx, "s", "root")
			if original.Status != store.TurnFailed {
				t.Fatal("original terminal state changed")
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "attempt", Status: store.TurnFailed, Error: "503"}); err != nil {
				t.Fatal(err)
			}
			stale := in
			stale.ClientMessageID = "stale"
			stale.TurnID = "stale"
			if _, err := st.BeginSystemTurn(ctx, stale); !errors.Is(err, store.ErrInvalidRetry) {
				t.Fatalf("stale target: %v", err)
			}
			collision := in
			collision.RetryOfTurnID = "attempt"
			if _, err := st.BeginSystemTurn(ctx, collision); !errors.Is(err, store.ErrInvalidRetry) {
				t.Fatalf("idempotency target collision: %v", err)
			}
			in = store.BeginSystemTurnInput{SessionID: "s", RetryOfTurnID: "attempt", TurnID: "attempt2", ClientMessageID: "retry2", SystemMessageID: "instruction2", Text: "retry"}
			if _, err := st.BeginSystemTurn(ctx, in); err != nil {
				t.Fatal(err)
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "attempt2", Status: store.TurnCompleted, AssistantParts: store.TextPart("done")}); err != nil {
				t.Fatal(err)
			}
			if dbPath != "" {
				st.(*sqlitestore.Store).Close()
				reopened, err := sqlitestore.Open(dbPath)
				if err != nil {
					t.Fatal(err)
				}
				st = reopened
			}
			page, err := st.ListTurnsPage(ctx, "s", "", 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Turns) != 3 || page.HasMore || page.Turns[0].ID != "root" || page.Turns[2].RetryOfTurnID != "attempt" {
				t.Fatalf("retry chain split after restart: %+v", page)
			}
			next := in
			next.RetryOfTurnID = "attempt2"
			next.ClientMessageID = "retry3"
			next.TurnID = "attempt3"
			if _, err := st.BeginSystemTurn(ctx, next); !errors.Is(err, store.ErrInvalidRetry) {
				t.Fatalf("completed turn retry: %v", err)
			}
			duplicate, err := st.BeginSystemTurn(ctx, in)
			if err != nil || !duplicate.Duplicate {
				t.Fatalf("completed request replay: %+v %v", duplicate, err)
			}
			msgs, _ = st.ListMessages(ctx, "s", 0)
			if _, err := st.CloneSession(ctx, store.CloneSessionInput{SourceSessionID: "s", TargetSessionID: "clone", ThroughMessageID: msgs[len(msgs)-1].ID, TitleSuffix: " copy"}); err != nil {
				t.Fatal(err)
			}
			cloned, err := st.ListTurnsPage(ctx, "clone", "", 1)
			if err != nil || len(cloned.Turns) != 3 {
				t.Fatalf("clone chain: %+v %v", cloned, err)
			}
			if cloned.Turns[1].RetryOfTurnID != cloned.Turns[0].ID || cloned.Turns[2].RetryOfTurnID != cloned.Turns[1].ID {
				t.Fatal("clone leaked original retry IDs")
			}
		})
	}
}

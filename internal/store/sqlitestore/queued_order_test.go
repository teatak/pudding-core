package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func queuedIDs(inputs []*store.QueuedInput) []string {
	ids := make([]string, len(inputs))
	for i, input := range inputs {
		ids[i] = input.ClientMessageID
	}
	return ids
}

func TestQueuedOrderPersistsAndControlsPromotion(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "s")
	createTestSession(t, st, "other")
	for _, id := range []string{"a", "b", "c"} {
		if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "s", ClientMessageID: id, Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	original, _ := st.ListQueuedInputs(ctx, "s")
	for _, bad := range [][]string{{"a", "a", "c"}, {"b", "a"}, {"a", "b", "foreign"}} {
		if _, err := st.ReorderQueuedInputs(ctx, "s", bad); !errors.Is(err, store.ErrQueueChanged) {
			t.Fatalf("expected conflict: %v", err)
		}
		got, _ := st.ListQueuedInputs(ctx, "s")
		if !reflect.DeepEqual(queuedIDs(got), []string{"a", "b", "c"}) {
			t.Fatal("failed reorder changed queue")
		}
	}
	if _, err := st.ReorderQueuedInputs(ctx, "other", []string{"a", "b", "c"}); !errors.Is(err, store.ErrQueueChanged) {
		t.Fatal(err)
	}
	res, err := st.ReorderQueuedInputs(ctx, "s", []string{"c", "a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Events) != 3 || res.Events[2].Seq != res.Events[0].Seq+2 {
		t.Fatal("reorder events must be sequenced")
	}
	if !res.Inputs[1].CreatedAt.Equal(original[0].CreatedAt) {
		t.Fatal("reorder changed creation timestamp")
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "s", ClientMessageID: "d", Text: "d"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, _ := st.ListQueuedInputs(ctx, "s")
	if !reflect.DeepEqual(queuedIDs(got), []string{"c", "a", "b", "d"}) {
		t.Fatal(queuedIDs(got))
	}
	editing := store.QueuedInputEditing
	if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "s", ClientMessageID: "c", Status: &editing}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "s", TurnID: "blocked", UserMessageID: "blocked"}); !errors.Is(err, store.ErrQueueBlocked) {
		t.Fatal(err)
	}
	queued, text := store.QueuedInputQueued, "edited c"
	parts := []store.ContentPart{{Type: store.ContentPartText, Text: text}}
	if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "s", ClientMessageID: "c", Status: &queued, Text: &text, Parts: &parts}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c", "a", "b", "d"} {
		result, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "s", TurnID: "t" + id, UserMessageID: "m" + id})
		if err != nil {
			t.Fatal(err)
		}
		if result.Input.ClientMessageID != id {
			t.Fatalf("promoted %s, expected %s", result.Input.ClientMessageID, id)
		}
		if id == "c" && result.Input.Text != text {
			t.Fatal("edit lost")
		}
		if _, err := st.UpdateQueuedInput(ctx, store.UpdateQueuedInputInput{SessionID: "s", ClientMessageID: id, Status: &editing}); !errors.Is(err, store.ErrNotFound) {
			t.Fatal("promoted entry was editable")
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t" + id, Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQueuedReorderRollback(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "s")
	for _, id := range []string{"a", "b"} {
		if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "s", ClientMessageID: id, Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(`CREATE TRIGGER reject_queue_reorder BEFORE UPDATE OF sort_order ON queued_inputs WHEN NEW.client_message_id='a' BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReorderQueuedInputs(ctx, "s", []string{"b", "a"}); err == nil {
		t.Fatal("expected write failure")
	}
	got, _ := st.ListQueuedInputs(ctx, "s")
	if !reflect.DeepEqual(queuedIDs(got), []string{"a", "b"}) {
		t.Fatal("partial reorder committed")
	}
	var n int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM events WHERE session_id='s'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("partial reorder events committed: %d", n)
	}
}

func TestQueuedOrderMigrationFromV17(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "s")
	if _, err := st.db.Exec(`DROP INDEX queued_inputs_session_active;
ALTER TABLE queued_inputs DROP COLUMN sort_order;
CREATE INDEX queued_inputs_session_active ON queued_inputs(session_id,created_at) WHERE status IN ('queued','editing','cancelled');
INSERT INTO queued_inputs(session_id,client_message_id,text,status,created_at,updated_at) VALUES('s','a','a','queued',1,1),('s','b','b','editing',1,1),('s','c','c','queued',2,2);
PRAGMA user_version=17;
CREATE TRIGGER reject_migration BEFORE UPDATE ON queued_inputs BEGIN SELECT RAISE(ABORT,'test migration failure'); END;`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	if failed, err := Open(path); err == nil {
		failed.Close()
		t.Fatal("expected migration failure")
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	version, err := schemaVersion(db)
	if err != nil || version != 17 {
		t.Fatalf("migration not rolled back: %d %v", version, err)
	}
	var columns int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('queued_inputs') WHERE name='sort_order'`).Scan(&columns); err != nil || columns != 0 {
		t.Fatal("partial schema migration committed")
	}
	if _, err := db.Exec(`DROP TRIGGER reject_migration`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		reopened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got, err := reopened.ListQueuedInputs(context.Background(), "s")
		if err != nil || !reflect.DeepEqual(queuedIDs(got), []string{"a", "b", "c"}) || got[1].Status != store.QueuedInputEditing {
			t.Fatalf("migration lost queue: %+v %v", got, err)
		}
		reopened.Close()
	}
}

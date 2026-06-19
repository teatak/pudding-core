package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

func createTestSession(t *testing.T, st store.Store, id string) {
	t.Helper()
	if err := st.CreateSession(context.Background(), &store.Session{ID: id, Title: id, Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
}

func TestRenameDoesNotAffectRecentOrdering(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "older")
	time.Sleep(2 * time.Millisecond)
	createTestSession(t, st, "newer")
	time.Sleep(2 * time.Millisecond)

	title := "renamed older"
	if _, err := st.UpdateSession(context.Background(), "older", store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	pinned := true
	pinnedOrder := int64(7)
	updated, err := st.UpdateSession(context.Background(), "older", store.SessionUpdate{
		Pinned:      &pinned,
		PinnedOrder: &pinnedOrder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Pinned {
		t.Fatal("pinned flag was not persisted")
	}
	if updated.PinnedOrder != pinnedOrder {
		t.Fatalf("pinned order was not persisted: %d", updated.PinnedOrder)
	}
	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "newer" || sessions[1].ID != "older" {
		t.Fatalf("rename must not affect recent ordering: %+v", sessions)
	}
}

func beginTestTurn(t *testing.T, st store.Store, sessionID, turnID, msgID, clientID string) *store.BeginTurnResult {
	t.Helper()
	res, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		UserMessageID:   msgID,
		ClientMessageID: clientID,
		UserText:        "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestBeginTurnIdempotencyPrecedesRunningConflict(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")

	first := beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if first.Duplicate || first.StartedEvent == nil || first.StartedEvent.Seq != 1 {
		t.Fatalf("unexpected first begin: %+v", first)
	}

	duplicate, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_other",
		UserMessageID:   "msg_other",
		ClientMessageID: "client_1",
		UserText:        "retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Turn.ID != "turn_1" || duplicate.UserMessage.ID != "msg_1" || duplicate.StartedEvent != nil {
		t.Fatalf("duplicate should return original turn/message without event: %+v", duplicate)
	}

	_, err = st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_2",
		UserMessageID:   "msg_2",
		ClientMessageID: "client_2",
		UserText:        "conflict",
	})
	if !errors.Is(err, store.ErrTurnRunning) {
		t.Fatalf("want ErrTurnRunning, got %v", err)
	}
}

func TestFinishTurnTerminalStatesAndEventsAfter(t *testing.T) {
	st, _ := openTestStore(t)

	cases := []struct {
		sessionID   string
		turnID      string
		msgID       string
		clientID    string
		status      store.TurnStatus
		kind        event.Kind
		text        *string
		interrupted bool
		errorText   string
	}{
		{"sess_completed", "turn_completed", "msg_completed", "client_completed", store.TurnCompleted, event.TurnCompleted, strptr("done"), false, ""},
		{"sess_failed", "turn_failed", "msg_failed", "client_failed", store.TurnFailed, event.TurnFailed, strptr("partial"), true, "boom"},
		{"sess_cancelled", "turn_cancelled", "msg_cancelled", "client_cancelled", store.TurnCancelled, event.TurnCancelled, strptr("partial"), true, ""},
	}

	for _, tc := range cases {
		createTestSession(t, st, tc.sessionID)
		beginTestTurn(t, st, tc.sessionID, tc.turnID, tc.msgID, tc.clientID)
		res, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
			TurnID:        tc.turnID,
			Status:        tc.status,
			AssistantText: tc.text,
			Interrupted:   tc.interrupted,
			Error:         tc.errorText,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.FinalEvent.Kind != tc.kind || res.FinalEvent.Seq != 2 {
			t.Fatalf("unexpected final event: %+v", res.FinalEvent)
		}
		if tc.text != nil {
			if res.AssistantMessage == nil || res.FinalEvent.AssistantMessageID != res.AssistantMessage.ID {
				t.Fatalf("final event must reference assistant message: %+v", res)
			}
			if res.AssistantMessage.Interrupted != tc.interrupted {
				t.Fatalf("unexpected interrupted flag: %+v", res.AssistantMessage)
			}
		}

		afterStart, err := st.EventsAfter(context.Background(), tc.sessionID, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(afterStart) != 1 || afterStart[0].Kind != tc.kind || afterStart[0].Seq != 2 {
			t.Fatalf("EventsAfter should return final event only: %+v", afterStart)
		}
	}
}

func TestPersistenceAndSeqContinuation(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	text := "first"
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:        "turn_1",
		Status:        store.TurnCompleted,
		AssistantText: &text,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	sessions, err := reopened.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess_1" {
		t.Fatalf("sessions not persisted: %+v", sessions)
	}
	msgs, err := reopened.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Text != "first" {
		t.Fatalf("messages not persisted: %+v", msgs)
	}
	evs, err := reopened.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Seq != 2 {
		t.Fatalf("events not persisted: %+v", evs)
	}

	beginTestTurn(t, reopened, "sess_1", "turn_2", "msg_2", "client_2")
	evs, err = reopened.EventsAfter(context.Background(), "sess_1", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Seq != 3 || evs[0].Kind != event.TurnStarted {
		t.Fatalf("seq did not continue after reopen: %+v", evs)
	}
}

func TestTurnModelConfigPersists(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	cfg := []byte(`{"contextWindow":1000,"openai":{"temperature":0.6}}`)
	if _, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_1",
		UserMessageID:   "msg_1",
		ClientMessageID: "client_1",
		UserText:        "hello",
		Provider:        "default",
		Model:           "m1",
		ModelConfig:     cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	turn, err := reopened.RunningTurn(context.Background(), "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if string(turn.ModelConfig) != string(cfg) {
		t.Fatalf("model config not persisted: %s", turn.ModelConfig)
	}
}

func TestOpenMigratesAddedColumns(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL DEFAULT '',
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE turns (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    client_message_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('running','completed','failed','cancelled')),
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE (session_id, client_message_id)
);
`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if !hasColumn(t, st.db, "turns", "model_config") {
		t.Fatal("model_config column was not added")
	}
	if !hasColumn(t, st.db, "sessions", "last_activity_at") {
		t.Fatal("last_activity_at column was not added")
	}
	if !hasColumn(t, st.db, "sessions", "pinned") {
		t.Fatal("pinned column was not added")
	}
	if !hasColumn(t, st.db, "sessions", "pinned_order") {
		t.Fatal("pinned_order column was not added")
	}
}

func hasColumn(t *testing.T, db *sql.DB, table, column string) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == column {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return false
}

func TestDeleteSessionCascades(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	text := "done"
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:        "turn_1",
		Status:        store.TurnCompleted,
		AssistantText: &text,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession(context.Background(), "sess_1"); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"turns", "messages", "events"} {
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s should cascade delete, count=%d", table, count)
		}
	}
}

func TestSchemaDoesNotStoreConfigTables(t *testing.T) {
	st, _ := openTestStore(t)
	rows, err := st.db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == "settings" || name == "provider_profiles" {
			t.Fatalf("config table %q must not be in SQLite runtime store", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func strptr(s string) *string { return &s }

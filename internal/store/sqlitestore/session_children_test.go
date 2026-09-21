package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestChildSessionMigrationRollbackAndRestart(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "parent")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "turn", ClientMessageID: "input", UserMessageID: "message", UserText: "preserve history"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	before, err := st.GetConversationTurn(ctx, "parent", "turn")
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`DROP TABLE session_dispatches; DROP TABLE collaboration_stops; DROP TABLE session_children; PRAGMA user_version=20;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	migration := schemaMigrations[21]
	defer func() { schemaMigrations[21] = migration }()
	schemaMigrations[21] = func(tx *sql.Tx) error {
		if err := migration(tx); err != nil {
			return err
		}
		return errors.New("injected child migration failure")
	}
	if failed, err := Open(path); err == nil {
		failed.Close()
		t.Fatal("expected migration failure")
	}
	db = openMigrationTestDB(t, path)
	var version, tables int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name='session_children'`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if version != 20 || tables != 0 {
		t.Fatalf("migration rollback: version=%d tables=%d", version, tables)
	}
	db.Close()
	schemaMigrations[21] = migration
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if reopened != nil {
			reopened.Close()
		}
	}()
	turn, err := reopened.GetConversationTurn(ctx, "parent", "turn")
	if err != nil || !reflect.DeepEqual(turn, before) {
		t.Fatalf("migration changed canonical history: %+v %v", turn, err)
	}
	if err := reopened.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Title: "Child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	reopened.Close()
	reopened, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if parentID, err := reopened.ParentSessionID(ctx, "child"); err != nil || parentID != "parent" {
		t.Fatalf("relation lost on restart: %q %v", parentID, err)
	}
	if children, err := reopened.ListChildSessions(ctx, "parent"); err != nil || len(children) != 1 || children[0].Title != "Child" {
		t.Fatalf("children after restart: %+v %v", children, err)
	}
}

func TestChildSessionCreateIsAtomic(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "parent")
	if _, err := st.db.Exec(`CREATE TRIGGER reject_child BEFORE INSERT ON session_children BEGIN SELECT RAISE(ABORT,'injected relation failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err == nil {
		t.Fatal("expected atomic creation failure")
	}
	if _, err := st.GetSession(ctx, "child"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("relation failure left a session: %v", err)
	}
	if _, err := st.db.Exec(`DROP TRIGGER reject_child;`); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DELETE FROM sessions WHERE id='parent'`); err == nil {
		t.Fatal("database permitted orphaning a child")
	}
	if err := st.DeleteSession(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
}

func TestCollaborationDispatchSurvivesRestart(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "parent")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "parent", TurnID: "origin", ClientMessageID: "origin_input", UserMessageID: "origin_message", UserText: "work"}); err != nil {
		t.Fatal(err)
	}
	dispatch := store.DispatchChildInput{ParentSessionID: "parent", ParentTurnID: "origin", CallID: "call", Child: &store.Session{ID: "child", Title: "Child", Provider: "mock", Model: "mock"}, Input: store.QueueInputInput{SessionID: "child", ClientMessageID: "dispatch", Text: "work", Provider: "mock", Model: "mock"}}
	if _, err := st.DispatchChild(ctx, dispatch); err != nil {
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
	replay, err := reopened.DispatchChild(ctx, dispatch)
	if err != nil || !replay.Duplicate || replay.Session.ID != "child" {
		t.Fatalf("restart replay: %+v %v", replay, err)
	}
	if _, err := reopened.StopCollaboration(ctx, "parent"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	dispatch.CallID = "late"
	dispatch.Child = &store.Session{ID: "late", Provider: "mock", Model: "mock"}
	dispatch.Input.SessionID = "late"
	if _, err := again.DispatchChild(ctx, dispatch); !errors.Is(err, store.ErrCollaborationStopped) {
		t.Fatalf("restart lost stop barrier: %v", err)
	}
	queued, err := again.ListQueuedInputs(ctx, "child")
	if err != nil || len(queued) != 0 {
		t.Fatalf("restart resumed cancelled queue: %+v %v", queued, err)
	}
}

func TestCollaborationMigrationRollbackPreservesChildren(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "parent")
	if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`DROP TABLE session_dispatches; DROP TABLE collaboration_stops; PRAGMA user_version=21;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	migration := schemaMigrations[22]
	defer func() { schemaMigrations[22] = migration }()
	schemaMigrations[22] = func(tx *sql.Tx) error {
		if err := migration(tx); err != nil {
			return err
		}
		return errors.New("injected collaboration migration failure")
	}
	if failed, err := Open(path); err == nil {
		failed.Close()
		t.Fatal("expected migration failure")
	}
	db = openMigrationTestDB(t, path)
	var version, tables, children int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name IN ('session_dispatches','collaboration_stops')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM session_children WHERE child_session_id='child' AND parent_session_id='parent'`).Scan(&children); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if version != 21 || tables != 0 || children != 1 {
		t.Fatalf("rollback: version=%d tables=%d children=%d", version, tables, children)
	}
	schemaMigrations[22] = migration
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if parent, err := reopened.ParentSessionID(ctx, "child"); err != nil || parent != "parent" {
		t.Fatalf("migration lost child relationship: %q %v", parent, err)
	}
}

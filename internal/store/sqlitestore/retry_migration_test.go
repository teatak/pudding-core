package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestRetryMigrationPreservesDataAndRollsBack(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "s")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "failed", ClientMessageID: "user", UserMessageID: "message", UserText: "preserve me"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "failed", Status: store.TurnFailed, Error: "503"}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`DROP INDEX turns_one_retry; ALTER TABLE turns DROP COLUMN retry_of_turn_id; PRAGMA user_version=19;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	migration := schemaMigrations[20]
	defer func() { schemaMigrations[20] = migration }()
	schemaMigrations[20] = func(tx *sql.Tx) error {
		if err := migration(tx); err != nil {
			return err
		}
		return errors.New("injected retry migration failure")
	}
	if failed, err := Open(path); err == nil {
		failed.Close()
		t.Fatal("expected migration failure")
	}
	db = openMigrationTestDB(t, path)
	var version int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 19 {
		t.Fatal("failed migration advanced version")
	}
	var columnCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('turns') WHERE name='retry_of_turn_id'`).Scan(&columnCount); err != nil {
		t.Fatal(err)
	}
	if columnCount != 0 {
		t.Fatal("failed migration did not roll back column")
	}
	db.Close()
	schemaMigrations[20] = migration
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	turn, err := reopened.GetConversationTurn(ctx, "s", "failed")
	if err != nil || turn.Status != store.TurnFailed || turn.Messages[0].Text != "preserve me" || turn.RetryOfTurnID != "" {
		t.Fatalf("migration changed existing data: %+v %v", turn, err)
	}
	if _, err := reopened.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "s", RetryOfTurnID: "failed", TurnID: "retry", ClientMessageID: "retry", SystemMessageID: "instruction", Text: "retry"}); err != nil {
		t.Fatal(err)
	}
}

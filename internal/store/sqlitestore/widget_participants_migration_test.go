package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestWidgetParticipantMigrationPreservesStateAndRollsBack(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, s, "identity")
	page, err := s.OpenWidgetPage(ctx, w.ID, "library", w.ActiveRevision, "old")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "old", 0, []byte(`{"seats":{"black":"p"},"board":[1,0]}`)); err != nil {
		t.Fatal(err)
	}
	old := []byte(`{"participants":[{"id":"p","name":"Human","roles":["black"]}],"bindingVersion":1}`)
	if err = s.SetWidgetPageInteraction(ctx, "old", old); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("PRAGMA user_version=36"); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected failure")
	if err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := migrateWidgetParticipantIdentities(tx); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "SELECT interaction FROM widget_pages", string(old))
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "37")
	assertWorkspaceMigrationValue(t, s.db, "SELECT json_extract(interaction,'$.participants[0].id') FROM widget_pages", "p")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM widget_pages WHERE json_type(interaction,'$.participants[0].roles') IS NOT NULL", "0")
	restored, err := s.OpenWidgetPage(ctx, w.ID, "library", w.ActiveRevision, "new")
	if err != nil || restored.Version != 1 || string(restored.Data) != `{"seats":{"black":"p"},"board":[1,0]}` {
		t.Fatal(restored, err)
	}
}

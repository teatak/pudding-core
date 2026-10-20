package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/teatak/pudding-core/internal/store"
	"testing"
)

func TestWidgetOriginMigrationPreservesDataAndRollsBack(t *testing.T) {
	st, path := openTestStore(t)
	useLegacySheetFixture(t, st.db)
	w := createDataWidget(t, st, "kept")
	if _, err := st.db.Exec(`DROP TABLE widget_page_pins; DROP TABLE widget_pages; DROP INDEX studio_items_package; ALTER TABLE studio_items DROP COLUMN origin; PRAGMA user_version=33`); err != nil {
		t.Fatal(err)
	}
	err := runSchemaMigration(st.db, 34, func(tx *sql.Tx) error {
		if e := migrateWidgetOrigins(tx); e != nil {
			return e
		}
		return errors.New("injected")
	})
	if err == nil {
		t.Fatal("expected rollback")
	}
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", "33")
	assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM pragma_table_info('studio_items') WHERE name='origin'", "0")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetStudioItem(context.Background(), w.ID)
	if err != nil || got.ActiveRevision != w.ActiveRevision || got.Origin != nil {
		t.Fatal(got, err)
	}
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
}

func TestWidgetEditingCopyTransactionAndRestart(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, st, "original")
	origin := `{"registryURL":"https://hub.test/registry.json","packageID":"test/hub/widgets/todo","version":"1.0.0","packageHash":"hash","sourceHash":"source","copy":false}`
	if _, err := st.db.Exec(`UPDATE studio_items SET origin=? WHERE id=?`, origin, w.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 0, []byte(`{"tasks":["original"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER fail_copy BEFORE INSERT ON widget_data WHEN NEW.item_id='copy_failed' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ForkWidgetForEditing(ctx, w.ID, "copy_failed", "copy", w.Revision); err == nil {
		t.Fatal("expected transaction failure")
	}
	if _, err := st.GetStudioItem(ctx, "copy_failed"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("partial copy survived", err)
	}
	copy, err := st.ForkWidgetForEditing(ctx, w.ID, "copy", "copy", w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if copy.ActiveRevision != w.ActiveRevision || !copy.Origin.Copy {
		t.Fatal(copy)
	}
	snapshot, err := st.GetWidgetData(ctx, copy.ID)
	if err != nil || string(snapshot.Data) != `{"tasks":["original"]}` {
		t.Fatal(snapshot, err)
	}
	if _, err := st.WriteWidgetData(ctx, copy.ID, copy.ActiveRevision, "", snapshot.Version, []byte(`{"tasks":["custom"]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ForkWidgetForEditing(ctx, w.ID, copy.ID, "copy", w.Revision); err != nil {
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
	for id, want := range map[string]string{w.ID: `{"tasks":["original"]}`, copy.ID: `{"tasks":["custom"]}`} {
		data, err := st.GetWidgetData(ctx, id)
		if err != nil || string(data.Data) != want {
			t.Fatal(id, data, err)
		}
	}
}

func TestWidgetLegacyTitlesBackfillPreservesIdentityAndData(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, st, "legacy")
	origin := `{"registryURL":"https://hub.test/registry.json","packageID":"test/hub/widgets/todo","version":"1.0.0","packageHash":"same","sourceHash":"%s","copy":false}`
	if _, err := st.db.Exec(`UPDATE studio_items SET origin=? WHERE id=?`, fmt.Sprintf(origin, w.HeadRevision), w.ID); err != nil {
		t.Fatal(err)
	}
	w, _ = st.GetStudioItem(ctx, w.ID)
	if _, err := st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 0, []byte(`{"tasks":["keep"]}`)); err != nil {
		t.Fatal(err)
	}
	revision := w.Revision
	w.Origin.Titles = map[string]string{"en": "Todo", "zh-CN": "待办"}
	got, err := st.InstallWidgetPackage(ctx, w, revision)
	if err != nil || got.ID != w.ID || got.Revision != revision || got.Origin.Titles["zh-CN"] != "待办" {
		t.Fatal(got, err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err = st.GetStudioItem(ctx, w.ID)
	if err != nil || got.Origin.Titles["zh-CN"] != "待办" {
		t.Fatal(got, err)
	}
	data, err := st.GetWidgetData(ctx, w.ID)
	if err != nil || string(data.Data) != `{"tasks":["keep"]}` {
		t.Fatal(data, err)
	}
	mount := store.ProjectStudioMount(&store.StudioMount{}, got)
	if mount.Origin.Titles["zh-CN"] != "待办" {
		t.Fatal("mount lost titles")
	}
}

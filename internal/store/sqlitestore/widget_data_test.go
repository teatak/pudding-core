package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
)

func createDataWidget(t *testing.T, st *Store, id string) *store.StudioItem {
	t.Helper()
	ctx := context.Background()
	w, err := st.CreateStudioItem(ctx, &store.StudioItem{ID: id, Kind: "widget", Name: "Todo", CreatedAt: time.Now(), UpdatedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	w, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: id, Hash: hash, ClientRequestID: "source", CreatedAt: time.Now()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.PutWidgetBuildReceipt(ctx, id, hash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = hash
	w, err = st.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func TestWidgetDataSharedPersistentAndIsolated(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, st, "todo")
	createDataWidget(t, st, "other")
	for _, id := range []string{"left", "right"} {
		createTestSession(t, st, id)
		if _, err := st.OpenStudioItem(ctx, id, w.ID, id+"-mount"); err != nil {
			t.Fatal(err)
		}
	}
	initial, err := st.GetWidgetData(ctx, w.ID)
	if err != nil || initial.Version != 0 || string(initial.Data) != "{}" {
		t.Fatal(initial, err)
	}
	data := json.RawMessage(`{"todos":[{"id":"1","text":"Buy milk","done":false}]}`)
	saved, err := st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 0, data)
	if err != nil || saved.Version != 1 {
		t.Fatal(saved, err)
	}
	other, err := st.GetWidgetData(ctx, "other")
	if err != nil || other.Version != 0 {
		t.Fatal("data crossed widget boundary", other, err)
	}
	if _, err = st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 0, json.RawMessage(`{}`)); !errors.Is(err, store.ErrWidgetDataConflict) {
		t.Fatal("stale overwrite", err)
	}
	if _, err = st.WriteWidgetData(ctx, w.ID, strings.Repeat("b", 64), "", 1, json.RawMessage(`{}`)); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal("inactive source wrote data", err)
	}
	// Activating new code changes no durable data or data version.
	nextHash := strings.Repeat("b", 64)
	w, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: nextHash, ClientRequestID: "upgrade", CreatedAt: time.Now()}, w.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.PutWidgetBuildReceipt(ctx, w.ID, nextHash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = nextHash
	w, err = st.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	upgraded, err := st.GetWidgetData(ctx, w.ID)
	if err != nil || upgraded.Version != 1 || string(upgraded.Data) != string(data) {
		t.Fatal("source upgrade changed data", upgraded, err)
	}
	w.Name = "Renamed Todo"
	w, err = st.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetWidgetData(ctx, w.ID)
	if err != nil || got.Version != 1 || string(got.Data) != string(data) {
		t.Fatal("data lost on reopen", got, err)
	}
	archived, err := st.SetStudioItemArchived(ctx, w.ID, w.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.GetWidgetData(ctx, w.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("archived data accessible", err)
	}
	if _, err = st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 1, data); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("archived data writable", err)
	}
	w, err = st.SetStudioItemArchived(ctx, w.ID, archived.Revision, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err = st.GetWidgetData(ctx, w.ID)
	if err != nil || string(got.Data) != string(data) {
		t.Fatal("archive lost data", got, err)
	}
	if _, err = st.MarkStudioItemDeleted(ctx, w.ID, w.Revision); err != nil {
		t.Fatal(err)
	}
	if err = st.PurgeDeletedStudioItem(ctx, w.ID); err != nil {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM widget_data", "0")
}

func TestWidgetDataConcurrentWriteAndValidation(t *testing.T) {
	st, _ := openTestStore(t)
	w := createDataWidget(t, st, "todo")
	ctx := context.Background()
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Go(func() {
			_, err := st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 0, json.RawMessage(`{"done":true}`))
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	success, conflict := 0, 0
	for err := range errs {
		if err == nil {
			success++
		} else if errors.Is(err, store.ErrWidgetDataConflict) {
			conflict++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	for _, bad := range []string{"null", "[]", "42", "{bad}", `{"text":"` + strings.Repeat("a", contracts.Widget().MaxStorageBytes) + `"}`} {
		if _, err := st.WriteWidgetData(ctx, w.ID, w.ActiveRevision, "", 1, json.RawMessage(bad)); !errors.Is(err, store.ErrInvalidWidgetData) {
			t.Fatal("invalid data accepted", err)
		}
	}
	if _, err := st.GetWidgetData(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := st.CreateDocument(ctx, &store.StudioItem{ID: "doc", Kind: "doc", Name: "Doc"}, "body", store.ContentAuthor{Kind: "user"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetWidgetData(ctx, "doc"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("document has widget storage", err)
	}
}

func TestWidgetDataMigrationPreservesItemsAndRollsBack(t *testing.T) {
	st, path := openTestStore(t)
	w := createDataWidget(t, st, "existing")
	if _, err := st.db.Exec(`DROP TABLE widget_page_pins; DROP TABLE widget_pages; DROP TABLE widget_data; DROP INDEX studio_items_package; ALTER TABLE studio_items DROP COLUMN origin; PRAGMA user_version=32`); err != nil {
		t.Fatal(err)
	}
	err := runSchemaMigration(st.db, 33, func(tx *sql.Tx) error {
		if err := migrateWidgetData(tx); err != nil {
			return err
		}
		return errors.New("injected migration failure")
	})
	if err == nil {
		t.Fatal("expected migration failure")
	}
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", "32")
	assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM sqlite_master WHERE name='widget_data'", "0")
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.GetStudioItem(context.Background(), w.ID)
	if err != nil || got.ActiveRevision != w.ActiveRevision || got.Name != w.Name {
		t.Fatal("migration changed existing item", got, err)
	}
	data, err := st.GetWidgetData(context.Background(), w.ID)
	if err != nil || data.Version != 0 {
		t.Fatal(data, err)
	}
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", "37")
}

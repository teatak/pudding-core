package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWidgetPagePersistenceAndClose(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, s, "board")
	createTestSession(t, s, "other")
	page, err := s.OpenWidgetPage(ctx, w.ID, "library", w.ActiveRevision, "first")
	if err != nil {
		t.Fatal(err)
	}
	data := json.RawMessage(`{"board":[1,2,0],"turn":"black"}`)
	if _, err = s.WriteWidgetPage(ctx, page.ID, "first", 0, data); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "first", 0, data); !errors.Is(err, store.ErrWidgetDataConflict) {
		t.Fatal(err)
	}
	if err = s.SetWidgetPageInteraction(ctx, "first", json.RawMessage(`{"participants":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteWidgetData(ctx, w.ID, w.ActiveRevision, 0, json.RawMessage(`{"todos":["keep"]}`)); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.OpenWidgetPage(ctx, w.ID, "library", w.ActiveRevision, "second")
	if err != nil || restored.ID != page.ID || restored.Version != 1 || string(restored.Data) != string(data) || len(restored.Interaction) == 0 {
		t.Fatal(restored, err)
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "first", 1, data); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("stale target wrote", err)
	}
	other, err := s.OpenWidgetPage(ctx, w.ID, "other", w.ActiveRevision, "other-target")
	if err != nil || other.ID == page.ID || other.Version != 0 {
		t.Fatal(other, err)
	}
	if err = s.CloseWidgetPages(ctx, w.ID, "library"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "second", 1, data); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("closed page wrote", err)
	}
	reopened, err := s.OpenWidgetPage(ctx, w.ID, "library", w.ActiveRevision, "third")
	if err != nil || reopened.ID == page.ID || reopened.Version != 0 || len(reopened.Interaction) > 0 {
		t.Fatal(reopened, err)
	}
	if _, err = s.GetWidgetPageByTarget(ctx, "other-target"); err != nil {
		t.Fatal("closed another page", err)
	}
	shared, err := s.GetWidgetData(ctx, w.ID)
	if err != nil || shared.Version != 1 {
		t.Fatal("closed shared storage", shared, err)
	}
}
func TestWidgetPageMigrationRollbackAndPreservation(t *testing.T) {
	s, path := openTestStore(t)
	w := createDataWidget(t, s, "saved")
	if _, err := s.WriteWidgetData(context.Background(), w.ID, w.ActiveRevision, 0, json.RawMessage(`{"keep":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE widget_pages; PRAGMA user_version=34`); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("injected")
	if err := s.tx(context.Background(), func(tx *sql.Tx) error {
		if err := migrateWidgetPages(tx); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM sqlite_master WHERE name='widget_pages'", "0")
	s.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data, err := s.GetWidgetData(context.Background(), w.ID)
	if err != nil || string(data.Data) != `{"keep":true}` {
		t.Fatal(data, err)
	}
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "35")
}

func TestWidgetPageRevisionIsolationConcurrentWritesAndSessionCleanup(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, s, "drafts")
	createTestSession(t, s, "session")
	p, err := s.OpenWidgetPage(ctx, w.ID, "session", w.ActiveRevision, "v1")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.WriteWidgetPage(ctx, p.ID, "v1", 0, []byte(`{"value":1}`))
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	ok, conflicts := 0, 0
	for err := range results {
		if err == nil {
			ok++
		} else if errors.Is(err, store.ErrWidgetDataConflict) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatal(ok, conflicts)
	}
	next := strings.Repeat("b", 64)
	if _, err = s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: next, ClientRequestID: "revision2", CreatedAt: time.Now()}, w.HeadRevision); err != nil {
		t.Fatal(err)
	}
	preview, err := s.OpenWidgetPage(ctx, w.ID, "session", next, "preview")
	if err != nil || preview.Version != 0 || preview.ID == p.ID {
		t.Fatal(preview, err)
	}
	if _, err = s.WriteWidgetPage(ctx, preview.ID, "preview", 0, []byte(`{"value":2}`)); err != nil {
		t.Fatal(err)
	}
	old, err := s.OpenWidgetPage(ctx, w.ID, "session", w.ActiveRevision, "old-again")
	if err != nil || old.Version != 1 || string(old.Data) != `{"value":1}` {
		t.Fatal(old, err)
	}
	if err = s.DeleteSession(ctx, "session"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GetWidgetPageByTarget(ctx, "old-again"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("deleted session retained page", err)
	}
}

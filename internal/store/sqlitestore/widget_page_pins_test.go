package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestWidgetPagesPinVersionsAcrossUpgradeAndRestart(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, s, "board")
	old := w.ActiveRevision
	createTestSession(t, s, "new-session")
	// First activation may approve an already-rendered draft of this exact source.
	if _, err := s.OpenWidgetPage(ctx, w.ID, "library", old, "draft-target"); err != nil {
		t.Fatal(err)
	}
	if err := s.AuthorizeWidgetPage(ctx, w.ID, old, "draft-target"); err == nil {
		t.Fatal("unselected draft authorized")
	}
	selected, err := s.SelectWidgetPage(ctx, w.ID, "library", "")
	if err != nil || selected != old {
		t.Fatal(selected, err)
	}
	if err := s.AuthorizeWidgetPage(ctx, w.ID, old, "draft-target"); err != nil {
		t.Fatal("first activation revoked the same-source page", err)
	}
	page, err := s.OpenWidgetPage(ctx, w.ID, "library", old, "old-target")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "old-target", 0, []byte(`{"moves":[1,2]}`)); err != nil {
		t.Fatal(err)
	}
	next := strings.Repeat("b", 64)
	w, err = s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: next, ClientRequestID: "new-source", CreatedAt: time.Now()}, w.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutWidgetBuildReceipt(ctx, w.ID, next, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = next
	w, err = s.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if hash, err := s.SelectWidgetPage(ctx, w.ID, "library", ""); err != nil || hash != old {
		t.Fatal("upgraded an existing page", hash, err)
	}
	if hash, err := s.SelectWidgetPage(ctx, w.ID, "new-session", ""); err != nil || hash != next {
		t.Fatal(hash, err)
	}
	if err = s.AuthorizeWidgetPage(ctx, w.ID, old, "old-target"); err != nil {
		t.Fatal("old page revoked", err)
	}
	if _, err = s.WriteWidgetData(ctx, w.ID, old, "old-target", 0, []byte(`{"tasks":["old page"]}`)); err != nil {
		t.Fatal(err)
	}
	preview, err := s.OpenWidgetPage(ctx, w.ID, "new-session", old, "preview")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.AuthorizeWidgetPage(ctx, w.ID, old, preview.TargetID); err == nil {
		t.Fatal("historical preview authorized")
	}
	if _, err = s.WriteWidgetData(ctx, w.ID, old, "preview", 1, []byte(`{}`)); err == nil {
		t.Fatal("preview wrote shared data")
	}
	// A pending page-bound external action survives a default version change.
	action := &store.WidgetAction{ID: "pinned-action", ItemID: w.ID, ClientRequestID: "pinned-action", RequestHash: "intent", State: "prepared", CreatedAt: time.Now(), Spec: store.WidgetActionSpec{TargetID: "old-target", RevisionHash: old, BindingVersion: w.BindingVersion, ResourceRevision: w.Revision - 1}}
	if _, err = s.CreateWidgetAction(ctx, action); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimWidgetAction(ctx, w.ID, action.ID); err != nil {
		t.Fatal("upgrade revoked external action", err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if hash, err := s.SelectWidgetPage(ctx, w.ID, "library", ""); err != nil || hash != old {
		t.Fatal("restart lost pin", hash, err)
	}
	restored, err := s.OpenWidgetPage(ctx, w.ID, "library", old, "restored-target")
	if err != nil || restored.ID != page.ID || restored.Version != 1 {
		t.Fatal(restored, err)
	}
	if _, err = s.OpenWidgetPage(ctx, w.ID, "library", next, "candidate-preview"); err != nil {
		t.Fatal(err)
	}
	if err = s.AuthorizeWidgetPage(ctx, w.ID, next, "candidate-preview"); err == nil {
		t.Fatal("candidate authorized before activation on this page")
	}
	if _, err = s.SelectWidgetPage(ctx, w.ID, "library", next); err != nil {
		t.Fatal(err)
	}
	if err = s.AuthorizeWidgetPage(ctx, w.ID, next, "candidate-preview"); err != nil {
		t.Fatal("activation revoked the chosen preview", err)
	}
	if err = s.AuthorizeWidgetPage(ctx, w.ID, old, "restored-target"); err == nil {
		t.Fatal("switched page retained authority")
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "restored-target", 1, []byte(`{}`)); err == nil {
		t.Fatal("late write after switch")
	}
	if err = s.CloseWidgetPages(ctx, w.ID, "library"); err != nil {
		t.Fatal(err)
	}
	if hash, err := s.SelectWidgetPage(ctx, w.ID, "library", ""); err != nil || hash != next {
		t.Fatal(hash, err)
	}
	shared, err := s.GetWidgetData(ctx, w.ID)
	if err != nil || shared.Version != 1 {
		t.Fatal("upgrade cleared shared data", shared, err)
	}
}
func TestWidgetPagePinsMigrationPreservesStateAndRollsBack(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	w := createDataWidget(t, s, "saved")
	page, err := s.OpenWidgetPage(ctx, w.ID, "library", w.ActiveRevision, "old-target")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteWidgetPage(ctx, page.ID, "old-target", 0, []byte(`{"keep":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec(`DROP TABLE widget_page_pins; PRAGMA user_version=35`); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("rollback")
	if err = s.tx(ctx, func(tx *sql.Tx) error {
		if err := migrateWidgetPagePins(tx); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM sqlite_master WHERE name='widget_page_pins'", "0")
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if hash, err := s.SelectWidgetPage(ctx, w.ID, "library", ""); err != nil || hash != w.ActiveRevision {
		t.Fatal(hash, err)
	}
	saved, err := s.GetWidgetPageByTarget(ctx, "old-target")
	if err != nil || saved.Version != 1 || string(saved.Data) != `{"keep":true}` {
		t.Fatal(saved, err)
	}
}

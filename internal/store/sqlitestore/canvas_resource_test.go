package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestCanvasVersionsSurviveSessionDeletionAndRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "canvas.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "origin")
	now := time.Now().UTC()
	w, err := s.CreateCanvas(ctx, &store.Canvas{ID: "wb_1", Name: "Test", SourceSessionID: "origin", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	r := &store.CanvasRevision{CanvasID: w.ID, Hash: "hash_a", ClientRequestID: "save_a", CreatedAt: now}
	w, err = s.SaveCanvasRevision(ctx, r, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveCanvasRevision(ctx, r, ""); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	r.ClientRequestID = "save_b"
	w, err = s.SaveCanvasRevision(ctx, r, w.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveCanvasRevision(ctx, r, ""); err != nil {
		t.Fatalf("same package new request retry: %v", err)
	}
	r.Hash = "hash_b"
	if _, err = s.SaveCanvasRevision(ctx, r, w.HeadRevision); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatalf("request ID reused for other source: %v", err)
	}
	w.ActiveRevision = "hash_a"
	if _, err = s.UpdateCanvas(ctx, w, w.Revision); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatalf("activated unbuilt source: %v", err)
	}
	if err = s.PutCanvasBuildReceipt(ctx, w.ID, "hash_a", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w, err = s.UpdateCanvas(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateCanvas(ctx, w, w.Revision-1); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if err = s.DeleteSession(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, err = s.GetCanvas(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.SourceSessionID != "" || w.ActiveRevision != "hash_a" {
		t.Fatalf("wrong recovered canvas: %+v", w)
	}
	versions, err := s.ListCanvasRevisions(ctx, w.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("duplicate source versions: %+v %v", versions, err)
	}
}

func TestSaveCanvasRevisionIgnoresMetadataRevision(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "canvas.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	w, err := s.CreateCanvas(ctx, &store.Canvas{ID: "canvas_metadata", Name: "First", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w.ID, Hash: "first", ClientRequestID: "save-first", CreatedAt: now}, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Name = "Renamed"
	w, err = s.UpdateCanvas(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w.ID, Hash: "second", ClientRequestID: "save-second", CreatedAt: now}, "first")
	if err != nil || w.HeadRevision != "second" || w.Name != "Renamed" {
		t.Fatalf("source save conflicted with metadata change: %+v %v", w, err)
	}
	if _, err = s.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w.ID, Hash: "stale", ClientRequestID: "save-stale", CreatedAt: now}, "first"); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatalf("stale source base was accepted: %v", err)
	}
}

func TestCanvasMigrationFailureRollsBackAndPreservesOldData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v24.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "old_session")
	useV25CanvasFixture(t, s.db)
	if _, err = s.db.Exec(`DROP TABLE workbench_saves; DROP TABLE workbench_revisions; DROP TABLE workbenches; PRAGMA user_version=24;`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	migration := schemaMigrations[25]
	schemaMigrations[25] = func(tx *sql.Tx) error {
		if err := migration(tx); err != nil {
			return err
		}
		return errors.New("injected migration failure")
	}
	_, err = Open(dbPath)
	schemaMigrations[25] = migration
	if err == nil {
		t.Fatal("migration failure swallowed")
	}
	db := openMigrationTestDB(t, dbPath)
	var version, count int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil || version != 24 {
		t.Fatalf("failed migration advanced version: %d %v", version, err)
	}
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='canvases'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial migration remained: %d %v", count, err)
	}
	db.Close()
	s, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.GetSession(context.Background(), "old_session"); err != nil {
		t.Fatalf("old session lost: %v", err)
	}
}

func TestCanvasActionClaimAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "actions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	w, err := s.CreateCanvas(ctx, &store.Canvas{ID: "canvas-actions", Name: "Actions", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	w, err = s.SaveCanvasRevision(ctx, &store.CanvasRevision{CanvasID: w.ID, Hash: hash, ClientRequestID: "save", CreatedAt: now}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutCanvasBuildReceipt(ctx, w.ID, hash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = hash
	w, err = s.UpdateCanvas(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a := &store.CanvasAction{ID: "action", CanvasID: w.ID, ClientRequestID: "request", RequestHash: "intent", State: "prepared", CreatedAt: now, Spec: store.CanvasActionSpec{RevisionHash: hash, ResourceRevision: w.Revision, BindingVersion: w.BindingVersion, Request: json.RawMessage(`{}`)}}
	if _, err = s.CreateCanvasAction(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimCanvasAction(ctx, w.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimCanvasAction(ctx, w.ID, a.ID); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatal("duplicate claim", err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.GetCanvasAction(ctx, w.ID, a.ID)
	if err != nil || restored.State != "unknown" {
		t.Fatalf("restart %+v %v", restored, err)
	}
	if err = s.ClaimCanvasAction(ctx, w.ID, a.ID); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatal("unknown action retried", err)
	}
	a.ID = "another"
	a.RequestHash = "changed"
	if _, err = s.CreateCanvasAction(ctx, a); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatal("idempotency payload collision accepted", err)
	}
}

func TestCanvasLinksRetainOriginalAccounts(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	w, err := s.CreateCanvas(ctx, &store.Canvas{ID: "wb_links", Name: "Links", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	link := &store.CanvasLink{ID: "link_one", CanvasID: w.ID, Left: store.CanvasEntity{AppID: "mail", ConnectionID: "old-mail", EntityType: "message", EntityID: "one"}, Right: store.CanvasEntity{AppID: "issues", ConnectionID: "old-issues", EntityType: "issue", EntityID: "one"}, CreatedAt: now}
	if err = s.PutCanvasLink(ctx, link, w.Revision); err != nil {
		t.Fatal(err)
	}
	w.Bindings = map[string]string{"mail": "new-mail", "issues": "new-issues"}
	w.BindingVersion++
	w, err = s.UpdateCanvas(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	links, err := s.ListCanvasLinks(ctx, w.ID)
	if err != nil || len(links) != 1 || links[0].Left.ConnectionID != "old-mail" || links[0].Right.ConnectionID != "old-issues" {
		t.Fatalf("link changed account %+v %v", links, err)
	}
	if err = s.PutCanvasLink(ctx, link, w.Revision-1); !errors.Is(err, store.ErrCanvasConflict) {
		t.Fatal("stale link accepted", err)
	}
	if err = s.DeleteCanvasLink(ctx, w.ID, link.ID, w.Revision); err != nil {
		t.Fatal(err)
	}
	links, err = s.ListCanvasLinks(ctx, w.ID)
	if err != nil || len(links) != 0 {
		t.Fatal("link removal failed", err)
	}
}

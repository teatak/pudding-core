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

func TestWorkbenchVersionsSurviveSessionDeletionAndRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "workbench.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "origin")
	now := time.Now().UTC()
	w, err := s.CreateWorkbench(ctx, &store.Workbench{ID: "wb_1", Name: "Test", SourceSessionID: "origin", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	r := &store.WorkbenchRevision{WorkbenchID: w.ID, Hash: "hash_a", ClientRequestID: "save_a", CreatedAt: now}
	w, err = s.SaveWorkbenchRevision(ctx, r, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveWorkbenchRevision(ctx, r, 1); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	r.ClientRequestID = "save_b"
	w, err = s.SaveWorkbenchRevision(ctx, r, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveWorkbenchRevision(ctx, r, w.Revision-1); err != nil {
		t.Fatalf("same package new request retry: %v", err)
	}
	r.Hash = "hash_b"
	if _, err = s.SaveWorkbenchRevision(ctx, r, w.Revision); !errors.Is(err, store.ErrWorkbenchConflict) {
		t.Fatalf("request ID reused for other source: %v", err)
	}
	w.ActiveRevision = "hash_a"
	if _, err = s.UpdateWorkbench(ctx, w, w.Revision); !errors.Is(err, store.ErrWorkbenchConflict) {
		t.Fatalf("activated unbuilt source: %v", err)
	}
	if err = s.PutWorkbenchBuildReceipt(ctx, w.ID, "hash_a", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w, err = s.UpdateWorkbench(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateWorkbench(ctx, w, w.Revision-1); !errors.Is(err, store.ErrWorkbenchConflict) {
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
	w, err = s.GetWorkbench(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.SourceSessionID != "" || w.ActiveRevision != "hash_a" {
		t.Fatalf("wrong recovered workbench: %+v", w)
	}
	versions, err := s.ListWorkbenchRevisions(ctx, w.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("duplicate source versions: %+v %v", versions, err)
	}
}

func TestWorkbenchMigrationFailureRollsBackAndPreservesOldData(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "v24.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "old_session")
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
	if err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='workbenches'`).Scan(&count); err != nil || count != 0 {
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

func TestWorkbenchActionClaimAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "actions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	w, err := s.CreateWorkbench(ctx, &store.Workbench{ID: "workbench-actions", Name: "Actions", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	w, err = s.SaveWorkbenchRevision(ctx, &store.WorkbenchRevision{WorkbenchID: w.ID, Hash: hash, ClientRequestID: "save", CreatedAt: now}, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutWorkbenchBuildReceipt(ctx, w.ID, hash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = hash
	w, err = s.UpdateWorkbench(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a := &store.WorkbenchAction{ID: "action", WorkbenchID: w.ID, ClientRequestID: "request", RequestHash: "intent", State: "prepared", CreatedAt: now, Spec: store.WorkbenchActionSpec{RevisionHash: hash, ResourceRevision: w.Revision, BindingVersion: w.BindingVersion, Request: json.RawMessage(`{}`)}}
	if _, err = s.CreateWorkbenchAction(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimWorkbenchAction(ctx, w.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimWorkbenchAction(ctx, w.ID, a.ID); !errors.Is(err, store.ErrWorkbenchConflict) {
		t.Fatal("duplicate claim", err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.GetWorkbenchAction(ctx, w.ID, a.ID)
	if err != nil || restored.State != "unknown" {
		t.Fatalf("restart %+v %v", restored, err)
	}
	if err = s.ClaimWorkbenchAction(ctx, w.ID, a.ID); !errors.Is(err, store.ErrWorkbenchConflict) {
		t.Fatal("unknown action retried", err)
	}
	a.ID = "another"
	a.RequestHash = "changed"
	if _, err = s.CreateWorkbenchAction(ctx, a); !errors.Is(err, store.ErrWorkbenchConflict) {
		t.Fatal("idempotency payload collision accepted", err)
	}
}

func TestWorkbenchLinksRetainOriginalAccounts(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	w, err := s.CreateWorkbench(ctx, &store.Workbench{ID: "wb_links", Name: "Links", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	link := &store.WorkbenchLink{ID: "link_one", WorkbenchID: w.ID, Left: store.WorkbenchEntity{AppID: "mail", ConnectionID: "old-mail", EntityType: "message", EntityID: "one"}, Right: store.WorkbenchEntity{AppID: "issues", ConnectionID: "old-issues", EntityType: "issue", EntityID: "one"}, CreatedAt: now}
	if err = s.PutWorkbenchLink(ctx, link, w.Revision); err != nil {
		t.Fatal(err)
	}
	w.Bindings = map[string]string{"mail": "new-mail", "issues": "new-issues"}
	w.BindingVersion++
	w, err = s.UpdateWorkbench(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	links, err := s.ListWorkbenchLinks(ctx, w.ID)
	if err != nil || len(links) != 1 || links[0].Left.ConnectionID != "old-mail" || links[0].Right.ConnectionID != "old-issues" {
		t.Fatalf("link changed account %+v %v", links, err)
	}
	if err = s.PutWorkbenchLink(ctx, link, w.Revision-1); !errors.Is(err, store.ErrWorkbenchConflict) {
		t.Fatal("stale link accepted", err)
	}
	if err = s.DeleteWorkbenchLink(ctx, w.ID, link.ID, w.Revision); err != nil {
		t.Fatal(err)
	}
	links, err = s.ListWorkbenchLinks(ctx, w.ID)
	if err != nil || len(links) != 0 {
		t.Fatal("link removal failed", err)
	}
}

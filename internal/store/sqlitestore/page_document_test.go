package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestPageStructuredStorageBlockEditingAndUndo(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "blocks.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	user := store.ContentAuthor{Kind: "user"}
	createTestSession(t, s, "writer")
	_, err = s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "m", ClientMessageID: "input", UserText: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	ai := store.ContentAuthor{Kind: "session", SessionID: "writer", TurnID: "turn"}
	original := "One🙂\r\n\r\nSame\r\n\r\nSame\r\n\r\nEnd\r\n"
	initial, err := s.CreatePage(ctx, &store.StudioItem{ID: "page", Kind: "page", Name: "Blocks"}, original, user)
	if err != nil {
		t.Fatal(err)
	}
	ids := initial.Structure.Blocks
	var raw, format string
	if err = s.db.QueryRow(`SELECT body,content_format FROM studio_item_content WHERE item_id='page'`).Scan(&raw, &format); err != nil {
		t.Fatal(err)
	}
	if format != store.PageDocumentFormat || raw == original {
		t.Fatal("body is not a structured snapshot")
	}
	human := strings.Replace(original, "One🙂", "Human🙂", 1)
	current, err := s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "human", ExpectedHash: &initial.ContentHash, Body: &human, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	// Stale whole-body hash is harmless: target block versions remain current.
	begin := store.PageEditRequest{Action: "begin", ClientRequestID: "begin", ExpectedHash: initial.ContentHash, Blocks: []store.PageBlockTarget{{BlockID: ids[1].ID, Version: ids[1].Version}}, Author: ai}
	op, err := s.ChangePageEdit(ctx, "page", begin)
	if err != nil {
		t.Fatal(err)
	}
	later := strings.Replace(human, "End", "User footer", 1)
	current, err = s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "later", ExpectedHash: &current.ContentHash, Body: &later, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	// Rewrite to identical text elsewhere; the operation, not text search, owns ID.
	replacement := "Human🙂"
	written, err := s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "commit", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &replacement, Author: ai})
	if err != nil {
		t.Fatal(err)
	}
	if written.Structure.Blocks[0].ID != ids[0].ID || written.Structure.Blocks[1].ID != ids[1].ID || written.Structure.Blocks[2].ID != ids[2].ID {
		t.Fatal("identity stolen by duplicate text")
	}
	if !strings.Contains(written.Body, "User footer") {
		t.Fatal("unrelated write lost")
	}
	begin.ClientRequestID = "stale"
	var conflict *store.ContentConflict
	if _, err = s.ChangePageEdit(ctx, "page", begin); !errors.As(err, &conflict) {
		t.Fatal("stale version accepted", err)
	}
	undone, err := s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "undo", UndoRevision: written.RevisionID, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	if undone.Body != later || undone.Structure.Blocks[1].ID != ids[1].ID {
		t.Fatal("undo lost identity or human edits")
	}
	// Delete then undo must revive the deleted block with a new version.
	target := undone.Structure.Blocks[1]
	op, err = s.ChangePageEdit(ctx, "page", store.PageEditRequest{Action: "begin", ClientRequestID: "delete-begin", Blocks: []store.PageBlockTarget{{BlockID: target.ID, Version: target.Version}}, Author: ai})
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	deleted, err := s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "delete", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &empty, Author: ai})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := s.WritePage(ctx, "page", store.PageWrite{ClientRequestID: "undo-delete", UndoRevision: deleted.RevisionID, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != later || restored.Structure.Blocks[1].ID != target.ID || restored.Structure.Blocks[1].Version <= target.Version {
		t.Fatalf("undo failed to revive block: %+v", restored.Structure.Blocks)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := s.GetPage(ctx, "page")
	if err != nil || !reflect.DeepEqual(reopened, restored) {
		t.Fatal("restart changed snapshot", err)
	}
	revision, err := s.GetStudioItemRevision(ctx, "page", written.RevisionID)
	if err != nil || revision.Body == nil || *revision.Body != written.Body {
		t.Fatal("history leaked JSON", err)
	}
}

func TestPageV44MigrationPreservesHistoryAndMarkdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	db := openMigrationTestDB(t, path)
	schema := strings.ReplaceAll(store.SchemaSQL, ",\n    content_format TEXT NOT NULL DEFAULT 'plain'", "")
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	body := "{\"schemaVersion\":1,\"blocks\":[]}\r\n\r\n```go\r\ncode\r\n```\r\n"
	hash := store.ContentHash(body)
	if _, err := db.Exec(`INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES('old','page','Legacy',1,'rev','','{}',1,0,1,1);
PRAGMA user_version=44;`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO studio_item_content(item_id,body,content_hash) VALUES('old',?,?)`, body, hash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind) VALUES('old','rev','','create',1,'',?,?,'user')`, body, hash); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	page, err := s.GetPage(context.Background(), "old")
	if err != nil || page.Body != body || page.ContentHash != hash || page.RevisionID != "rev" || page.Structure == nil {
		t.Fatal("migration changed logical content", err)
	}
	var saved, format string
	if err = s.db.QueryRow(`SELECT content,content_format FROM studio_item_revisions WHERE item_id='old'`).Scan(&saved, &format); err != nil || saved != body || format != "plain" {
		t.Fatal("immutable history rewritten", err)
	}
	current, err := s.WritePage(context.Background(), "old", store.PageWrite{ClientRequestID: "restore", RestoreRevision: "rev", ExpectedHash: &hash, Author: store.ContentAuthor{Kind: "user"}})
	if err != nil || current.Body != body || current.Structure.Blocks[0].ID != page.Structure.Blocks[0].ID {
		t.Fatal("legacy restore lost source or identity", err)
	}
}

func TestPageStructuredMigrationRollsBackOnConversionFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollback.db")
	db := openMigrationTestDB(t, path)
	schema := strings.ReplaceAll(store.SchemaSQL, ",\n    content_format TEXT NOT NULL DEFAULT 'plain'", "")
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES('old','page','Legacy',1,'rev','','{}',1,0,1,1);
 INSERT INTO studio_item_content(item_id,body,content_hash) VALUES('old','Original','hash');
 CREATE TRIGGER fail_page_conversion BEFORE UPDATE ON studio_item_content BEGIN SELECT RAISE(ABORT,'conversion fixture'); END;
 PRAGMA user_version=44;`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if s, err := Open(path); err == nil {
		s.Close()
		t.Fatal("failed migration accepted")
	}
	db = openMigrationTestDB(t, path)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "44")
	assertWorkspaceMigrationValue(t, db, "SELECT body FROM studio_item_content WHERE item_id='old'", "Original")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM pragma_table_info('studio_item_content') WHERE name='content_format'", "0")
	if _, err := db.Exec(`DROP TRIGGER fail_page_conversion`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
}

package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

const pageEditLegacyBody = "前言🙂\r\n\r\n目标段落。\r\n"

func pageEditLegacyDB(t *testing.T, path string, version int) *sql.DB {
	t.Helper()
	// Frozen schema from 0892414c6a7443f16cd4a06453861af3b583109d, before
	// the edit tables existed. Do not derive it from today's SchemaSQL.
	schema, err := os.ReadFile("testdata/schema-v39.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(schema)); got != "054017d2e390bbc3483527e4efef6f7ac5606559abccc2231ac1634fe8137fbf" {
		t.Fatal("historical v39 schema changed", got)
	}
	db := openMigrationTestDB(t, path)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	hash := store.ContentHash(pageEditLegacyBody)
	if _, err := db.Exec(`
INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
VALUES('doc_legacy','doc','Legacy',1,'legacy-revision','','{}',1,0,1,1);
INSERT INTO studio_item_content(item_id,body,content_hash) VALUES('doc_legacy',?,?);
INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind)
VALUES('doc_legacy','legacy-revision','','create',1,'',?,?,'user');
INSERT INTO studio_item_saves(item_id,client_request_id,hash,request_hash)
VALUES('doc_legacy','create','legacy-revision','legacy-request');
PRAGMA user_version=39;`, pageEditLegacyBody, hash, pageEditLegacyBody, hash); err != nil {
		t.Fatal(err)
	}
	if version == 43 {
		// Reproduce the defective upgrade, including its recorded v43 marker.
		for _, next := range []int{40, 41, 42, 43} {
			if err := runSchemaMigration(db, next, schemaMigrations[next]); err != nil {
				t.Fatal(err)
			}
		}
	}
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", fmt.Sprint(version))
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name IN ('studio_document_edits','studio_document_edit_requests')", "0")
	return db
}

func TestPageEditMigrationRepairsSkippedTables(t *testing.T) {
	for _, version := range []int{39, 43} {
		t.Run(fmt.Sprintf("v%d", version), func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "pudding.db")
			db := pageEditLegacyDB(t, path, version)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if s != nil {
					s.Close()
				}
			}()
			assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
			assertWorkspaceMigrationValue(t, s.db, "PRAGMA foreign_keys", "1")
			assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM pragma_foreign_key_check", "0")
			assertWorkspaceMigrationValue(t, s.db, "SELECT kind FROM studio_items WHERE id='doc_legacy'", "page")
			assertWorkspaceMigrationValue(t, s.db, "SELECT request_hash FROM studio_item_saves WHERE item_id='doc_legacy'", "legacy-request")
			doc, err := s.GetNote(ctx, "doc_legacy")
			if err != nil || doc.Body != pageEditLegacyBody || doc.ContentHash != store.ContentHash(pageEditLegacyBody) || doc.RevisionID != "legacy-revision" {
				t.Fatal("migration changed the Page snapshot", doc, err)
			}
			user := store.ContentAuthor{Kind: "user"}
			body := pageEditLegacyBody + "Human footer\r\n"
			write := store.NoteWrite{ClientRequestID: "save", Body: &body, ExpectedHash: &doc.ContentHash, Author: user}
			saved, err := s.WriteNote(ctx, doc.ItemID, write)
			if err != nil {
				t.Fatal("save after migration", err)
			}
			begin := store.NoteEditRequest{Action: "begin", ClientRequestID: "begin", ExpectedHash: saved.ContentHash, Target: "目标段落。", Author: user}
			op, err := s.ChangeNoteEdit(ctx, doc.ItemID, begin)
			if err != nil {
				t.Fatal("begin after migration", err)
			}
			state, err := s.GetNoteEditState(ctx, doc.ItemID)
			if err != nil || len(state.Operations) != 1 || state.Operations[0].ID != op.ID {
				t.Fatal("read edit state after migration", state, err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			retry, err := s.ChangeNoteEdit(ctx, doc.ItemID, begin)
			if err != nil || *retry != *op {
				t.Fatal("lease retry changed after restart", retry, err)
			}
			replacement := "完成。"
			committed, err := s.WriteNote(ctx, doc.ItemID, store.NoteWrite{ClientRequestID: "commit", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &replacement, Author: user})
			if err != nil || committed.Body != strings.Replace(body, begin.Target, replacement, 1) {
				t.Fatal("commit after restart", committed, err)
			}
			replayed, err := s.WriteNote(ctx, doc.ItemID, write)
			if err != nil || *replayed != *saved {
				t.Fatal("save retry lost its original result", replayed, err)
			}
			current, err := s.GetNote(ctx, doc.ItemID)
			if err != nil || *current != *committed {
				t.Fatal("save replay moved the head", current, err)
			}
			revision, err := s.GetStudioItemRevision(ctx, doc.ItemID, "legacy-revision")
			if err != nil || revision.Body == nil || *revision.Body != pageEditLegacyBody || revision.ContentHash != doc.ContentHash {
				t.Fatal("historical revision changed", revision, err)
			}
			assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_item_revisions WHERE item_id='doc_legacy'", "3")
		})
	}
}

func TestPageEditMigrationPreservesExistingLeaseAndRequest(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	user := store.ContentAuthor{Kind: "user"}
	doc, err := s.CreateNote(ctx, &store.StudioItem{ID: "page", Kind: store.StudioItemKindPage, Name: "Page"}, pageEditLegacyBody, user)
	if err != nil {
		t.Fatal(err)
	}
	begin := store.NoteEditRequest{Action: "begin", ClientRequestID: "begin", ExpectedHash: doc.ContentHash, Target: "目标段落。", Author: user}
	op, err := s.ChangeNoteEdit(ctx, doc.ItemID, begin)
	if err != nil {
		t.Fatal(err)
	}
	var payload, requestHash, result string
	if err := s.db.QueryRow(`SELECT payload FROM studio_document_edits WHERE id=?`, op.ID).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT request_hash,result FROM studio_document_edit_requests WHERE item_id=? AND client_request_id='begin'`, doc.ItemID).Scan(&requestHash, &result); err != nil {
		t.Fatal(err)
	}
	// The canonical schema is unchanged from v43. Also exercise index repair
	// while retaining both existing tables and their original record bytes.
	if _, err := s.db.Exec(`DROP INDEX studio_document_edits_item; PRAGMA user_version=43;`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, s.db, "SELECT payload FROM studio_document_edits", payload)
	assertWorkspaceMigrationValue(t, s.db, "SELECT request_hash FROM studio_document_edit_requests", requestHash)
	assertWorkspaceMigrationValue(t, s.db, "SELECT result FROM studio_document_edit_requests", result)
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM sqlite_master WHERE type='index' AND name='studio_document_edits_item'", "1")
	retry, err := s.ChangeNoteEdit(ctx, doc.ItemID, begin)
	if err != nil || *retry != *op {
		t.Fatal("migration changed the idempotent lease result", retry, err)
	}
	var locked *store.NoteLocked
	if _, err := s.WriteNote(ctx, doc.ItemID, store.NoteWrite{ClientRequestID: "conflicting", Edits: []store.NoteEdit{{Old: "目标段落。", New: "wrong"}}, Author: user}); !errors.As(err, &locked) {
		t.Fatal("migration lost the active lease", err)
	}
	replacement := "完成。"
	if _, err := s.WriteNote(ctx, doc.ItemID, store.NoteWrite{ClientRequestID: "commit", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &replacement, Author: user}); err != nil {
		t.Fatal("retained lease cannot commit", err)
	}
}

func TestPageEditMigrationFailureRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := pageEditLegacyDB(t, path, 43)
	sentinel := errors.New("after Page edit repair")
	err := runSchemaMigration(db, 44, func(tx *sql.Tx) error {
		if err := migratePageEdits(tx); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "43")
	assertWorkspaceMigrationValue(t, db, "PRAGMA foreign_keys", "1")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name IN ('studio_document_edits','studio_document_edit_requests','studio_document_edits_item')", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT body FROM studio_item_content WHERE item_id='doc_legacy'", pageEditLegacyBody)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(path)
	if err != nil {
		t.Fatal("retry after rollback", err)
	}
	defer s.Close()
	if _, err := s.GetNoteEditState(context.Background(), "doc_legacy"); err != nil {
		t.Fatal("retry did not repair Page editing", err)
	}
}

func TestPageEditSchemaContractRequiresStorage(t *testing.T) {
	for _, drop := range []string{
		"DROP TABLE studio_document_edits",
		"DROP TABLE studio_document_edit_requests",
		"DROP INDEX studio_document_edits_item",
	} {
		t.Run(drop, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pudding.db")
			s, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(drop); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = Open(path)
			if s != nil {
				s.Close()
			}
			if !errors.Is(err, ErrUnsupportedSchema) {
				t.Fatal("accepted current schema without Page editing storage", err)
			}
		})
	}
}

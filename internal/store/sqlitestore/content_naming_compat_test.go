package sqlitestore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestContentNamingPreservesHistoricalStorageAndRetries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	useLegacySheetFixture(t, s.db)
	fixture, err := os.ReadFile("testdata/content_naming_legacy.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, "PRAGMA ignore_check_constraints=ON"); err != nil {
		t.Fatal(err)
	}
	if err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
			return err
		}
		// The frozen dump predates explicit format columns; keep its value bytes.
		sqlText := strings.ReplaceAll(string(fixture), `INSERT INTO "studio_item_content" VALUES`, `INSERT INTO "studio_item_content"(item_id,body,content_hash) VALUES`)
		sqlText = strings.ReplaceAll(sqlText, `INSERT INTO "studio_item_revisions" VALUES`, `INSERT INTO "studio_item_revisions"(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind,author_session_id,author_turn_id) VALUES`)
		_, err := tx.ExecContext(ctx, sqlText)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/content_naming_legacy.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Note      json.RawMessage `json:"note"`
		PageWrite store.PageWrite `json:"noteWrite"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	checkJSON := func(got any, want json.RawMessage) {
		t.Helper()
		if snapshot, ok := got.(*store.ContentSnapshot); ok {
			legacyView := *snapshot
			legacyView.Structure = nil
			got = &legacyView
		}
		raw, err := json.Marshal(got)
		var compact bytes.Buffer
		if err != nil || json.Compact(&compact, want) != nil || !bytes.Equal(raw, compact.Bytes()) {
			t.Fatalf("historical wire payload changed: %s; error: %v", raw, err)
		}
	}
	note, err := s.GetPage(ctx, "doc_legacy")
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(note, legacy.Note)
	legacy.PageWrite.Author = store.ContentAuthor{Kind: "user"}
	retriedPage, err := s.WritePage(ctx, note.ItemID, legacy.PageWrite)
	if err != nil {
		t.Fatal("historical note request fingerprint changed", err)
	}
	checkJSON(retriedPage, legacy.Note)
	revisions, err := s.ListStudioItemRevisions(ctx, note.ItemID)
	if err != nil || len(revisions) != 2 {
		t.Fatal("retry changed historical versions", note.ItemID, revisions, err)
	}
	for _, revision := range revisions {
		full, err := s.GetStudioItemRevision(ctx, note.ItemID, revision.Hash)
		if err != nil || full.Body == nil || store.ContentHash(*full.Body) != full.ContentHash {
			t.Fatal("historical body/hash changed", note.ItemID, err)
		}
	}
}

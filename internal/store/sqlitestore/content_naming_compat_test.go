package sqlitestore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

// These fixtures were written by the implementation before the internal rename,
// not generated through the renamed models. In particular, request fingerprints
// and revision IDs must stay byte-compatible after reopening the old content.
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
		_, err := tx.ExecContext(ctx, string(fixture))
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
		Note          json.RawMessage     `json:"note"`
		Document      json.RawMessage     `json:"document"`
		Blob          store.DocumentBlob  `json:"blob"`
		NoteWrite     store.NoteWrite     `json:"noteWrite"`
		DocumentWrite store.DocumentWrite `json:"documentWrite"`
	}
	if err := json.Unmarshal(data, &legacy); err != nil {
		t.Fatal(err)
	}
	checkJSON := func(got any, want json.RawMessage) {
		t.Helper()
		raw, err := json.Marshal(got)
		var compact bytes.Buffer
		if err != nil || json.Compact(&compact, want) != nil || !bytes.Equal(raw, compact.Bytes()) {
			t.Fatalf("historical wire payload changed: %s; error: %v", raw, err)
		}
	}
	note, err := s.GetNote(ctx, "doc_legacy")
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(note, legacy.Note)
	document, err := s.GetDocument(ctx, "rich_legacy")
	if err != nil {
		t.Fatal(err)
	}
	checkJSON(document, legacy.Document)
	blob, err := s.GetDocumentBlob(ctx, document.ItemID, legacy.Blob.Hash)
	if err != nil || blob.Type != legacy.Blob.Type || !bytes.Equal(blob.Data, legacy.Blob.Data) {
		t.Fatal("historical asset changed", err)
	}
	legacy.NoteWrite.Author = store.ContentAuthor{Kind: "user"}
	retriedNote, err := s.WriteNote(ctx, note.ItemID, legacy.NoteWrite)
	if err != nil {
		t.Fatal("historical note request fingerprint changed", err)
	}
	checkJSON(retriedNote, legacy.Note)
	retriedDocument, err := s.WriteDocument(ctx, document.ItemID, legacy.DocumentWrite)
	if err != nil {
		t.Fatal("historical document request fingerprint changed", err)
	}
	checkJSON(retriedDocument, legacy.Document)
	for _, id := range []string{note.ItemID, document.ItemID} {
		revisions, err := s.ListStudioItemRevisions(ctx, id)
		if err != nil || len(revisions) != 2 {
			t.Fatal("retry changed historical versions", id, revisions, err)
		}
		for _, revision := range revisions {
			full, err := s.GetStudioItemRevision(ctx, id, revision.Hash)
			if err != nil || full.Body == nil || store.ContentHash(*full.Body) != full.ContentHash {
				t.Fatal("historical body/hash changed", id, err)
			}
		}
	}
	if _, err := s.GetNote(ctx, document.ItemID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("document routed to note storage", err)
	}
	if _, err := s.GetDocument(ctx, note.ItemID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("note routed to document storage", err)
	}
}

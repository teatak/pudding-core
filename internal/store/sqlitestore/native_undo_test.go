package sqlitestore

import (
	"context"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"testing"
)

func TestNativeUndoIsAtomicIdempotentAndAttributed(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	user := store.ContentAuthor{Kind: "user"}
	createTestSession(t, s, "writer")
	if _, err := s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "edit"}); err != nil {
		t.Fatal(err)
	}
	ai := store.ContentAuthor{Kind: "session", SessionID: "writer", TurnID: "turn"}
	doc, err := s.CreateDocument(ctx, &store.StudioItem{ID: "doc", Kind: "doc", Name: "Doc"}, "First\n\nLast", user)
	if err != nil {
		t.Fatal(err)
	}
	edited, err := s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "ai", Edits: []store.DocumentEdit{{Old: "First", New: "Revised"}}, Author: ai})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "human", Edits: []store.DocumentEdit{{Old: "Last", New: "Human"}}, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	request := store.DocumentWrite{ClientRequestID: "undo", UndoRevision: edited.RevisionID, Author: user}
	undone, err := s.WriteDocument(ctx, "doc", request)
	if err != nil || undone.Body != "First\n\nHuman" || undone.RevisionID == doc.RevisionID {
		t.Fatal(undone, err)
	}
	retry, err := s.WriteDocument(ctx, "doc", request)
	if err != nil || retry.RevisionID != undone.RevisionID {
		t.Fatal(retry, err)
	}
	revisions, _ := s.ListStudioItemRevisions(ctx, "doc")
	if len(revisions) != 4 || revisions[0].Author.Kind != "user" {
		t.Fatal(revisions)
	}
	if _, err = s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "again", UndoRevision: edited.RevisionID, Author: user}); err == nil {
		t.Fatal("undo replay accepted")
	}
	_, err = s.CreateTable(ctx, &store.StudioItem{ID: "table", Kind: "table", Name: "Table"}, tableFixture(), user)
	if err != nil {
		t.Fatal(err)
	}
	table, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "ai", Operations: []store.TableOperation{tableCell("r", "a", "AI")}, Author: ai})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "human", Operations: []store.TableOperation{tableCell("r", "b", float64(9))}, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	out, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "undo", UndoRevision: table.RevisionID, RowID: "r", Author: user})
	if err != nil || out.Body.Rows[0].Cells["a"] != "Original" || out.Body.Rows[0].Cells["b"] != float64(9) {
		t.Fatal(out, err)
	}
	var conflict *store.ContentConflict
	if _, err = s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "again", UndoRevision: table.RevisionID, RowID: "r", Author: user}); !errors.As(err, &conflict) {
		t.Fatal(err)
	}
	deleted, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "delete", Operations: []store.TableOperation{{Kind: "delete_row", RowID: "r"}}, Author: ai})
	if err != nil {
		t.Fatal(err)
	}
	out, err = s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "undo-delete", UndoRevision: deleted.RevisionID, Author: user})
	if err != nil || len(out.Body.Rows) != 1 {
		t.Fatal(out, err)
	}
	if _, err = s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "cross-item", UndoRevision: edited.RevisionID, Author: user}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
}

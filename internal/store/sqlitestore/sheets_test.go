package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"path/filepath"
	"reflect"
	"testing"
)

func sheetFixture() store.SheetBody {
	return store.SheetBody{Columns: []store.SheetColumn{{ID: "a", Name: "Text", Type: "text"}, {ID: "b", Name: "Number", Type: "number"}}, Rows: []store.SheetRow{{ID: "r", Cells: map[string]any{"a": "Original", "b": float64(1)}}}}
}
func sheetCell(row, col string, value any) store.SheetOperation {
	raw, _ := json.Marshal(value)
	return store.SheetOperation{Kind: "set_cell", RowID: row, ColumnID: col, Value: raw}
}
func TestSheetConcurrentEditsAtomicityAndHistory(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "table.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	user := store.ContentAuthor{Kind: "user"}
	createTestSession(t, s, "writer")
	if _, err := s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "edit"}); err != nil {
		t.Fatal(err)
	}
	ai := store.ContentAuthor{Kind: "session", SessionID: "writer", TurnID: "turn"}
	initial, err := s.CreateSheet(ctx, &store.StudioItem{ID: "table", Kind: "sheet", Name: "Data"}, sheetFixture(), user)
	if err != nil {
		t.Fatal(err)
	}
	write := func(id string, author store.ContentAuthor, ops ...store.SheetOperation) *store.SheetContent {
		t.Helper()
		out, err := s.WriteSheet(ctx, "table", store.SheetWrite{ClientRequestID: id, Operations: ops, Author: author})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	human := write("human", user, sheetCell("r", "a", "Human"))
	both := write("ai", ai, sheetCell("r", "b", float64(2)))
	if both.Body.Rows[0].Cells["a"] != "Human" || both.Body.Rows[0].Cells["b"] != float64(2) {
		t.Fatal("unrelated edit lost", both)
	}
	// Retry an acknowledged older request must not reapply it over newer content.
	retry := write("human", user, sheetCell("r", "a", "Human"))
	if retry.ContentHash != human.ContentHash {
		t.Fatal("retry returned a different result")
	}
	current, _ := s.GetSheet(ctx, "table")
	if current.ContentHash != both.ContentHash {
		t.Fatal("retry rewound content")
	}
	invalid := []store.SheetOperation{sheetCell("r", "a", "Partial"), sheetCell("missing", "b", float64(3))}
	if _, err := s.WriteSheet(ctx, "table", store.SheetWrite{ClientRequestID: "bad", Operations: invalid, Author: user}); err == nil {
		t.Fatal("deleted target accepted")
	}
	invalid[1] = sheetCell("r", "b", "not a number")
	if _, err := s.WriteSheet(ctx, "table", store.SheetWrite{ClientRequestID: "badtype", Operations: invalid, Author: user}); err == nil {
		t.Fatal("wrong type accepted")
	}
	guarded := sheetCell("r", "a", "Overwrite")
	guarded.Expected = json.RawMessage(`"Original"`)
	var conflict *store.SheetCellConflict
	if _, err := s.WriteSheet(ctx, "table", store.SheetWrite{ClientRequestID: "stale", Operations: []store.SheetOperation{guarded}, Author: ai}); !errors.As(err, &conflict) {
		t.Fatal("same-cell conflict missing", err)
	}
	current, _ = s.GetSheet(ctx, "table")
	if !reflect.DeepEqual(current, both) {
		t.Fatal("failed batch changed content")
	}
	rows, _ := s.ListStudioItemRevisions(ctx, "table")
	if len(rows) != 3 || rows[0].Author.SessionID != "writer" || rows[0].Author.TurnID != "turn" {
		t.Fatal("history/author incorrect", rows)
	}
	last := write("last", user, sheetCell("r", "a", "Last writer"))
	if last.Body.Rows[0].Cells["a"] != "Last writer" {
		t.Fatal(last)
	}
	deleted := write("delete", user, store.SheetOperation{Kind: "delete_row", RowID: "r"})
	if _, err := s.WriteSheet(ctx, "table", store.SheetWrite{ClientRequestID: "reuse", Operations: []store.SheetOperation{{Kind: "add_row", Row: &store.SheetRow{ID: "r", Cells: map[string]any{}}}}, Author: user}); err == nil {
		t.Fatal("deleted ID reused")
	}
	restored, err := s.WriteSheet(ctx, "table", store.SheetWrite{ClientRequestID: "restore", RestoreRevision: initial.RevisionID, ExpectedHash: &deleted.ContentHash, Author: user})
	if err != nil || restored.RevisionID == initial.RevisionID || !reflect.DeepEqual(restored.Body, initial.Body) {
		t.Fatal("restore failed", restored, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.GetSheet(ctx, "table")
	if err != nil || !reflect.DeepEqual(after, restored) {
		t.Fatal("restart lost table", err)
	}
	if _, err := s.GetNote(ctx, "table"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("table exposed as document")
	}
}
func TestSheetTransientIDsCannotBeReused(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	user := store.ContentAuthor{Kind: "user"}
	_, err := s.CreateSheet(ctx, &store.StudioItem{ID: "transient", Kind: "sheet", Name: "Transient"}, sheetFixture(), user)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"row", "column"} {
		add := store.SheetOperation{Kind: "add_" + kind, Row: &store.SheetRow{ID: "gone", Cells: map[string]any{}}, Column: &store.SheetColumn{ID: "gone", Name: "Gone", Type: "text"}}
		remove := store.SheetOperation{Kind: "delete_" + kind, RowID: "gone", ColumnID: "gone"}
		_, err = s.WriteSheet(ctx, "transient", store.SheetWrite{ClientRequestID: kind + "-transient", Operations: []store.SheetOperation{add, remove}, Author: user})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.WriteSheet(ctx, "transient", store.SheetWrite{ClientRequestID: kind + "-reuse", Operations: []store.SheetOperation{add}, Author: user})
		if err == nil {
			t.Errorf("%s ID was reused after deletion in an atomic batch", kind)
		}
	}
}

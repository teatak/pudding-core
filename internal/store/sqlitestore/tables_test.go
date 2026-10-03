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

func tableFixture() store.TableBody {
	return store.TableBody{Columns: []store.TableColumn{{ID: "a", Name: "Text", Type: "text"}, {ID: "b", Name: "Number", Type: "number"}}, Rows: []store.TableRow{{ID: "r", Cells: map[string]any{"a": "Original", "b": float64(1)}}}}
}
func tableCell(row, col string, value any) store.TableOperation {
	raw, _ := json.Marshal(value)
	return store.TableOperation{Kind: "set_cell", RowID: row, ColumnID: col, Value: raw}
}
func TestTableConcurrentEditsAtomicityAndHistory(t *testing.T) {
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
	initial, err := s.CreateTable(ctx, &store.StudioItem{ID: "table", Kind: "table", Name: "Data"}, tableFixture(), user)
	if err != nil {
		t.Fatal(err)
	}
	write := func(id string, author store.ContentAuthor, ops ...store.TableOperation) *store.TableContent {
		t.Helper()
		out, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: id, Operations: ops, Author: author})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	human := write("human", user, tableCell("r", "a", "Human"))
	both := write("ai", ai, tableCell("r", "b", float64(2)))
	if both.Body.Rows[0].Cells["a"] != "Human" || both.Body.Rows[0].Cells["b"] != float64(2) {
		t.Fatal("unrelated edit lost", both)
	}
	// Retry an acknowledged older request must not reapply it over newer content.
	retry := write("human", user, tableCell("r", "a", "Human"))
	if retry.ContentHash != human.ContentHash {
		t.Fatal("retry returned a different result")
	}
	current, _ := s.GetTable(ctx, "table")
	if current.ContentHash != both.ContentHash {
		t.Fatal("retry rewound content")
	}
	invalid := []store.TableOperation{tableCell("r", "a", "Partial"), tableCell("missing", "b", float64(3))}
	if _, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "bad", Operations: invalid, Author: user}); err == nil {
		t.Fatal("deleted target accepted")
	}
	invalid[1] = tableCell("r", "b", "not a number")
	if _, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "badtype", Operations: invalid, Author: user}); err == nil {
		t.Fatal("wrong type accepted")
	}
	guarded := tableCell("r", "a", "Overwrite")
	guarded.Expected = json.RawMessage(`"Original"`)
	var conflict *store.TableCellConflict
	if _, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "stale", Operations: []store.TableOperation{guarded}, Author: ai}); !errors.As(err, &conflict) {
		t.Fatal("same-cell conflict missing", err)
	}
	current, _ = s.GetTable(ctx, "table")
	if !reflect.DeepEqual(current, both) {
		t.Fatal("failed batch changed content")
	}
	rows, _ := s.ListStudioItemRevisions(ctx, "table")
	if len(rows) != 3 || rows[0].Author.SessionID != "writer" || rows[0].Author.TurnID != "turn" {
		t.Fatal("history/author incorrect", rows)
	}
	last := write("last", user, tableCell("r", "a", "Last writer"))
	if last.Body.Rows[0].Cells["a"] != "Last writer" {
		t.Fatal(last)
	}
	deleted := write("delete", user, store.TableOperation{Kind: "delete_row", RowID: "r"})
	if _, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "reuse", Operations: []store.TableOperation{{Kind: "add_row", Row: &store.TableRow{ID: "r", Cells: map[string]any{}}}}, Author: user}); err == nil {
		t.Fatal("deleted ID reused")
	}
	restored, err := s.WriteTable(ctx, "table", store.TableWrite{ClientRequestID: "restore", RestoreRevision: initial.RevisionID, ExpectedHash: &deleted.ContentHash, Author: user})
	if err != nil || restored.RevisionID == initial.RevisionID || !reflect.DeepEqual(restored.Body, initial.Body) {
		t.Fatal("restore failed", restored, err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	after, err := s.GetTable(ctx, "table")
	if err != nil || !reflect.DeepEqual(after, restored) {
		t.Fatal("restart lost table", err)
	}
	if _, err := s.GetDocument(ctx, "table"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("table exposed as document")
	}
}
func TestTableTransientIDsCannotBeReused(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	user := store.ContentAuthor{Kind: "user"}
	_, err := s.CreateTable(ctx, &store.StudioItem{ID: "transient", Kind: "table", Name: "Transient"}, tableFixture(), user)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"row", "column"} {
		add := store.TableOperation{Kind: "add_" + kind, Row: &store.TableRow{ID: "gone", Cells: map[string]any{}}, Column: &store.TableColumn{ID: "gone", Name: "Gone", Type: "text"}}
		remove := store.TableOperation{Kind: "delete_" + kind, RowID: "gone", ColumnID: "gone"}
		_, err = s.WriteTable(ctx, "transient", store.TableWrite{ClientRequestID: kind + "-transient", Operations: []store.TableOperation{add, remove}, Author: user})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.WriteTable(ctx, "transient", store.TableWrite{ClientRequestID: kind + "-reuse", Operations: []store.TableOperation{add}, Author: user})
		if err == nil {
			t.Errorf("%s ID was reused after deletion in an atomic batch", kind)
		}
	}
}

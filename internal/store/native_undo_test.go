package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestUndoDocumentPreservesDisjointEditsAndRejectsOverlap(t *testing.T) {
	before := "Header\n\nFirst paragraph.\n\nLast paragraph.\n"
	after := "Header\n\nAI revised first.\n\nLast paragraph.\n"
	current := "Human header\n\nAI revised first.\n\nHuman revised last.\n"
	got, err := UndoDocument(before, after, current)
	if err != nil || got != "Human header\n\nFirst paragraph.\n\nHuman revised last.\n" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err = UndoDocument(before, after, "Header\n\nHuman replaced first.\n\nLast paragraph.\n"); !errors.Is(err, ErrUndoConflict) {
		t.Fatal(err)
	}
	for _, pair := range [][2]string{{"中文😀 原文\n\n尾段", "中文😀 改写\n\n尾段"}, {"one\n\nend", "one\n\nAI added\n\nend"}, {"one\n\nAI deletes\n\nend", "one\n\nend"}} {
		got, err = UndoDocument(pair[0], pair[1], pair[1])
		if err != nil || got != pair[0] {
			t.Fatalf("%q %v", got, err)
		}
	}
}
func TestUndoTableRowsPreserveLaterCellsAndGuardStructure(t *testing.T) {
	before := TableBody{Columns: []TableColumn{{ID: "a", Name: "A", Type: "text"}, {ID: "b", Name: "B", Type: "text"}}, Rows: []TableRow{{ID: "r1", Cells: map[string]any{"a": "old", "b": "old"}}, {ID: "r2", Cells: map[string]any{"a": "old"}}}}
	after := TableBody{Columns: before.Columns, Rows: []TableRow{{ID: "r1", Cells: map[string]any{"a": "AI", "b": "old"}}, {ID: "r2", Cells: map[string]any{"a": "AI"}}}}
	current := TableBody{Columns: before.Columns, Rows: []TableRow{{ID: "r1", Cells: map[string]any{"a": "AI", "b": "Human"}}, {ID: "r2", Cells: map[string]any{"a": "AI"}}}}
	got, err := UndoTable(before, after, current, "r1")
	if err != nil || got.Rows[0].Cells["a"] != "old" || got.Rows[0].Cells["b"] != "Human" || got.Rows[1].Cells["a"] != "AI" {
		t.Fatal(got, err)
	}
	got, err = UndoTable(before, after, current, "")
	if err != nil || got.Rows[1].Cells["a"] != "old" || got.Rows[0].Cells["b"] != "Human" {
		t.Fatal(got, err)
	}
	current.Rows[0].Cells["a"] = "Newer"
	if _, err = UndoTable(before, after, current, "r1"); !errors.Is(err, ErrUndoConflict) {
		t.Fatal(err)
	}
	deleted := TableBody{Columns: before.Columns, Rows: before.Rows[:1]}
	got, err = UndoTable(before, deleted, deleted, "")
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
	moved := TableBody{Columns: before.Columns, Rows: []TableRow{before.Rows[1], before.Rows[0]}}
	got, err = UndoTable(before, moved, moved, "")
	if err != nil || !reflect.DeepEqual(got, before) {
		t.Fatal(got, err)
	}
}

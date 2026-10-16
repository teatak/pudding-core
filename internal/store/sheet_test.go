package store

import (
	"encoding/csv"
	"encoding/json"
	"github.com/teatak/pudding-core/contracts"
	"reflect"
	"strings"
	"testing"
)

func TestSheetTypedCellsAndLimits(t *testing.T) {
	for _, tc := range []struct {
		kind      string
		good, bad any
	}{
		{"text", "text", 1.0}, {"number", 1.5, "1.5"}, {"date", "2024-02-29", "2025-02-29"}, {"select", "Yes", "Unknown"}, {"checkbox", false, "false"}, {"link", "https://example.com/path", "javascript:alert(1)"},
	} {
		c := SheetColumn{ID: "c", Name: "Column", Type: tc.kind, Options: []string{"Yes"}}
		if err := ValidateSheetCell(c, tc.good); err != nil {
			t.Fatal(tc.kind, err)
		}
		if err := ValidateSheetCell(c, tc.bad); err == nil {
			t.Fatal("invalid value accepted", tc.kind)
		}
		if err := ValidateSheetCell(c, nil); err != nil {
			t.Fatal("blank rejected", tc.kind)
		}
	}
	if err := ValidateSheetCell(SheetColumn{Type: "text"}, strings.Repeat("x", contracts.Studio().MaxSheetCellBytes+1)); err == nil {
		t.Fatal("large cell accepted")
	}
	body := SheetBody{Columns: []SheetColumn{{ID: "c", Name: "Column", Type: "text"}}, Rows: []SheetRow{{ID: "r", Cells: map[string]any{"c": "1"}}}}
	conversion := []SheetOperation{{Kind: "update_column", ColumnID: "c", Column: &SheetColumn{ID: "c", Name: "Numeric", Type: "number"}}, {Kind: "set_cell", RowID: "r", ColumnID: "c", Value: json.RawMessage(`1`)}}
	out, err := ApplySheetOperations(body, conversion)
	if err != nil || out.Rows[0].Cells["c"] != float64(1) {
		t.Fatal("atomic conversion failed", out, err)
	}
	if body.Rows[0].Cells["c"] != "1" {
		t.Fatal("input mutated")
	}
	body.Rows = make([]SheetRow, contracts.Studio().MaxSheetRows+1)
	if err := ValidateSheet(body); err == nil {
		t.Fatal("row limit ignored")
	}
}
func TestSheetCSVPreservesQuotedUnicodeAndNewlines(t *testing.T) {
	cells := []string{"中文,comma", "a\"quote", "two\nlines", "=SUM(A1)", ""}
	cols := []SheetColumn{}
	row := SheetRow{ID: "r", Cells: map[string]any{}}
	for i, v := range cells {
		id := string(rune('a' + i))
		cols = append(cols, SheetColumn{ID: id, Name: id, Type: "text"})
		row.Cells[id] = v
	}
	raw, err := SheetCSV(SheetBody{Columns: cols, Rows: []SheetRow{row}})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil || !reflect.DeepEqual(rows[1], cells) {
		t.Fatal("CSV values changed", rows, err)
	}
}

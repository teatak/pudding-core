package store

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

const StudioItemKindTable = "table"

type TableColumn struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Options []string `json:"options,omitempty"`
}
type TableRow struct {
	ID    string         `json:"id"`
	Cells map[string]any `json:"cells"`
}
type TableBody struct {
	Columns []TableColumn `json:"columns"`
	Rows    []TableRow    `json:"rows"`
}
type TableContent struct {
	ItemID      string    `json:"itemID"`
	Body        TableBody `json:"body"`
	ContentHash string    `json:"contentHash"`
	RevisionID  string    `json:"revisionID"`
}

// IDs identify entities; beforeID controls order only and omitted means append.
// expected is optional: absent is last-writer-wins, null compares an empty cell.
type TableOperation struct {
	Kind     string          `json:"kind"`
	RowID    string          `json:"rowID,omitempty"`
	ColumnID string          `json:"columnID,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Expected json.RawMessage `json:"expected,omitempty"`
	Row      *TableRow       `json:"row,omitempty"`
	Column   *TableColumn    `json:"column,omitempty"`
	BeforeID string          `json:"beforeID,omitempty"`
}
type TableWrite struct {
	ClientRequestID string           `json:"clientRequestID"`
	Operations      []TableOperation `json:"operations,omitempty"`
	RestoreRevision string           `json:"restoreRevision,omitempty"`
	ExpectedHash    *string          `json:"expectedHash,omitempty"`
	Author          ContentAuthor    `json:"-"`
}
type TableStore interface {
	CreateTable(context.Context, *StudioItem, TableBody, ContentAuthor) (*TableContent, error)
	GetTable(context.Context, string) (*TableContent, error)
	WriteTable(context.Context, string, TableWrite) (*TableContent, error)
}
type InvalidTable struct{ Message string }

func (e *InvalidTable) Error() string { return e.Message }

type TableCellConflict struct{ RowID, ColumnID string }

func (e *TableCellConflict) Error() string {
	return fmt.Sprintf("cell changed: row %s, column %s", e.RowID, e.ColumnID)
}
func tableError(format string, args ...any) error { return &InvalidTable{fmt.Sprintf(format, args...)} }

var tableID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

func ValidateTable(body TableBody) error {
	p := contracts.Studio()
	if body.Columns == nil || body.Rows == nil {
		return tableError("columns and rows must be arrays")
	}
	if len(body.Columns) > p.MaxTableColumns || len(body.Rows) > p.MaxTableRows {
		return tableError("table exceeds limits: at most %d columns and %d rows", p.MaxTableColumns, p.MaxTableRows)
	}
	columns := map[string]TableColumn{}
	for _, c := range body.Columns {
		if !tableID.MatchString(c.ID) {
			return tableError("invalid column ID: %s", c.ID)
		}
		if _, exists := columns[c.ID]; exists {
			return tableError("duplicate column ID: %s", c.ID)
		}
		if strings.TrimSpace(c.Name) == "" || len(c.Name) > 200 || !utf8.ValidString(c.Name) {
			return tableError("column name must be UTF-8, nonempty and at most 200 bytes")
		}
		if !slices.Contains([]string{"text", "number", "date", "select", "checkbox", "link"}, c.Type) {
			return tableError("invalid column type: %s", c.Type)
		}
		if c.Type != "select" && len(c.Options) > 0 {
			return tableError("options are only allowed on select columns")
		}
		if len(c.Options) > 100 {
			return tableError("select column exceeds 100 options")
		}
		seen := map[string]bool{}
		for _, option := range c.Options {
			if option == "" || len(option) > 200 || !utf8.ValidString(option) || seen[option] {
				return tableError("select options must be unique nonempty UTF-8 strings (max 200 bytes)")
			}
			seen[option] = true
		}
		columns[c.ID] = c
	}
	rows := map[string]bool{}
	for _, r := range body.Rows {
		if !tableID.MatchString(r.ID) || rows[r.ID] {
			return tableError("invalid or duplicate row ID: %s", r.ID)
		}
		rows[r.ID] = true
		if r.Cells == nil {
			return tableError("row %s cells must be an object", r.ID)
		}
		for id, value := range r.Cells {
			c, ok := columns[id]
			if !ok {
				return tableError("row %s references missing column %s", r.ID, id)
			}
			if err := ValidateTableCell(c, value); err != nil {
				return tableError("row %s, column %s: %s", r.ID, c.Name, err)
			}
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return tableError("invalid table JSON: %s", err)
	}
	if len(raw) > p.MaxTableBytes {
		return tableError("table exceeds %d bytes", p.MaxTableBytes)
	}
	return nil
}
func ValidateTableCell(c TableColumn, value any) error {
	if value == nil {
		return nil
	}
	text, isText := value.(string)
	if isText && (!utf8.ValidString(text) || len(text) > contracts.Studio().MaxTableCellBytes) {
		return tableError("cell text exceeds size limit or is not UTF-8")
	}
	valid := false
	switch c.Type {
	case "text":
		valid = isText
	case "number":
		n, ok := value.(float64)
		valid = ok && !math.IsNaN(n) && !math.IsInf(n, 0)
	case "date":
		if isText {
			d, err := time.Parse("2006-01-02", text)
			valid = err == nil && d.Format("2006-01-02") == text
		}
	case "select":
		valid = isText && slices.Contains(c.Options, text)
	case "checkbox":
		_, valid = value.(bool)
	case "link":
		if isText {
			u, err := url.Parse(text)
			valid = err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != ""
		}
	}
	if !valid {
		return tableError("invalid %s value", c.Type)
	}
	return nil
}

// Apply to a clone and validate once at the end. Column conversions and their
// cell replacements can therefore be one atomic operation, with no partial edits.
func ApplyTableOperations(body TableBody, ops []TableOperation) (TableBody, error) {
	if len(ops) < 1 || len(ops) > contracts.Studio().MaxTableOperations {
		return TableBody{}, tableError("operation count must be 1–%d", contracts.Studio().MaxTableOperations)
	}
	raw, _ := json.Marshal(body)
	var out TableBody
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, err
	}
	// Deleted entities cannot be reintroduced as new entities even within a batch.
	seenRows, seenColumns := map[string]bool{}, map[string]bool{}
	for _, r := range out.Rows {
		seenRows[r.ID] = true
	}
	for _, c := range out.Columns {
		seenColumns[c.ID] = true
	}
	for _, op := range ops {
		row := slices.IndexFunc(out.Rows, func(r TableRow) bool { return r.ID == op.RowID })
		col := slices.IndexFunc(out.Columns, func(c TableColumn) bool { return c.ID == op.ColumnID })
		switch op.Kind {
		case "set_cell":
			if row < 0 || col < 0 {
				return out, tableError("cell target was deleted: row %s, column %s", op.RowID, op.ColumnID)
			}
			if len(op.Value) == 0 {
				return out, tableError("set_cell requires value (null clears the cell)")
			}
			if len(op.Expected) > 0 {
				var expected any
				if err := json.Unmarshal(op.Expected, &expected); err != nil {
					return out, tableError("invalid expected value")
				}
				if !reflect.DeepEqual(out.Rows[row].Cells[op.ColumnID], expected) {
					return out, &TableCellConflict{op.RowID, op.ColumnID}
				}
			}
			var value any
			if err := json.Unmarshal(op.Value, &value); err != nil {
				return out, tableError("invalid cell value")
			}
			if value == nil {
				delete(out.Rows[row].Cells, op.ColumnID)
			} else {
				out.Rows[row].Cells[op.ColumnID] = value
			}
		case "add_row":
			if op.Row == nil || op.Row.Cells == nil || seenRows[op.Row.ID] {
				return out, tableError("add_row requires a new row ID and cells object")
			}
			seenRows[op.Row.ID] = true
			at := len(out.Rows)
			if op.BeforeID != "" {
				at = slices.IndexFunc(out.Rows, func(r TableRow) bool { return r.ID == op.BeforeID })
				if at < 0 {
					return out, tableError("row order target was deleted: %s", op.BeforeID)
				}
			}
			cells := map[string]any{}
			for k, v := range op.Row.Cells {
				cells[k] = v
			}
			out.Rows = slices.Insert(out.Rows, at, TableRow{ID: op.Row.ID, Cells: cells})
		case "delete_row", "move_row":
			if row < 0 {
				return out, tableError("row was deleted: %s", op.RowID)
			}
			if op.Kind == "move_row" && op.BeforeID == op.RowID {
				continue
			}
			r := out.Rows[row]
			out.Rows = slices.Delete(out.Rows, row, row+1)
			if op.Kind == "move_row" {
				at := len(out.Rows)
				if op.BeforeID != "" {
					at = slices.IndexFunc(out.Rows, func(r TableRow) bool { return r.ID == op.BeforeID })
					if at < 0 {
						return out, tableError("row order target was deleted: %s", op.BeforeID)
					}
				}
				out.Rows = slices.Insert(out.Rows, at, r)
			}
		case "add_column":
			if op.Column == nil || seenColumns[op.Column.ID] {
				return out, tableError("add_column requires a new column ID")
			}
			seenColumns[op.Column.ID] = true
			at := len(out.Columns)
			if op.BeforeID != "" {
				at = slices.IndexFunc(out.Columns, func(c TableColumn) bool { return c.ID == op.BeforeID })
				if at < 0 {
					return out, tableError("column order target was deleted: %s", op.BeforeID)
				}
			}
			out.Columns = slices.Insert(out.Columns, at, *op.Column)
		case "update_column":
			if col < 0 || op.Column == nil || op.Column.ID != op.ColumnID {
				return out, tableError("update_column requires an existing column and the same ID")
			}
			out.Columns[col] = *op.Column
		case "delete_column", "move_column":
			if col < 0 {
				return out, tableError("column was deleted: %s", op.ColumnID)
			}
			if op.Kind == "move_column" && op.BeforeID == op.ColumnID {
				continue
			}
			c := out.Columns[col]
			out.Columns = slices.Delete(out.Columns, col, col+1)
			if op.Kind == "delete_column" {
				for i := range out.Rows {
					delete(out.Rows[i].Cells, op.ColumnID)
				}
			} else {
				at := len(out.Columns)
				if op.BeforeID != "" {
					at = slices.IndexFunc(out.Columns, func(c TableColumn) bool { return c.ID == op.BeforeID })
					if at < 0 {
						return out, tableError("column order target was deleted: %s", op.BeforeID)
					}
				}
				out.Columns = slices.Insert(out.Columns, at, c)
			}
		default:
			return out, tableError("unknown table operation: %s", op.Kind)
		}
		if len(out.Rows) > contracts.Studio().MaxTableRows || len(out.Columns) > contracts.Studio().MaxTableColumns {
			return out, tableError("table row or column limit exceeded")
		}
	}
	return out, ValidateTable(out)
}
func TableCSV(body TableBody) (string, error) {
	var output strings.Builder
	w := csv.NewWriter(&output)
	header := make([]string, len(body.Columns))
	for i, c := range body.Columns {
		header[i] = c.Name
	}
	if err := w.Write(header); err != nil {
		return "", err
	}
	for _, r := range body.Rows {
		row := make([]string, len(body.Columns))
		for i, c := range body.Columns {
			if value := r.Cells[c.ID]; value != nil {
				if text, ok := value.(string); ok {
					row[i] = text
				} else {
					raw, _ := json.Marshal(value)
					row[i] = string(raw)
				}
			}
		}
		if err := w.Write(row); err != nil {
			return "", err
		}
	}
	w.Flush()
	return output.String(), w.Error()
}

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

const StudioItemKindSheet = "sheet"

type SheetColumn struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Options []string `json:"options,omitempty"`
}
type SheetRow struct {
	ID    string         `json:"id"`
	Cells map[string]any `json:"cells"`
}
type SheetBody struct {
	Columns []SheetColumn `json:"columns"`
	Rows    []SheetRow    `json:"rows"`
}
type SheetContent struct {
	ItemID      string    `json:"itemID"`
	Body        SheetBody `json:"body"`
	ContentHash string    `json:"contentHash"`
	RevisionID  string    `json:"revisionID"`
}

// IDs identify entities; beforeID controls order only and omitted means append.
// expected is optional: absent is last-writer-wins, null compares an empty cell.
type SheetOperation struct {
	Kind     string          `json:"kind"`
	RowID    string          `json:"rowID,omitempty"`
	ColumnID string          `json:"columnID,omitempty"`
	Value    json.RawMessage `json:"value,omitempty"`
	Expected json.RawMessage `json:"expected,omitempty"`
	Row      *SheetRow       `json:"row,omitempty"`
	Column   *SheetColumn    `json:"column,omitempty"`
	BeforeID string          `json:"beforeID,omitempty"`
}
type SheetWrite struct {
	ClientRequestID string           `json:"clientRequestID"`
	Operations      []SheetOperation `json:"operations,omitempty"`
	RestoreRevision string           `json:"restoreRevision,omitempty"`
	UndoRevision    string           `json:"undoRevision,omitempty"`
	RowID           string           `json:"rowID,omitempty"`
	ExpectedHash    *string          `json:"expectedHash,omitempty"`
	Author          ContentAuthor    `json:"-"`
}
type SheetStore interface {
	CreateSheet(context.Context, *StudioItem, SheetBody, ContentAuthor, ...string) (*SheetContent, error)
	GetSheet(context.Context, string) (*SheetContent, error)
	WriteSheet(context.Context, string, SheetWrite) (*SheetContent, error)
}
type InvalidSheet struct{ Message string }

func (e *InvalidSheet) Error() string { return e.Message }

type SheetCellConflict struct{ RowID, ColumnID string }

func (e *SheetCellConflict) Error() string {
	return fmt.Sprintf("cell changed: row %s, column %s", e.RowID, e.ColumnID)
}
func sheetError(format string, args ...any) error { return &InvalidSheet{fmt.Sprintf(format, args...)} }

var sheetID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,100}$`)

func ValidateSheet(body SheetBody) error {
	p := contracts.Studio()
	if body.Columns == nil || body.Rows == nil {
		return sheetError("columns and rows must be arrays")
	}
	if len(body.Columns) > p.MaxSheetColumns || len(body.Rows) > p.MaxSheetRows {
		return sheetError("table exceeds limits: at most %d columns and %d rows", p.MaxSheetColumns, p.MaxSheetRows)
	}
	columns := map[string]SheetColumn{}
	for _, c := range body.Columns {
		if !sheetID.MatchString(c.ID) {
			return sheetError("invalid column ID: %s", c.ID)
		}
		if _, exists := columns[c.ID]; exists {
			return sheetError("duplicate column ID: %s", c.ID)
		}
		if strings.TrimSpace(c.Name) == "" || len(c.Name) > 200 || !utf8.ValidString(c.Name) {
			return sheetError("column name must be UTF-8, nonempty and at most 200 bytes")
		}
		if !slices.Contains([]string{"text", "number", "date", "select", "checkbox", "link"}, c.Type) {
			return sheetError("invalid column type: %s", c.Type)
		}
		if c.Type != "select" && len(c.Options) > 0 {
			return sheetError("options are only allowed on select columns")
		}
		if len(c.Options) > 100 {
			return sheetError("select column exceeds 100 options")
		}
		seen := map[string]bool{}
		for _, option := range c.Options {
			if option == "" || len(option) > 200 || !utf8.ValidString(option) || seen[option] {
				return sheetError("select options must be unique nonempty UTF-8 strings (max 200 bytes)")
			}
			seen[option] = true
		}
		columns[c.ID] = c
	}
	rows := map[string]bool{}
	for _, r := range body.Rows {
		if !sheetID.MatchString(r.ID) || rows[r.ID] {
			return sheetError("invalid or duplicate row ID: %s", r.ID)
		}
		rows[r.ID] = true
		if r.Cells == nil {
			return sheetError("row %s cells must be an object", r.ID)
		}
		for id, value := range r.Cells {
			c, ok := columns[id]
			if !ok {
				return sheetError("row %s references missing column %s", r.ID, id)
			}
			if err := ValidateSheetCell(c, value); err != nil {
				return sheetError("row %s, column %s: %s", r.ID, c.Name, err)
			}
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return sheetError("invalid table JSON: %s", err)
	}
	if len(raw) > p.MaxSheetBytes {
		return sheetError("table exceeds %d bytes", p.MaxSheetBytes)
	}
	return nil
}
func ValidateSheetCell(c SheetColumn, value any) error {
	if value == nil {
		return nil
	}
	text, isText := value.(string)
	if isText && (!utf8.ValidString(text) || len(text) > contracts.Studio().MaxSheetCellBytes) {
		return sheetError("cell text exceeds size limit or is not UTF-8")
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
		return sheetError("invalid %s value", c.Type)
	}
	return nil
}

// Apply to a clone and validate once at the end. Column conversions and their
// cell replacements can therefore be one atomic operation, with no partial edits.
func ApplySheetOperations(body SheetBody, ops []SheetOperation) (SheetBody, error) {
	if len(ops) < 1 || len(ops) > contracts.Studio().MaxSheetOperations {
		return SheetBody{}, sheetError("operation count must be 1–%d", contracts.Studio().MaxSheetOperations)
	}
	raw, _ := json.Marshal(body)
	var out SheetBody
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
		row := slices.IndexFunc(out.Rows, func(r SheetRow) bool { return r.ID == op.RowID })
		col := slices.IndexFunc(out.Columns, func(c SheetColumn) bool { return c.ID == op.ColumnID })
		switch op.Kind {
		case "set_cell":
			if row < 0 || col < 0 {
				return out, sheetError("cell target was deleted: row %s, column %s", op.RowID, op.ColumnID)
			}
			if len(op.Value) == 0 {
				return out, sheetError("set_cell requires value (null clears the cell)")
			}
			if len(op.Expected) > 0 {
				var expected any
				if err := json.Unmarshal(op.Expected, &expected); err != nil {
					return out, sheetError("invalid expected value")
				}
				if !reflect.DeepEqual(out.Rows[row].Cells[op.ColumnID], expected) {
					return out, &SheetCellConflict{op.RowID, op.ColumnID}
				}
			}
			var value any
			if err := json.Unmarshal(op.Value, &value); err != nil {
				return out, sheetError("invalid cell value")
			}
			if value == nil {
				delete(out.Rows[row].Cells, op.ColumnID)
			} else {
				out.Rows[row].Cells[op.ColumnID] = value
			}
		case "add_row":
			if op.Row == nil || op.Row.Cells == nil || seenRows[op.Row.ID] {
				return out, sheetError("add_row requires a new row ID and cells object")
			}
			seenRows[op.Row.ID] = true
			at := len(out.Rows)
			if op.BeforeID != "" {
				at = slices.IndexFunc(out.Rows, func(r SheetRow) bool { return r.ID == op.BeforeID })
				if at < 0 {
					return out, sheetError("row order target was deleted: %s", op.BeforeID)
				}
			}
			cells := map[string]any{}
			for k, v := range op.Row.Cells {
				cells[k] = v
			}
			out.Rows = slices.Insert(out.Rows, at, SheetRow{ID: op.Row.ID, Cells: cells})
		case "delete_row", "move_row":
			if row < 0 {
				return out, sheetError("row was deleted: %s", op.RowID)
			}
			if op.Kind == "move_row" && op.BeforeID == op.RowID {
				continue
			}
			r := out.Rows[row]
			out.Rows = slices.Delete(out.Rows, row, row+1)
			if op.Kind == "move_row" {
				at := len(out.Rows)
				if op.BeforeID != "" {
					at = slices.IndexFunc(out.Rows, func(r SheetRow) bool { return r.ID == op.BeforeID })
					if at < 0 {
						return out, sheetError("row order target was deleted: %s", op.BeforeID)
					}
				}
				out.Rows = slices.Insert(out.Rows, at, r)
			}
		case "add_column":
			if op.Column == nil || seenColumns[op.Column.ID] {
				return out, sheetError("add_column requires a new column ID")
			}
			seenColumns[op.Column.ID] = true
			at := len(out.Columns)
			if op.BeforeID != "" {
				at = slices.IndexFunc(out.Columns, func(c SheetColumn) bool { return c.ID == op.BeforeID })
				if at < 0 {
					return out, sheetError("column order target was deleted: %s", op.BeforeID)
				}
			}
			out.Columns = slices.Insert(out.Columns, at, *op.Column)
		case "update_column":
			if col < 0 || op.Column == nil || op.Column.ID != op.ColumnID {
				return out, sheetError("update_column requires an existing column and the same ID")
			}
			out.Columns[col] = *op.Column
		case "delete_column", "move_column":
			if col < 0 {
				return out, sheetError("column was deleted: %s", op.ColumnID)
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
					at = slices.IndexFunc(out.Columns, func(c SheetColumn) bool { return c.ID == op.BeforeID })
					if at < 0 {
						return out, sheetError("column order target was deleted: %s", op.BeforeID)
					}
				}
				out.Columns = slices.Insert(out.Columns, at, c)
			}
		default:
			return out, sheetError("unknown table operation: %s", op.Kind)
		}
		if len(out.Rows) > contracts.Studio().MaxSheetRows || len(out.Columns) > contracts.Studio().MaxSheetColumns {
			return out, sheetError("table row or column limit exceeded")
		}
	}
	return out, ValidateSheet(out)
}
func SheetCSV(body SheetBody) (string, error) {
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

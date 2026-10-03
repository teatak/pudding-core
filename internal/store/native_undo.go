package store

import (
	"errors"
	"reflect"
	"slices"

	"github.com/sergi/go-diff/diffmatchpatch"
)

var ErrUndoConflict = errors.New("later edits overlap this change")

type textChange struct {
	start, end int
	text       []rune
}

func textChanges(before, after string) []textChange {
	diffs := diffmatchpatch.New().DiffMain(before, after, false)
	var changes []textChange
	cursor := 0
	for i := 0; i < len(diffs); {
		if diffs[i].Type == diffmatchpatch.DiffEqual {
			cursor += len([]rune(diffs[i].Text))
			i++
			continue
		}
		c := textChange{start: cursor, end: cursor}
		for i < len(diffs) && diffs[i].Type != diffmatchpatch.DiffEqual {
			text := []rune(diffs[i].Text)
			if diffs[i].Type == diffmatchpatch.DiffDelete {
				cursor += len(text)
				c.end = cursor
			} else {
				c.text = append(c.text, text...)
			}
			i++
		}
		changes = append(changes, c)
	}
	return changes
}
func overlaps(a, b textChange) bool {
	if a.start == a.end {
		return b.start <= a.start && a.start <= b.end
	}
	if b.start == b.end {
		return a.start <= b.start && b.start <= a.end
	}
	return a.start < b.end && b.start < a.end
}

// Map an exact inverse through later, disjoint edits. Never apply fuzzy patches:
// an overlap rejects the complete operation rather than overwriting newer text.
func UndoDocument(before, after, current string) (string, error) {
	inverse, later := textChanges(after, before), textChanges(after, current)
	if len(inverse) == 0 {
		return "", &InvalidDocument{"version has no content changes"}
	}
	for i := range inverse {
		shift := 0
		for _, change := range later {
			if overlaps(inverse[i], change) {
				return "", ErrUndoConflict
			}
			if change.end <= inverse[i].start {
				shift += len(change.text) - (change.end - change.start)
			}
		}
		inverse[i].start += shift
		inverse[i].end += shift
	}
	out := []rune(current)
	for i := len(inverse) - 1; i >= 0; i-- {
		c := inverse[i]
		out = append(out[:c.start], append(c.text, out[c.end:]...)...)
	}
	result := string(out)
	return result, ValidateDocument(result)
}

// The boolean distinguishes an absent map key from an explicit null. Unchanged
// fields come from current; changed fields must still contain the AI result.
func undoValue(before any, beforeOK bool, after any, afterOK bool, current any, currentOK bool) (any, bool, error) {
	if beforeOK == afterOK && reflect.DeepEqual(before, after) {
		return current, currentOK, nil
	}
	if currentOK == afterOK && reflect.DeepEqual(current, after) {
		return before, beforeOK, nil
	}
	b, bMap := before.(map[string]any)
	a, aMap := after.(map[string]any)
	c, cMap := current.(map[string]any)
	if beforeOK && afterOK && currentOK && bMap && aMap && cMap {
		out := map[string]any{}
		for k, v := range c {
			out[k] = v
		}
		keys := map[string]bool{}
		for k := range b {
			keys[k] = true
		}
		for k := range a {
			keys[k] = true
		}
		for k := range keys {
			bv, bok := b[k]
			av, aok := a[k]
			cv, cok := c[k]
			v, ok, err := undoValue(bv, bok, av, aok, cv, cok)
			if err != nil {
				return nil, false, err
			}
			if ok {
				out[k] = v
			} else {
				delete(out, k)
			}
		}
		return out, true, nil
	}
	return nil, false, ErrUndoConflict
}
func columnValues(columns []TableColumn) (map[string]any, []string) {
	out := map[string]any{}
	ids := []string{}
	for _, c := range columns {
		out[c.ID] = map[string]any{"id": c.ID, "name": c.Name, "type": c.Type, "options": c.Options}
		ids = append(ids, c.ID)
	}
	return out, ids
}
func rowValues(rows []TableRow) (map[string]any, []string) {
	out := map[string]any{}
	ids := []string{}
	for _, r := range rows {
		cells := map[string]any{}
		for k, v := range r.Cells {
			if v != nil {
				cells[k] = v
			}
		}
		out[r.ID] = cells
		ids = append(ids, r.ID)
	}
	return out, ids
}
func undoOrder(before, after, current []string) ([]string, error) {
	if slices.Equal(before, after) {
		return current, nil
	}
	if slices.Equal(after, current) {
		return before, nil
	}
	// A structural edit plus later structural edits is ambiguous. Cell edits do
	// not change this sequence and remain eligible for selective undo.
	return nil, ErrUndoConflict
}
func UndoTable(before, after, current TableBody, rowID string) (TableBody, error) {
	bRows, bOrder := rowValues(before.Rows)
	aRows, aOrder := rowValues(after.Rows)
	cRows, cOrder := rowValues(current.Rows)
	if rowID != "" {
		if !reflect.DeepEqual(before.Columns, after.Columns) {
			return TableBody{}, &InvalidTable{"row undo requires unchanged columns; undo the complete change instead"}
		}
		b, bok := bRows[rowID]
		a, aok := aRows[rowID]
		c, cok := cRows[rowID]
		if !bok || !aok || reflect.DeepEqual(b, a) {
			return TableBody{}, &InvalidTable{"row undo requires changed cells in an existing row"}
		}
		value, _, err := undoValue(b, true, a, true, c, cok)
		if err != nil {
			return TableBody{}, err
		}
		cRows[rowID] = value
	} else {
		rows, _, err := undoValue(bRows, true, aRows, true, cRows, true)
		if err != nil {
			return TableBody{}, err
		}
		cRows = rows.(map[string]any)
		cOrder, err = undoOrder(bOrder, aOrder, cOrder)
		if err != nil {
			return TableBody{}, err
		}
	}
	bCols, bColOrder := columnValues(before.Columns)
	aCols, aColOrder := columnValues(after.Columns)
	cCols, cColOrder := columnValues(current.Columns)
	if rowID == "" {
		cols, _, err := undoValue(bCols, true, aCols, true, cCols, true)
		if err != nil {
			return TableBody{}, err
		}
		cCols = cols.(map[string]any)
		cColOrder, err = undoOrder(bColOrder, aColOrder, cColOrder)
		if err != nil {
			return TableBody{}, err
		}
	}
	out := TableBody{Columns: []TableColumn{}, Rows: []TableRow{}}
	for _, id := range cColOrder {
		c := cCols[id].(map[string]any)
		out.Columns = append(out.Columns, TableColumn{ID: id, Name: c["name"].(string), Type: c["type"].(string), Options: c["options"].([]string)})
	}
	for _, id := range cOrder {
		out.Rows = append(out.Rows, TableRow{ID: id, Cells: cRows[id].(map[string]any)})
	}
	// Stable deterministic traversal above follows current/baseline orders; map
	// iteration never decides table ordering or the resulting serialized hash.
	if err := ValidateTable(out); err != nil {
		return TableBody{}, ErrUndoConflict
	}
	if reflect.DeepEqual(out, current) {
		return TableBody{}, &InvalidTable{"version has no content changes"}
	}
	return out, nil
}

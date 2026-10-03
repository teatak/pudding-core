package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
	"slices"
	"strings"
	"time"
)

func (e *Engine) executeTable(ctx context.Context, sessionID, turnID string, call tool.Call, args tool.StudioArgs) (any, error) {
	author := store.ContentAuthor{Kind: "session", SessionID: sessionID, TurnID: turnID}
	switch call.Name {
	case tool.TableCreate:
		name := strings.TrimSpace(args.Name)
		if name == "" || len(name) > 200 || args.Table == nil {
			return nil, errors.New("name (max 200 bytes) and table are required")
		}
		id := "table_" + store.DocumentHash(sessionID + "\x00" + turnID + "\x00" + call.CallID)[:32]
		table, err := e.store.GetTable(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			now := time.Now().UTC()
			table, err = e.store.CreateTable(ctx, &store.StudioItem{ID: id, Kind: store.StudioItemKindTable, Name: name, Icon: "table", SourceSessionID: sessionID, CreatedAt: now, UpdatedAt: now}, *args.Table, author)
		}
		if err != nil {
			return nil, err
		}
		mount, err := e.store.OpenStudioItem(ctx, sessionID, id, store.NewID("mount"))
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": id, "mountID": mount.ID, "contentHash": table.ContentHash, "revisionID": table.RevisionID}, nil
	case tool.TableUpdate:
		table, err := e.store.WriteTable(ctx, args.ItemID, store.TableWrite{ClientRequestID: turnID + "/" + call.CallID, Operations: args.Operations, Author: author})
		if err != nil {
			return nil, err
		}
		return map[string]any{"itemID": table.ItemID, "contentHash": table.ContentHash, "revisionID": table.RevisionID}, nil
	case tool.TableRead:
		table, err := e.store.GetTable(ctx, args.ItemID)
		if err != nil {
			return nil, err
		}
		return readTableRange(table, args)
	}
	return nil, errors.New("unknown table tool")
}
func readTableRange(table *store.TableContent, args tool.StudioArgs) (any, error) {
	p := contracts.Studio()
	if args.Limit == 0 {
		args.Limit = 100
	}
	if args.Offset < 0 || args.Limit < 1 || args.Limit > p.MaxTableReadRows || len(args.RowIDs) > p.MaxTableReadRows || len(args.ColumnIDs) > p.MaxTableColumns {
		return nil, errors.New("invalid table read range")
	}
	if args.Format != "" && args.Format != "json" && args.Format != "csv" {
		return nil, errors.New("format must be json or csv")
	}
	columns := []store.TableColumn{}
	for _, c := range table.Body.Columns {
		if len(args.ColumnIDs) == 0 || slices.Contains(args.ColumnIDs, c.ID) {
			columns = append(columns, c)
		}
	}
	for _, id := range args.ColumnIDs {
		if !slices.ContainsFunc(columns, func(c store.TableColumn) bool { return c.ID == id }) {
			return nil, fmt.Errorf("column not found: %s", id)
		}
	}
	rows := table.Body.Rows
	if len(args.RowIDs) > 0 {
		rows = []store.TableRow{}
		for _, r := range table.Body.Rows {
			if slices.Contains(args.RowIDs, r.ID) {
				rows = append(rows, r)
			}
		}
		for _, id := range args.RowIDs {
			if !slices.ContainsFunc(rows, func(r store.TableRow) bool { return r.ID == id }) {
				return nil, fmt.Errorf("row not found: %s", id)
			}
		}
	}
	start := min(args.Offset, len(rows))
	end := min(start+args.Limit, len(rows))
	selected := []store.TableRow{}
	ids := []string{}
	// Reserve metadata space and leave a conservative factor for CSV escaping.
	size := 0
	columnJSON, _ := json.Marshal(columns)
	size += len(columnJSON) + 4096
	for i := start; i < end; i++ {
		r := store.TableRow{ID: rows[i].ID, Cells: map[string]any{}}
		for _, c := range columns {
			if value, ok := rows[i].Cells[c.ID]; ok {
				r.Cells[c.ID] = value
			}
		}
		raw, _ := json.Marshal(r)
		if size+len(raw)*2 > p.MaxTableReadBytes {
			if len(selected) == 0 {
				return nil, errors.New("row exceeds read size limit; select fewer columns")
			}
			end = i
			break
		}
		size += len(raw) * 2
		selected = append(selected, r)
		ids = append(ids, r.ID)
	}
	out := map[string]any{"itemID": table.ItemID, "contentHash": table.ContentHash, "revisionID": table.RevisionID, "columns": columns, "rowIDs": ids, "offset": start, "totalRows": len(rows)}
	if args.Format == "csv" {
		csv, err := store.TableCSV(store.TableBody{Columns: columns, Rows: selected})
		if err != nil {
			return nil, err
		}
		out["csv"] = csv
	} else {
		out["rows"] = selected
	}
	if end < len(rows) {
		out["nextOffset"] = end
	}
	return out, nil
}

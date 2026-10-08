package sqlitestore

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
)

func widgetDataTx(ctx context.Context, tx *sql.Tx, id string) (*store.WidgetData, error) {
	w, err := scanStudioItem(tx.QueryRowContext(ctx, `SELECT `+studioItemColumns+` FROM studio_items WHERE id=? AND kind='widget' AND deleted=0 AND archived_at=0`, id))
	if err != nil {
		return nil, err
	}
	data := &store.WidgetData{Data: json.RawMessage(`{}`)}
	var raw string
	err = tx.QueryRowContext(ctx, `SELECT version,data FROM widget_data WHERE item_id=?`, w.ID).Scan(&data.Version, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return data, nil
	}
	if err != nil {
		return nil, err
	}
	data.Data = json.RawMessage(raw)
	return data, nil
}

func (s *Store) GetWidgetData(ctx context.Context, id string) (*store.WidgetData, error) {
	var data *store.WidgetData
	err := s.tx(ctx, func(tx *sql.Tx) (err error) { data, err = widgetDataTx(ctx, tx, id); return err })
	return data, err
}

// WriteWidgetData replaces one item-scoped JSON object using compare-and-swap.
// No automatic write retry: a stale or uncertain caller must read the current data.
func (s *Store) WriteWidgetData(ctx context.Context, id, revisionHash string, expected int64, input json.RawMessage) (*store.WidgetData, error) {
	if expected < 0 || len(input) > contracts.Widget().MaxStorageBytes {
		return nil, store.ErrInvalidWidgetData
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(input, &fields) != nil || fields == nil {
		return nil, store.ErrInvalidWidgetData
	}
	var compact bytes.Buffer
	if json.Compact(&compact, input) != nil {
		return nil, store.ErrInvalidWidgetData
	}
	var data *store.WidgetData
	err := s.tx(ctx, func(tx *sql.Tx) error {
		current, err := widgetDataTx(ctx, tx, id)
		if err != nil {
			return err
		}
		var active string
		if err := tx.QueryRowContext(ctx, `SELECT active_revision FROM studio_items WHERE id=?`, id).Scan(&active); err != nil {
			return err
		}
		if active == "" || revisionHash != active {
			return store.ErrStudioItemConflict
		}
		if current.Version != expected {
			return store.ErrWidgetDataConflict
		}
		data = &store.WidgetData{Version: current.Version + 1, Data: append(json.RawMessage(nil), compact.Bytes()...)}
		_, err = tx.ExecContext(ctx, `INSERT INTO widget_data(item_id,version,data) VALUES(?,?,?) ON CONFLICT(item_id) DO UPDATE SET version=excluded.version,data=excluded.data`, id, data.Version, string(data.Data))
		return err
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

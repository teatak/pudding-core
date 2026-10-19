package sqlitestore

import (
	"context"
	"database/sql"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

// Existing names and immutable content/history are untouched. Only an exact
// content-derived name or a known empty placeholder can safely follow content.
func migratePageNames(tx *sql.Tx) error {
	for _, column := range []string{"name_mode", "name_fallback"} {
		if err := addTableColumnIfMissing(tx, "studio_items", column, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return err
		}
	}
	rows, err := tx.Query(`SELECT i.id,i.name,c.body,c.content_format FROM studio_items i JOIN studio_item_content c ON c.item_id=i.id WHERE i.kind IN ('page','note','doc') AND i.name_mode=''`)
	if err != nil {
		return err
	}
	type entry struct{ id, name, raw, format string }
	var entries []entry
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.name, &e.raw, &e.format); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		body, _, err := store.DecodeStoredContent(e.id, e.raw, e.format)
		if err != nil {
			return err
		}
		mode, fallback := store.PageNameManual, store.PageNameFallback
		if name := store.PageAutomaticName(body); name != "" && name == e.name {
			mode = store.PageNameAuto
		}
		if strings.TrimSpace(body) == "" {
			switch e.name {
			case "Untitled page", "Untitled Page", "未命名 Page", "未命名笔记", "未命名筆記", "Untitled note":
				mode, fallback = store.PageNameAuto, e.name
			}
		}
		if _, err = tx.Exec(`UPDATE studio_items SET name_mode=?,name_fallback=? WHERE id=?`, mode, fallback, e.id); err != nil {
			return err
		}
	}
	return nil
}

func updatePageNameTx(ctx context.Context, tx *sql.Tx, id, body string) error {
	var mode, fallback string
	if err := tx.QueryRowContext(ctx, `SELECT name_mode,name_fallback FROM studio_items WHERE id=?`, id).Scan(&mode, &fallback); err != nil {
		return err
	}
	if mode != store.PageNameAuto {
		return nil
	}
	name := store.PageAutomaticName(body)
	if name == "" {
		name = fallback
	}
	if name == "" {
		name = store.PageNameFallback
	}
	_, err := tx.ExecContext(ctx, `UPDATE studio_items SET name=? WHERE id=?`, name, id)
	return err
}

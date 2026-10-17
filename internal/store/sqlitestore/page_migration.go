package sqlitestore

import (
	"database/sql"
	"fmt"
	"strings"
)

// migratePages is v43. It upgrades studio_items constraint to ('page','sheet','widget')
// and maps any remaining 'note' or 'doc' kinds to 'page'.
func migratePages(tx *sql.Tx) error {
	var ddl string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='studio_items'`).Scan(&ddl); err != nil {
		return err
	}
	if strings.Contains(ddl, "'page','sheet','widget'") {
		return nil
	}
	rows, err := tx.Query(`SELECT sql FROM sqlite_master WHERE tbl_name='studio_items' AND type IN ('index','trigger') AND sql IS NOT NULL ORDER BY type,name`)
	if err != nil {
		return err
	}
	var objects []string
	for rows.Next() {
		var query string
		if err := rows.Scan(&query); err != nil {
			rows.Close()
			return err
		}
		objects = append(objects, query)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	ddl = strings.Replace(ddl, "studio_items", "studio_items_page", 1)
	replaced := false
	for _, old := range []string{"'note','sheet','widget'", "'note','table','widget'", "'doc','table','widget'"} {
		if strings.Contains(ddl, old) {
			ddl = strings.Replace(ddl, old, "'page','sheet','widget'", 1)
			replaced = true
			break
		}
	}
	if !replaced {
		return fmt.Errorf("unexpected Studio kind constraint before page migration: %s", ddl)
	}
	if _, err := tx.Exec(ddl); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO studio_items_page (rowid,id,kind,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at,archived_at,origin)
 SELECT rowid,id,CASE kind WHEN 'doc' THEN 'page' WHEN 'note' THEN 'page' ELSE kind END,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at,archived_at,origin FROM studio_items;
DROP TABLE studio_items;
ALTER TABLE studio_items_page RENAME TO studio_items;`); err != nil {
		return err
	}
	for _, query := range objects {
		if _, err := tx.Exec(query); err != nil {
			return err
		}
	}
	check, err := tx.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return err
	}
	defer check.Close()
	if check.Next() {
		return fmt.Errorf("foreign key violation after page migration")
	}
	return check.Err()
}

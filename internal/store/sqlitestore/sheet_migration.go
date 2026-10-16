package sqlitestore

import (
	"database/sql"
	"fmt"
	"strings"
)

// migrateSheets is v42. It changes identities, never content bytes, hashes,
// revision chains, request fingerprints or existing item/row/column IDs.
// runSchemaMigration disables foreign keys before beginning this transaction.
func migrateSheets(tx *sql.Tx) error {
	var ddl string
	readDDL := func() error {
		return tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='studio_items'`).Scan(&ddl)
	}
	if err := readDDL(); err != nil {
		return err
	}
	// Some development v41 databases still use the pre-Notes/Documents spelling.
	// Preserve their existing repair path before applying the v42 kind change.
	if strings.Contains(ddl, "'doc'") || strings.Contains(ddl, "'rich-document'") {
		if err := migrateDocuments(tx); err != nil {
			return err
		}
		if err := readDDL(); err != nil {
			return err
		}
	}
	if !strings.Contains(ddl, "'note','table','widget','document'") {
		return fmt.Errorf("unexpected Studio kind constraint before sheet migration")
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
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	ddl = strings.Replace(ddl, "studio_items", "studio_items_sheet", 1)
	ddl = strings.Replace(ddl, "'note','table','widget','document'", "'note','sheet','widget','document'", 1)
	if _, err := tx.Exec(ddl); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO studio_items_sheet (rowid,id,kind,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at,archived_at,origin)
 SELECT rowid,id,CASE kind WHEN 'table' THEN 'sheet' WHEN 'doc' THEN 'note' WHEN 'rich-document' THEN 'document' ELSE kind END,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at,archived_at,origin FROM studio_items;
 DROP TABLE studio_items;
 ALTER TABLE studio_items_sheet RENAME TO studio_items;
 ALTER TABLE studio_table_ids RENAME TO studio_sheet_ids;`); err != nil {
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
		return fmt.Errorf("foreign key violation after sheet migration")
	}
	return check.Err()
}

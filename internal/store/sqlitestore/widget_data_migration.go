package sqlitestore

import "database/sql"

func migrateWidgetData(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE widget_data (
    item_id TEXT PRIMARY KEY REFERENCES studio_items(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    data TEXT NOT NULL
);
`)
	return err
}

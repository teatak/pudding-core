package sqlitestore

import "database/sql"

func migrateWidgetPages(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE widget_pages (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    revision_hash TEXT NOT NULL,
    target_id TEXT NOT NULL UNIQUE,
    version INTEGER NOT NULL,
    data TEXT NOT NULL,
    interaction TEXT NOT NULL DEFAULT '',
    UNIQUE(item_id, scope, revision_hash)
);
`)
	return err
}

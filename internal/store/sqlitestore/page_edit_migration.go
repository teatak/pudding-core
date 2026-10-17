package sqlitestore

import "database/sql"

// v43 could be reached from v39 without the Page edit tables formerly created
// by v40. Their historical document names also serve Markdown Pages. Repair the
// missing tables without replacing existing leases or idempotency records.
func migratePageEdits(tx *sql.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS studio_document_edits (
    id TEXT PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    state TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS studio_document_edits_item ON studio_document_edits(item_id,state,updated_at);
CREATE TABLE IF NOT EXISTS studio_document_edit_requests (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    client_request_id TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    result TEXT NOT NULL,
    PRIMARY KEY(item_id,client_request_id)
);`)
	return err
}

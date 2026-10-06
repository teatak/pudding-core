package sqlitestore

import "database/sql"

// Version 31 codes required a later desktop approval. They must expire rather
// than acquire the new preauthorization meaning. Existing grants and identity
// are unchanged; the enclosing schema transaction provides rollback.
func migratePreauthorizedRemotePairings(tx *sql.Tx) error {
	_, err := tx.Exec(`DROP TABLE remote_pairings;
CREATE TABLE remote_pairings (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL CHECK (mode IN ('lan','relay')),
    origin TEXT NOT NULL,
    code_hash TEXT NOT NULL UNIQUE,
    expires_at INTEGER NOT NULL
);`)
	return err
}

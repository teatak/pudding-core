package sqlitestore

import "database/sql"

func migrateRemoteAccess(tx *sql.Tx) error {
	_, err := tx.Exec(`
CREATE TABLE remote_identity (
    singleton INTEGER PRIMARY KEY CHECK (singleton=1),
    desktop_id TEXT NOT NULL UNIQUE
);
CREATE TABLE remote_pairings (
    id TEXT PRIMARY KEY,
    mode TEXT NOT NULL CHECK (mode IN ('lan','relay')),
    origin TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','requested','approved')),
    code_hash TEXT UNIQUE,
    poll_hash TEXT UNIQUE,
    device_name TEXT NOT NULL DEFAULT '',
    expires_at INTEGER NOT NULL
);
CREATE TABLE remote_devices (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN ('lan','relay')),
    origin TEXT NOT NULL,
    credential_hash TEXT NOT NULL UNIQUE,
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
);
`)
	return err
}

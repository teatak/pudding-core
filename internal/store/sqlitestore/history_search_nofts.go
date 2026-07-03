//go:build !sqlite_fts5

package sqlitestore

import "database/sql"

func historySearchAvailable() bool { return false }

func ensureHistorySearch(*sql.DB) error { return nil }

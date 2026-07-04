//go:build !sqlite_fts5

package sqlitestore

import (
	"context"
	"database/sql"
)

func historySearchAvailable() bool { return false }

func ensureHistorySearch(*sql.DB) error { return nil }

func recoverableHistorySearchError(error) bool { return false }

func (s *Store) repairHistorySearch(context.Context) error { return nil }

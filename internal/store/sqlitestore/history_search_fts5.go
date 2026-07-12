//go:build sqlite_fts5

package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func historySearchAvailable() bool { return true }

func recoverableHistorySearchError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database disk image is malformed") ||
		strings.Contains(msg, "messages_fts") ||
		strings.Contains(msg, "messages_terms_fts")
}

func (s *Store) repairHistorySearch(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return resetMessagesFTS(ctx, s.db)
}

// ensureHistorySearch initializes only the current FTS schema. It does not
// inspect or transform older database layouts.
func ensureHistorySearch(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("sqlite: begin messages fts5 setup: %w", err)
	}
	defer tx.Rollback()
	messagesFTSExists, err := ftsTableExists(tx, "messages_fts")
	if err != nil {
		return fmt.Errorf("sqlite: inspect messages fts5: %w", err)
	}
	messagesTermsFTSExists, err := ftsTableExists(tx, "messages_terms_fts")
	if err != nil {
		return fmt.Errorf("sqlite: inspect message terms fts5: %w", err)
	}
	if _, err := tx.Exec(messagesFTS5Schema); err != nil {
		return fmt.Errorf("sqlite: apply messages fts5 schema: %w", err)
	}
	if !messagesFTSExists {
		if _, err := tx.Exec(`INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("sqlite: populate messages fts5: %w", err)
		}
	}
	if !messagesTermsFTSExists {
		if _, err := tx.Exec(`INSERT INTO messages_terms_fts(messages_terms_fts) VALUES('rebuild')`); err != nil {
			return fmt.Errorf("sqlite: populate message terms fts5: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: commit messages fts5 setup: %w", err)
	}
	return nil
}

// resetMessagesFTS repairs current-version derived indexes from canonical
// messages. The canonical tables are never rewritten here.
func resetMessagesFTS(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `
DROP TRIGGER IF EXISTS messages_ai;
DROP TRIGGER IF EXISTS messages_ad;
DROP TRIGGER IF EXISTS messages_au;
DROP TRIGGER IF EXISTS messages_terms_ai;
DROP TRIGGER IF EXISTS messages_terms_ad;
DROP TRIGGER IF EXISTS messages_terms_au;
DROP TABLE IF EXISTS messages_fts;
DROP TABLE IF EXISTS messages_terms_fts;
`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, messagesFTS5Schema); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages_fts(messages_fts) VALUES('rebuild')`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO messages_terms_fts(messages_terms_fts) VALUES('rebuild')`); err != nil {
		return err
	}
	return tx.Commit()
}

const messagesFTS5Schema = `
DROP TRIGGER IF EXISTS messages_ai;
DROP TRIGGER IF EXISTS messages_ad;
DROP TRIGGER IF EXISTS messages_au;
DROP TRIGGER IF EXISTS messages_terms_ai;
DROP TRIGGER IF EXISTS messages_terms_ad;
DROP TRIGGER IF EXISTS messages_terms_au;

CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
  text,
  content='messages',
  content_rowid='rowid',
  tokenize='trigram'
);

CREATE VIRTUAL TABLE IF NOT EXISTS messages_terms_fts USING fts5(
  search_tokens,
  content='messages',
  content_rowid='rowid',
  tokenize='unicode61 remove_diacritics 2'
);

CREATE TRIGGER IF NOT EXISTS messages_ai
  AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
  END;

CREATE TRIGGER IF NOT EXISTS messages_ad
  AFTER DELETE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text) VALUES('delete', old.rowid, old.text);
  END;

CREATE TRIGGER IF NOT EXISTS messages_au
  AFTER UPDATE OF text ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text) VALUES('delete', old.rowid, old.text);
    INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
  END;

CREATE TRIGGER IF NOT EXISTS messages_terms_ai
  AFTER INSERT ON messages BEGIN
    INSERT INTO messages_terms_fts(rowid, search_tokens) VALUES (new.rowid, new.search_tokens);
  END;

CREATE TRIGGER IF NOT EXISTS messages_terms_ad
  AFTER DELETE ON messages BEGIN
    INSERT INTO messages_terms_fts(messages_terms_fts, rowid, search_tokens) VALUES('delete', old.rowid, old.search_tokens);
  END;

CREATE TRIGGER IF NOT EXISTS messages_terms_au
  AFTER UPDATE OF search_tokens ON messages BEGIN
    INSERT INTO messages_terms_fts(messages_terms_fts, rowid, search_tokens) VALUES('delete', old.rowid, old.search_tokens);
    INSERT INTO messages_terms_fts(rowid, search_tokens) VALUES (new.rowid, new.search_tokens);
  END;
`

type ftsRowQuerier interface {
	QueryRow(query string, args ...any) *sql.Row
}

func ftsTableExists(db ftsRowQuerier, name string) (bool, error) {
	var count int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`,
		name,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

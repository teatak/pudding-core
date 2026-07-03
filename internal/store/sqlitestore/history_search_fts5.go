//go:build sqlite_fts5

package sqlitestore

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func historySearchAvailable() bool { return true }

func ensureHistorySearch(db *sql.DB) error {
	if err := rebuildMessagesFTSIfWrongTokenizer(db); err != nil {
		return fmt.Errorf("sqlite: rebuild messages fts5: %w", err)
	}
	if _, err := db.Exec(messagesFTS5Schema); err != nil {
		return fmt.Errorf("sqlite: apply messages fts5 schema: %w", err)
	}
	if err := backfillMessagesFTS(db); err != nil {
		return fmt.Errorf("sqlite: backfill messages fts5: %w", err)
	}
	return nil
}

const messagesFTS5Schema = `
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
  text,
  content='messages',
  content_rowid='rowid',
  tokenize='trigram'
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
  AFTER UPDATE ON messages BEGIN
    INSERT INTO messages_fts(messages_fts, rowid, text) VALUES('delete', old.rowid, old.text);
    INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
  END;
`

func rebuildMessagesFTSIfWrongTokenizer(db *sql.DB) error {
	var ftsSQL string
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='messages_fts'`,
	).Scan(&ftsSQL)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if strings.Contains(strings.ToLower(ftsSQL), "trigram") {
		return nil
	}
	if _, err := db.Exec(`
DROP TRIGGER IF EXISTS messages_ai;
DROP TRIGGER IF EXISTS messages_ad;
DROP TRIGGER IF EXISTS messages_au;
DROP TABLE messages_fts;
`); err != nil {
		return err
	}
	return nil
}

func backfillMessagesFTS(db *sql.DB) error {
	var ftsCount int
	if err := db.QueryRow(`SELECT count(*) FROM messages_fts`).Scan(&ftsCount); err != nil {
		return err
	}
	if ftsCount > 0 {
		return nil
	}
	var msgCount int
	if err := db.QueryRow(`SELECT count(*) FROM messages WHERE text != ''`).Scan(&msgCount); err != nil {
		return err
	}
	if msgCount == 0 {
		return nil
	}
	_, err := db.Exec(`INSERT INTO messages_fts(rowid, text) SELECT rowid, text FROM messages WHERE text != ''`)
	return err
}

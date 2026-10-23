package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/searchtext"
	"github.com/teatak/pudding-core/internal/store"
)

func migrateAssistantMemoriesV54(tx *sql.Tx) error {
	for _, column := range [][2]string{
		{"project_id", "TEXT NOT NULL DEFAULT ''"},
		{"revision", "INTEGER NOT NULL DEFAULT 1"},
		{"source_session_id", "TEXT NOT NULL DEFAULT ''"},
		{"source_turn_id", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := addTableColumnIfMissing(tx, "assistant_memories", column[0], column[1]); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS assistant_memory_terms (
    memory_id TEXT NOT NULL REFERENCES assistant_memories(id) ON DELETE CASCADE,
    term TEXT NOT NULL,
    PRIMARY KEY (term, memory_id)
);`); err != nil {
		return err
	}
	rows, err := tx.Query(`SELECT id,content FROM assistant_memories`)
	if err != nil {
		return err
	}
	var memories []store.AssistantMemory
	for rows.Next() {
		var m store.AssistantMemory
		if err := rows.Scan(&m.ID, &m.Content); err != nil {
			rows.Close()
			return err
		}
		memories = append(memories, m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, m := range memories {
		if err := indexAssistantMemory(context.Background(), tx, m.ID, m.Content); err != nil {
			return err
		}
	}
	return nil
}

func indexAssistantMemory(ctx context.Context, tx *sql.Tx, id, content string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM assistant_memory_terms WHERE memory_id=?`, id); err != nil {
		return err
	}
	for _, term := range searchtext.QueryTerms(content) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO assistant_memory_terms(memory_id,term) VALUES(?,?)`, id, term); err != nil {
			return err
		}
	}
	return nil
}

func requireAssistantConversation(ctx context.Context, tx *sql.Tx, sessionID string) error {
	var allowed bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM assistant_sessions WHERE assistant_id=? AND session_id=?)`, store.PersonalAssistantID, sessionID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return store.ErrAssistantToolUnavailable
	}
	return nil
}

const assistantMemoryColumns = `m.id,m.content,m.scope,m.source,m.created_at,m.updated_at,m.project_id,m.revision,m.source_session_id,m.source_turn_id`

func scanAssistantMemory(row interface{ Scan(...any) error }) (*store.AssistantMemory, error) {
	var m store.AssistantMemory
	var created, updated int64
	if err := row.Scan(&m.ID, &m.Content, &m.Scope, &m.Source, &created, &updated, &m.ProjectID, &m.Revision, &m.SourceSessionID, &m.SourceTurnID); err != nil {
		return nil, err
	}
	m.CreatedAt, m.UpdatedAt = time.UnixMilli(created).UTC(), time.UnixMilli(updated).UTC()
	return &m, nil
}

func (s *Store) SearchAssistantMemories(ctx context.Context, sessionID string, in store.AssistantMemoryQuery) ([]store.AssistantMemory, error) {
	if in.Limit < 1 || in.Limit > 50 || utf8.RuneCountInString(in.Query) > 4000 || (in.ID != "" && in.Query != "") {
		return nil, store.ErrInvalidAssistantMemory
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := requireAssistantConversation(ctx, tx, sessionID); err != nil {
		return nil, err
	}
	scope := "personal"
	if in.ProjectID != "" {
		scope = "project"
	}
	query := `SELECT ` + assistantMemoryColumns + ` FROM assistant_memories m`
	args := []any{}
	terms := searchtext.QueryTerms(in.Query)
	if len(terms) > 32 {
		terms = terms[:32]
	}
	if strings.TrimSpace(in.Query) != "" && len(terms) == 0 {
		return []store.AssistantMemory{}, tx.Commit()
	}
	if len(terms) > 0 {
		query += ` JOIN (SELECT memory_id,count(*) AS score FROM assistant_memory_terms WHERE term IN (` + strings.TrimSuffix(strings.Repeat("?,", len(terms)), ",") + `) GROUP BY memory_id) hit ON hit.memory_id=m.id`
		for _, term := range terms {
			args = append(args, term)
		}
	}
	query += ` WHERE m.assistant_id=? AND m.scope=? AND m.project_id=?`
	args = append(args, store.PersonalAssistantID, scope, in.ProjectID)
	if in.ID != "" {
		query += ` AND m.id=?`
		args = append(args, in.ID)
	}
	query += ` ORDER BY `
	if len(terms) > 0 {
		query += `hit.score DESC,`
	}
	query += `m.updated_at DESC,m.id LIMIT ?`
	args = append(args, in.Limit)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := []store.AssistantMemory{}
	for rows.Next() {
		m, err := scanAssistantMemory(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, *m)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, tx.Commit()
}

func (s *Store) SaveAssistantMemory(ctx context.Context, sessionID string, in store.AssistantMemoryWrite) (*store.AssistantMemory, error) {
	if err := in.Normalize(); err != nil {
		return nil, err
	}
	var out *store.AssistantMemory
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := requireAssistantConversation(ctx, tx, sessionID); err != nil {
			return err
		}
		var sourceValid bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM turns WHERE id=? AND session_id=?)`, in.SourceTurnID, sessionID).Scan(&sourceValid); err != nil {
			return err
		}
		if !sourceValid {
			return store.ErrInvalidAssistantMemory
		}
		scope := "personal"
		if in.ProjectID != "" {
			scope = "project"
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM projects WHERE id=?)`, in.ProjectID).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return store.ErrNotFound
			}
		}
		if in.ID == "" {
			// Exact duplicate creates are harmless, including after a lost tool result.
			current, err := scanAssistantMemory(tx.QueryRowContext(ctx, `SELECT `+assistantMemoryColumns+` FROM assistant_memories m WHERE assistant_id=? AND scope=? AND project_id=? AND content=? LIMIT 1`, store.PersonalAssistantID, scope, in.ProjectID, in.Content))
			if err == nil {
				out = current
				return nil
			}
			if !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			in.ID = store.NewID("mem")
		} else {
			current, err := scanAssistantMemory(tx.QueryRowContext(ctx, `SELECT `+assistantMemoryColumns+` FROM assistant_memories m WHERE assistant_id=? AND id=?`, store.PersonalAssistantID, in.ID))
			if errors.Is(err, sql.ErrNoRows) {
				if in.ID != communicationPreferenceID || in.ExpectedRevision != 0 {
					return store.ErrNotFound
				}
			} else if err != nil {
				return err
			} else {
				if current.ProjectID != in.ProjectID {
					return store.ErrInvalidAssistantMemory
				}
				if current.Content == in.Content {
					out = current
					return nil
				}
				if current.Revision != in.ExpectedRevision {
					return store.ErrAssistantMemoryConflict
				}
			}
		}
		now := time.Now().UTC().UnixMilli()
		if _, err := tx.ExecContext(ctx, `INSERT INTO assistant_memories(id,assistant_id,content,scope,source,created_at,updated_at,project_id,revision,source_session_id,source_turn_id)
VALUES(?,?,?,?,'conversation',?,?,?,1,?,?) ON CONFLICT(id) DO UPDATE SET content=excluded.content,source=excluded.source,updated_at=excluded.updated_at,revision=assistant_memories.revision+1,source_session_id=excluded.source_session_id,source_turn_id=excluded.source_turn_id`, in.ID, store.PersonalAssistantID, in.Content, scope, now, now, in.ProjectID, sessionID, in.SourceTurnID); err != nil {
			return err
		}
		if err := indexAssistantMemory(ctx, tx, in.ID, in.Content); err != nil {
			return err
		}
		if in.ID == communicationPreferenceID {
			if _, err := tx.ExecContext(ctx, `UPDATE assistants SET revision=revision+1,updated_at=? WHERE id=?`, now, store.PersonalAssistantID); err != nil {
				return err
			}
		}
		var err error
		out, err = scanAssistantMemory(tx.QueryRowContext(ctx, `SELECT `+assistantMemoryColumns+` FROM assistant_memories m WHERE id=?`, in.ID))
		return err
	})
	return out, err
}

func (s *Store) DeleteAssistantMemory(ctx context.Context, sessionID, id string, revision int64) error {
	if id == "" || revision < 1 || revision > 9007199254740991 {
		return store.ErrInvalidAssistantMemory
	}
	return s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := requireAssistantConversation(ctx, tx, sessionID); err != nil {
			return err
		}
		var current int64
		if err := tx.QueryRowContext(ctx, `SELECT revision FROM assistant_memories WHERE id=? AND assistant_id=?`, id, store.PersonalAssistantID).Scan(&current); errors.Is(err, sql.ErrNoRows) {
			return store.ErrNotFound
		} else if err != nil {
			return err
		}
		if current != revision {
			return store.ErrAssistantMemoryConflict
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM assistant_memories WHERE id=?`, id); err != nil {
			return err
		}
		if id == communicationPreferenceID {
			_, err := tx.ExecContext(ctx, `UPDATE assistants SET revision=revision+1,updated_at=? WHERE id=?`, time.Now().UTC().UnixMilli(), store.PersonalAssistantID)
			return err
		}
		return nil
	})
}

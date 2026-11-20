package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

// migrateTurnTopicV62 为 turns 增加 topic_title 冗余列与索引，避免查询大纲时扫描 messages.parts。
func migrateTurnTopicV62(tx *sql.Tx) error {
	if err := addTableColumnIfMissing(tx, "turns", "topic_title", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if _, err := tx.Exec(`CREATE INDEX IF NOT EXISTS turns_session_created ON turns(session_id, created_at)`); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE turns SET topic_title = COALESCE((
		SELECT json_extract(json_extract(p.value,'$.content'),'$.title')
		FROM messages m, json_each(m.parts) p
		WHERE m.session_id=turns.session_id AND m.turn_id=turns.id AND m.role='tool'
			AND json_extract(p.value,'$.type')='tool_result'
			AND json_extract(p.value,'$.name')='builtin_set_turn_topic'
			AND json_extract(p.value,'$.ok')=1
		ORDER BY m.turn_index DESC, m.rowid DESC, CAST(p.key AS INTEGER) DESC
		LIMIT 1
	), '')
	WHERE EXISTS (
		SELECT 1 FROM messages m
		WHERE m.session_id=turns.session_id AND m.turn_id=turns.id
			AND m.role='tool' AND INSTR(m.parts,'builtin_set_turn_topic')>0
	)`)
	return err
}

// migrateTurnOutlinePreviewV63 为 turns 增加 prompt_preview、answer_preview 冗余列并回填历史文本摘要。
func migrateTurnOutlinePreviewV63(tx *sql.Tx) error {
	if err := addTableColumnIfMissing(tx, "turns", "prompt_preview", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := addTableColumnIfMissing(tx, "turns", "answer_preview", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE turns SET
		prompt_preview = COALESCE((SELECT substr(m.text,1,512) FROM messages m
			WHERE m.session_id=turns.session_id AND m.turn_id=turns.id AND m.role='user'
			ORDER BY m.turn_index ASC, m.rowid ASC LIMIT 1), ''),
		answer_preview = COALESCE((SELECT substr(m.text,1,512) FROM messages m
			WHERE m.session_id=turns.session_id AND m.turn_id=turns.id AND m.role='assistant' AND m.kind='text' AND m.text<>''
			ORDER BY m.turn_index DESC, m.rowid DESC LIMIT 1), '')
		WHERE EXISTS (SELECT 1 FROM messages m WHERE m.session_id=turns.session_id AND m.turn_id=turns.id)`)
	return err
}

// truncateOutlinePreview 按 Unicode 字符截断至最多 512 字符，与 SQLite substr(text, 1, 512) 保持一致。
func truncateOutlinePreview(text string) string {
	if len(text) <= 512 {
		return text
	}
	count := 0
	for idx := range text {
		if count == 512 {
			return text[:idx]
		}
		count++
	}
	return text
}

// refreshTurnPromptPreviewTx 在消息顺序变更后重新对齐 turns.prompt_preview。
func refreshTurnPromptPreviewTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) error {
	_, err := tx.ExecContext(ctx, `UPDATE turns SET prompt_preview = COALESCE((
		SELECT substr(m.text,1,512) FROM messages m
		WHERE m.session_id=turns.session_id AND m.turn_id=turns.id AND m.role='user'
		ORDER BY m.turn_index ASC, m.rowid ASC LIMIT 1
	), '') WHERE session_id=? AND id=?`, sessionID, turnID)
	return err
}

// 只读单表短投影，不触碰 messages 与大字段。
func (s *Store) ListTurnOutline(ctx context.Context, sessionID string) ([]*store.TurnOutline, error) {
	entries := make([]*store.TurnOutline, 0)
	err := s.readTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT t.id, t.retry_of_turn_id, t.created_at, t.prompt_preview, t.answer_preview, t.topic_title
			FROM turns t WHERE t.session_id=? ORDER BY t.created_at ASC, t.rowid ASC`, sessionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			entry := &store.TurnOutline{}
			var created int64
			if err := rows.Scan(&entry.ID, &entry.RetryOfTurnID, &created, &entry.Prompt, &entry.Answer, &entry.TopicTitle); err != nil {
				return err
			}
			entry.CreatedAt = time.UnixMilli(created).UTC()
			entries = append(entries, entry)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func (s *Store) GetCurrentTurnTopic(ctx context.Context, sessionID string) (*store.TurnTopic, error) {
	defer s.readLock()()
	var topic store.TurnTopic
	var created int64
	err := s.readDB.QueryRowContext(ctx, `SELECT t.id, t.topic_title, t.created_at,
		(SELECT m.id FROM messages m WHERE m.session_id=t.session_id AND m.turn_id=t.id AND m.role='user'
		ORDER BY m.turn_index ASC, m.rowid ASC LIMIT 1)
		FROM turns t WHERE t.session_id=? AND t.topic_title<>''
		ORDER BY t.created_at DESC, t.rowid DESC LIMIT 1`, sessionID).Scan(&topic.TurnID, &topic.Title, &created, &topic.StartMessageID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	topic.StartedAt = time.UnixMilli(created).UTC()
	return &topic, nil
}

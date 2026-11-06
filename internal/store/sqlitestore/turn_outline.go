package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

// Topic markers use successful canonical tool results as their sole source of
// truth. Return only the title; tool payloads never travel with the outline.
const turnTopicTitleSQL = `COALESCE((SELECT json_extract(json_extract(p.value,'$.content'),'$.title')
	FROM messages m, json_each(m.parts) p
	WHERE m.session_id=t.session_id AND m.turn_id=t.id AND m.role='tool'
	AND json_extract(p.value,'$.type')='tool_result'
	AND json_extract(p.value,'$.name')='builtin_set_turn_topic'
	AND json_extract(p.value,'$.ok')=1
	ORDER BY m.turn_index DESC, m.rowid DESC, CAST(p.key AS INTEGER) DESC LIMIT 1), '')`

// Read only short text projections, without attachments, file diffs or provider state.
func (s *Store) ListTurnOutline(ctx context.Context, sessionID string) ([]*store.TurnOutline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := getSessionTx(ctx, tx, sessionID); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.id, t.retry_of_turn_id, t.created_at,
		COALESCE((SELECT substr(m.text,1,512) FROM messages m
			WHERE m.session_id=t.session_id AND m.turn_id=t.id AND m.role='user'
			ORDER BY m.turn_index ASC, m.rowid ASC LIMIT 1), ''),
		COALESCE((SELECT substr(m.text,1,512) FROM messages m
			WHERE m.session_id=t.session_id AND m.turn_id=t.id AND m.role='assistant' AND m.kind='text' AND m.text<>''
			ORDER BY m.turn_index DESC, m.rowid DESC LIMIT 1), ''), `+turnTopicTitleSQL+`
		FROM turns t WHERE t.session_id=? ORDER BY t.created_at ASC, t.rowid ASC`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]*store.TurnOutline, 0)
	for rows.Next() {
		entry := &store.TurnOutline{}
		var created int64
		if err := rows.Scan(&entry.ID, &entry.RetryOfTurnID, &created, &entry.Prompt, &entry.Answer, &entry.TopicTitle); err != nil {
			return nil, err
		}
		entry.CreatedAt = time.UnixMilli(created).UTC()
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (s *Store) GetCurrentTurnTopic(ctx context.Context, sessionID string) (*store.TurnTopic, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var topic store.TurnTopic
	var created int64
	err := s.db.QueryRowContext(ctx, `SELECT t.id, `+turnTopicTitleSQL+` AS title, t.created_at,
		(SELECT m.id FROM messages m WHERE m.session_id=t.session_id AND m.turn_id=t.id AND m.role='user'
		ORDER BY m.turn_index ASC, m.rowid ASC LIMIT 1)
		FROM turns t WHERE t.session_id=? AND title<>''
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

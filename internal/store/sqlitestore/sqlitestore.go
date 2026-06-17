// Package sqlitestore implements store.Store with SQLite.
package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

type Store struct {
	db *sql.DB
	mu sync.Mutex
}

func Open(path string) (*Store, error) {
	if err := ensureDBFile(path); err != nil {
		return nil, err
	}
	// foreign_keys 是 per-connection PRAGMA,必须走 DSN:database/sql 重建连接时
	// Exec 过的 PRAGMA 不会带过去,级联删除会静默失效。
	dsn := path + "?_foreign_keys=on&_busy_timeout=5000"
	if path == ":memory:" {
		dsn = "file::memory:?_foreign_keys=on"
	}
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &Store{db: db}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: enable wal: %w", err)
	}
	if _, err := db.Exec(store.SchemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite: apply schema: %w", err)
	}
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureSchema() error {
	if err := s.ensureColumn("turns", "model_config", `ALTER TABLE turns ADD COLUMN model_config TEXT NOT NULL DEFAULT '{}'`); err != nil {
		return err
	}
	if err := s.ensureColumn("sessions", "last_activity_at", `ALTER TABLE sessions ADD COLUMN last_activity_at INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE sessions SET last_activity_at=updated_at WHERE last_activity_at=0`); err != nil {
		return fmt.Errorf("sqlite: backfill sessions.last_activity_at: %w", err)
	}
	return nil
}

func (s *Store) ensureColumn(table, column, alter string) error {
	rows, err := s.db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.Exec(alter); err != nil {
		return fmt.Errorf("sqlite: add %s.%s: %w", table, column, err)
	}
	return nil
}

func ensureDBFile(path string) error {
	if path == ":memory:" {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("sqlite: create db file: %w", err)
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("sqlite: chmod db file: %w", err)
	}
	return nil
}

var _ store.Store = (*Store)(nil)

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateSession(ctx context.Context, sess *store.Session) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now()
		sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = now, now, now
		_, err := tx.ExecContext(ctx,
			`INSERT INTO sessions(id,title,provider,model,created_at,updated_at,last_activity_at) VALUES(?,?,?,?,?,?,?)`,
			sess.ID, sess.Title, sess.Provider, sess.Model, unixMS(now), unixMS(now), unixMS(now),
		)
		return err
	})
}

func (s *Store) GetSession(ctx context.Context, id string) (*store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var sess store.Session
	var created, updated, lastActivity int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id,title,provider,model,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &lastActivity, &sess.Running)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
	return &sess, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]*store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT id,title,provider,model,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions ORDER BY last_activity_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.Session, 0)
	for rows.Next() {
		var sess store.Session
		var created, updated, lastActivity int64
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &lastActivity, &sess.Running); err != nil {
			return nil, err
		}
		sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
		out = append(out, &sess)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSession(ctx context.Context, id string, upd store.SessionUpdate) (*store.Session, error) {
	var out *store.Session
	err := s.tx(ctx, func(tx *sql.Tx) error {
		sess, err := getSessionTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if upd.Title != nil {
			sess.Title = *upd.Title
		}
		if upd.Provider != nil {
			sess.Provider = *upd.Provider
		}
		if upd.Model != nil {
			sess.Model = *upd.Model
		}
		sess.UpdatedAt = time.Now()
		_, err = tx.ExecContext(ctx,
			`UPDATE sessions SET title=?, provider=?, model=?, updated_at=? WHERE id=?`,
			sess.Title, sess.Provider, sess.Model, unixMS(sess.UpdatedAt), id,
		)
		if err != nil {
			return err
		}
		out = sess
		return nil
	})
	return out, err
}

func (s *Store) DeleteSession(ctx context.Context, id string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE id=?`, id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return store.ErrNotFound
		}
		return nil
	})
}

func (s *Store) BeginTurn(ctx context.Context, in store.BeginTurnInput) (*store.BeginTurnResult, error) {
	var out *store.BeginTurnResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}

		existing, err := getTurnByClientMessageIDTx(ctx, tx, in.SessionID, in.ClientMessageID)
		if err == nil {
			msg, err := getUserMessageByClientMessageIDTx(ctx, tx, in.SessionID, in.ClientMessageID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return err
			}
			out = &store.BeginTurnResult{Duplicate: true, Turn: existing, UserMessage: msg}
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		if _, err := runningTurnTx(ctx, tx, in.SessionID); err == nil {
			return store.ErrTurnRunning
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		now := time.Now()
		turn := &store.Turn{
			ID:              in.TurnID,
			SessionID:       in.SessionID,
			ClientMessageID: in.ClientMessageID,
			Status:          store.TurnRunning,
			Provider:        in.Provider,
			Model:           in.Model,
			ModelConfig:     normalizeJSON(in.ModelConfig),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		msg := &store.Message{
			ID:              in.UserMessageID,
			SessionID:       in.SessionID,
			TurnID:          in.TurnID,
			Role:            store.RoleUser,
			Text:            in.UserText,
			ClientMessageID: in.ClientMessageID,
			CreatedAt:       now,
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO turns(id,session_id,client_message_id,status,provider,model,model_config,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			turn.ID, turn.SessionID, turn.ClientMessageID, turn.Status, turn.Provider, turn.Model, string(turn.ModelConfig), turn.Error, unixMS(now), unixMS(now),
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages(id,session_id,turn_id,role,text,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?)`,
			msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Text, msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
		); err != nil {
			return err
		}
		ev := event.Event{
			Seq:             0,
			SessionID:       in.SessionID,
			Kind:            event.TurnStarted,
			TurnID:          in.TurnID,
			ClientMessageID: in.ClientMessageID,
			UserMessageID:   in.UserMessageID,
		}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), in.SessionID); err != nil {
			return err
		}
		out = &store.BeginTurnResult{Turn: turn, UserMessage: msg, StartedEvent: &ev}
		return nil
	})
	return out, err
}

func (s *Store) FinishTurn(ctx context.Context, in store.FinishTurnInput) (*store.FinishTurnResult, error) {
	var out *store.FinishTurnResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		turn, err := getTurnTx(ctx, tx, in.TurnID)
		if err != nil {
			return err
		}
		if turn.Status != store.TurnRunning {
			return store.ErrNotFound
		}
		if in.Status != store.TurnCompleted && in.Status != store.TurnFailed && in.Status != store.TurnCancelled {
			return store.ErrNotFound
		}

		now := time.Now()
		if _, err := tx.ExecContext(ctx,
			`UPDATE turns SET status=?, error=?, updated_at=? WHERE id=?`,
			in.Status, in.Error, unixMS(now), in.TurnID,
		); err != nil {
			return err
		}

		res := &store.FinishTurnResult{}
		ev := event.Event{
			SessionID:   turn.SessionID,
			TurnID:      turn.ID,
			Interrupted: in.Interrupted,
			Error:       in.Error,
		}
		switch in.Status {
		case store.TurnCompleted:
			ev.Kind = event.TurnCompleted
		case store.TurnFailed:
			ev.Kind = event.TurnFailed
		case store.TurnCancelled:
			ev.Kind = event.TurnCancelled
		}
		if in.AssistantText != nil {
			msg := &store.Message{
				ID:          "msg_" + turn.ID,
				SessionID:   turn.SessionID,
				TurnID:      turn.ID,
				Role:        store.RoleAssistant,
				Text:        *in.AssistantText,
				Interrupted: in.Interrupted,
				CreatedAt:   now,
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO messages(id,session_id,turn_id,role,text,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?)`,
				msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Text, msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
			); err != nil {
				return err
			}
			ev.AssistantMessageID = msg.ID
			res.AssistantMessage = msg
		}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), turn.SessionID); err != nil {
			return err
		}
		res.FinalEvent = &ev
		out = res
		return nil
	})
	return out, err
}

func (s *Store) RunningTurn(ctx context.Context, sessionID string) (*store.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	return runningTurnTx(ctx, tx, sessionID)
}

func (s *Store) RunningTurns(ctx context.Context) ([]*store.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,session_id,client_message_id,status,provider,model,model_config,error,created_at,updated_at
		FROM turns WHERE status=?`, store.TurnRunning)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.Turn, 0)
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, turn)
	}
	return out, rows.Err()
}

func (s *Store) ListMessages(ctx context.Context, sessionID string, limit int) ([]*store.Message, error) {
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

	// rowid 作次级排序键:同毫秒落库的 user/assistant 消息顺序必须确定
	query := `SELECT id,session_id,turn_id,role,text,client_message_id,interrupted,created_at FROM messages WHERE session_id=? ORDER BY created_at ASC, rowid ASC`
	args := []any{sessionID}
	if limit > 0 {
		query = `SELECT id,session_id,turn_id,role,text,client_message_id,interrupted,created_at FROM (
			SELECT rowid AS rid, * FROM messages WHERE session_id=? ORDER BY created_at DESC, rowid DESC LIMIT ?
		) ORDER BY created_at ASC, rid ASC`
		args = append(args, limit)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.Message, 0)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func (s *Store) EventsAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]event.Event, error) {
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

	query := `SELECT payload FROM events WHERE session_id=? AND seq>? ORDER BY seq ASC`
	args := []any{sessionID, afterSeq}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]event.Event, 0)
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var ev event.Event
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) LatestSeq(ctx context.Context, sessionID string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.getSessionDB(ctx, sessionID); err != nil {
		return 0, err
	}
	var seq int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0) FROM events WHERE session_id=?`, sessionID,
	).Scan(&seq); err != nil {
		return 0, err
	}
	return seq, nil
}

// getSessionDB 是 getSessionTx 的免事务版本,服务只读单查。
func (s *Store) getSessionDB(ctx context.Context, id string) (*store.Session, error) {
	var sess store.Session
	var created, updated, lastActivity int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id,title,provider,model,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &lastActivity, &sess.Running)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
	return &sess, nil
}

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func getSessionTx(ctx context.Context, tx *sql.Tx, id string) (*store.Session, error) {
	var sess store.Session
	var created, updated, lastActivity int64
	err := tx.QueryRowContext(ctx,
		`SELECT id,title,provider,model,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &created, &updated, &lastActivity, &sess.Running)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
	return &sess, nil
}

func getTurnTx(ctx context.Context, tx *sql.Tx, id string) (*store.Turn, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id,session_id,client_message_id,status,provider,model,model_config,error,created_at,updated_at FROM turns WHERE id=?`, id,
	)
	turn, err := scanTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return turn, nil
}

func getTurnByClientMessageIDTx(ctx context.Context, tx *sql.Tx, sessionID, clientMessageID string) (*store.Turn, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id,session_id,client_message_id,status,provider,model,model_config,error,created_at,updated_at
		FROM turns WHERE session_id=? AND client_message_id=?`,
		sessionID, clientMessageID,
	)
	turn, err := scanTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return turn, nil
}

func runningTurnTx(ctx context.Context, tx *sql.Tx, sessionID string) (*store.Turn, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id,session_id,client_message_id,status,provider,model,model_config,error,created_at,updated_at
		FROM turns WHERE session_id=? AND status=?`,
		sessionID, store.TurnRunning,
	)
	turn, err := scanTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return turn, nil
}

func getUserMessageByClientMessageIDTx(ctx context.Context, tx *sql.Tx, sessionID, clientMessageID string) (*store.Message, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id,session_id,turn_id,role,text,client_message_id,interrupted,created_at
		FROM messages WHERE session_id=? AND role=? AND client_message_id=?`,
		sessionID, store.RoleUser, clientMessageID,
	)
	msg, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return msg, err
}

func insertEventTx(ctx context.Context, tx *sql.Tx, ev *event.Event) error {
	if !ev.Persistent() {
		return nil
	}
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE session_id=?`, ev.SessionID,
	).Scan(&seq); err != nil {
		return err
	}
	ev.Seq = seq
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(session_id,seq,kind,turn_id,payload,created_at) VALUES(?,?,?,?,?,?)`,
		ev.SessionID, ev.Seq, ev.Kind, ev.TurnID, string(payload), unixMS(time.Now()),
	); err != nil {
		return err
	}
	if ev.Seq > store.EventsRetainPerSession {
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM events WHERE session_id=? AND seq<=?`,
			ev.SessionID, ev.Seq-store.EventsRetainPerSession,
		); err != nil {
			return err
		}
	}
	return nil
}

type messageScanner interface {
	Scan(dest ...any) error
}

func scanTurn(row messageScanner) (*store.Turn, error) {
	var turn store.Turn
	var modelConfig string
	var created, updated int64
	err := row.Scan(
		&turn.ID,
		&turn.SessionID,
		&turn.ClientMessageID,
		&turn.Status,
		&turn.Provider,
		&turn.Model,
		&modelConfig,
		&turn.Error,
		&created,
		&updated,
	)
	if err != nil {
		return nil, err
	}
	turn.ModelConfig = normalizeJSON(json.RawMessage(modelConfig))
	turn.CreatedAt, turn.UpdatedAt = timeFromMS(created), timeFromMS(updated)
	return &turn, nil
}

func scanMessage(row messageScanner) (*store.Message, error) {
	var msg store.Message
	var interrupted int
	var created int64
	err := row.Scan(&msg.ID, &msg.SessionID, &msg.TurnID, &msg.Role, &msg.Text, &msg.ClientMessageID, &interrupted, &created)
	if err != nil {
		return nil, err
	}
	msg.Interrupted = interrupted != 0
	msg.CreatedAt = timeFromMS(created)
	return &msg, nil
}

func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	if !json.Valid(raw) {
		return json.RawMessage(`{}`)
	}
	return append(json.RawMessage(nil), raw...)
}

func unixMS(t time.Time) int64 { return t.UnixNano() / int64(time.Millisecond) }

func timeFromMS(ms int64) time.Time { return time.Unix(0, ms*int64(time.Millisecond)) }

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

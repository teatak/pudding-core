// Package sqlitestore implements store.Store with SQLite.
package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
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
	if err := ensureSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureHistorySearch(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func ensureSchema(db *sql.DB) error {
	if has, err := tableHasColumn(db, "queued_inputs", "parts"); err != nil {
		return err
	} else if !has {
		if _, err := db.Exec(`ALTER TABLE queued_inputs ADD COLUMN parts TEXT NOT NULL DEFAULT '[]'`); err != nil {
			return fmt.Errorf("sqlite: migrate queued_inputs.parts: %w", err)
		}
	}
	return nil
}

func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		return false, fmt.Errorf("sqlite: inspect table %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
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

const messageSelectColumns = `id,session_id,turn_id,role,kind,text,parts,turn_index,metadata,client_message_id,interrupted,created_at`

const messageSelectColumnsAliasM = `m.id,m.session_id,m.turn_id,m.role,m.kind,m.text,m.parts,m.turn_index,m.metadata,m.client_message_id,m.interrupted,m.created_at`

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) CreateSession(ctx context.Context, sess *store.Session) error {
	if err := store.NormalizeSessionProviderModel(sess); err != nil {
		return err
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		now := time.Now()
		sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = now, now, now
		_, err := tx.ExecContext(ctx,
			`INSERT INTO sessions(id,title,provider,model,reasoning_effort,reasoning_model_key,active_mode,mode_lease,workspace_dirs,pinned,pinned_order,created_at,updated_at,last_activity_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			sess.ID, sess.Title, sess.Provider, sess.Model, sess.ReasoningEffort, sess.ReasoningModelKey, sess.ActiveMode, sess.ModeLease, encodeStringSlice(sess.WorkspaceDirs), boolInt(sess.Pinned), sess.PinnedOrder, unixMS(now), unixMS(now), unixMS(now),
		)
		return err
	})
}

func (s *Store) GetSession(ctx context.Context, id string) (*store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var sess store.Session
	var created, updated, lastActivity int64
	var workspaceDirs string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,title,provider,model,reasoning_effort,reasoning_model_key,active_mode,mode_lease,workspace_dirs,pinned,pinned_order,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &sess.ReasoningEffort, &sess.ReasoningModelKey, &sess.ActiveMode, &sess.ModeLease, &workspaceDirs, &sess.Pinned, &sess.PinnedOrder, &created, &updated, &lastActivity, &sess.Running)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.ActiveMode = store.NormalizeAgentMode(sess.ActiveMode)
	if sess.ActiveMode == "" {
		sess.ActiveMode = store.ModeChat
	}
	sess.WorkspaceDirs = store.NormalizeWorkspaceDirs(decodeStringSlice(workspaceDirs))
	sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
	return &sess, nil
}

func (s *Store) ListSessions(ctx context.Context) ([]*store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.QueryContext(ctx, `SELECT id,title,provider,model,reasoning_effort,reasoning_model_key,active_mode,mode_lease,workspace_dirs,pinned,pinned_order,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions ORDER BY last_activity_at DESC, created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.Session, 0)
	for rows.Next() {
		var sess store.Session
		var created, updated, lastActivity int64
		var workspaceDirs string
		if err := rows.Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &sess.ReasoningEffort, &sess.ReasoningModelKey, &sess.ActiveMode, &sess.ModeLease, &workspaceDirs, &sess.Pinned, &sess.PinnedOrder, &created, &updated, &lastActivity, &sess.Running); err != nil {
			return nil, err
		}
		sess.ActiveMode = store.NormalizeAgentMode(sess.ActiveMode)
		if sess.ActiveMode == "" {
			sess.ActiveMode = store.ModeChat
		}
		sess.WorkspaceDirs = store.NormalizeWorkspaceDirs(decodeStringSlice(workspaceDirs))
		sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
		out = append(out, &sess)
	}
	return out, rows.Err()
}

func (s *Store) UpdateSession(ctx context.Context, id string, upd store.SessionUpdate) (*store.Session, error) {
	if err := store.NormalizeSessionUpdate(&upd); err != nil {
		return nil, err
	}
	var out *store.Session
	err := s.tx(ctx, func(tx *sql.Tx) error {
		sess, err := getSessionTx(ctx, tx, id)
		if err != nil {
			return err
		}
		if upd.Title != nil {
			sess.Title = *upd.Title
		}
		modelChanged := false
		if upd.Provider != nil {
			sess.Provider = *upd.Provider
			modelChanged = true
		}
		if upd.Model != nil {
			sess.Model = *upd.Model
			modelChanged = true
		}
		if modelChanged {
			sess.ReasoningEffort = ""
			sess.ReasoningModelKey = ""
		}
		if upd.ReasoningEffort != nil {
			sess.ReasoningEffort = *upd.ReasoningEffort
			sess.ReasoningModelKey = ""
			if sess.ReasoningEffort != "" {
				sess.ReasoningModelKey = sessionModelKey(sess.Provider, sess.Model)
			}
		}
		if upd.ActiveMode != nil {
			sess.ActiveMode = *upd.ActiveMode
		}
		if upd.ModeLease != nil {
			sess.ModeLease = *upd.ModeLease
		}
		if upd.WorkspaceDirs != nil {
			sess.WorkspaceDirs = append([]string(nil), (*upd.WorkspaceDirs)...)
		}
		if upd.Pinned != nil {
			sess.Pinned = *upd.Pinned
		}
		if upd.PinnedOrder != nil {
			sess.PinnedOrder = *upd.PinnedOrder
		}
		sess.UpdatedAt = time.Now()
		_, err = tx.ExecContext(ctx,
			`UPDATE sessions SET title=?, provider=?, model=?, reasoning_effort=?, reasoning_model_key=?, active_mode=?, mode_lease=?, workspace_dirs=?, pinned=?, pinned_order=?, updated_at=? WHERE id=?`,
			sess.Title, sess.Provider, sess.Model, sess.ReasoningEffort, sess.ReasoningModelKey, sess.ActiveMode, sess.ModeLease, encodeStringSlice(sess.WorkspaceDirs), boolInt(sess.Pinned), sess.PinnedOrder, unixMS(sess.UpdatedAt), id,
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
	err := s.deleteSession(ctx, id)
	if err == nil || !recoverableHistorySearchError(err) {
		return err
	}
	if repairErr := s.repairHistorySearch(ctx); repairErr != nil {
		return fmt.Errorf("%w; repair history search: %v", err, repairErr)
	}
	return s.deleteSession(ctx, id)
}

func (s *Store) deleteSession(ctx context.Context, id string) error {
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
		mode := in.Mode
		if mode == "" {
			mode = store.ModeChat
		}
		mode = store.NormalizeAgentMode(mode)
		if mode == "" {
			mode = store.ModeChat
		}
		turn := &store.Turn{
			ID:              in.TurnID,
			SessionID:       in.SessionID,
			ClientMessageID: in.ClientMessageID,
			Status:          store.TurnRunning,
			Provider:        in.Provider,
			Model:           in.Model,
			Mode:            mode,
			ModelConfig:     normalizeJSON(in.ModelConfig),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		msg := &store.Message{
			ID:              in.UserMessageID,
			SessionID:       in.SessionID,
			TurnID:          in.TurnID,
			Role:            store.RoleUser,
			Kind:            store.MessageKindText,
			Text:            in.UserText,
			Parts:           store.UserInputParts(in.UserText, in.UserParts),
			TurnIndex:       0,
			ClientMessageID: in.ClientMessageID,
			CreatedAt:       now,
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			turn.ID, turn.SessionID, turn.ClientMessageID, turn.Status, turn.Provider, turn.Model, turn.Mode, string(turn.ModelConfig), turn.Error, unixMS(now), unixMS(now),
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages(id,session_id,turn_id,role,kind,text,parts,turn_index,metadata,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Kind, msg.Text, encodeParts(msg.Parts), msg.TurnIndex, string(normalizeJSON(msg.Metadata)), msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
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
			Text:            in.UserText,
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

func (s *Store) BeginSystemTurn(ctx context.Context, in store.BeginSystemTurnInput) (*store.BeginSystemTurnResult, error) {
	var out *store.BeginSystemTurnResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}

		existing, err := getTurnByClientMessageIDTx(ctx, tx, in.SessionID, in.ClientMessageID)
		if err == nil {
			out = &store.BeginSystemTurnResult{Duplicate: true, Turn: existing}
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
		mode := in.Mode
		if mode == "" {
			mode = store.ModeChat
		}
		mode = store.NormalizeAgentMode(mode)
		if mode == "" {
			mode = store.ModeChat
		}
		turn := &store.Turn{
			ID:              in.TurnID,
			SessionID:       in.SessionID,
			ClientMessageID: in.ClientMessageID,
			Status:          store.TurnRunning,
			Provider:        in.Provider,
			Model:           in.Model,
			Mode:            mode,
			ModelConfig:     normalizeJSON(in.ModelConfig),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		msg := &store.Message{
			ID:        in.SystemMessageID,
			SessionID: in.SessionID,
			TurnID:    in.TurnID,
			Role:      store.RoleSystem,
			Kind:      store.MessageKindText,
			Text:      in.Text,
			Parts:     store.TextPart(in.Text),
			TurnIndex: 0,
			CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			turn.ID, turn.SessionID, turn.ClientMessageID, turn.Status, turn.Provider, turn.Model, turn.Mode, string(turn.ModelConfig), turn.Error, unixMS(now), unixMS(now),
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages(id,session_id,turn_id,role,kind,text,parts,turn_index,metadata,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Kind, msg.Text, encodeParts(msg.Parts), msg.TurnIndex, string(normalizeJSON(msg.Metadata)), msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
		); err != nil {
			return err
		}
		ev := event.Event{
			Seq:             0,
			SessionID:       in.SessionID,
			Kind:            event.TurnStarted,
			TurnID:          in.TurnID,
			ClientMessageID: in.ClientMessageID,
		}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), in.SessionID); err != nil {
			return err
		}
		out = &store.BeginSystemTurnResult{Turn: turn, SystemMessage: msg, StartedEvent: &ev}
		return nil
	})
	return out, err
}

func (s *Store) QueueInput(ctx context.Context, in store.QueueInputInput) (*store.QueueInputResult, error) {
	var out *store.QueueInputResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		if existing, err := getTurnByClientMessageIDTx(ctx, tx, in.SessionID, in.ClientMessageID); err == nil {
			out = &store.QueueInputResult{Duplicate: true, ExistingTurn: existing}
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if existing, err := getQueuedInputTx(ctx, tx, in.SessionID, in.ClientMessageID); err == nil {
			out = &store.QueueInputResult{Duplicate: true, Input: existing}
			return nil
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		now := time.Now()
		mode := in.Mode
		if mode == "" {
			mode = store.ModeChat
		}
		mode = store.NormalizeAgentMode(mode)
		if mode == "" {
			mode = store.ModeChat
		}
		input := &store.QueuedInput{
			SessionID:       in.SessionID,
			ClientMessageID: in.ClientMessageID,
			Text:            in.Text,
			Parts:           store.UserInputParts(in.Text, in.Parts),
			Status:          store.QueuedInputQueued,
			Provider:        in.Provider,
			Model:           in.Model,
			Mode:            mode,
			ModelConfig:     normalizeJSON(in.ModelConfig),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO queued_inputs(session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			input.SessionID, input.ClientMessageID, input.Text, encodeParts(input.Parts), input.Status, input.Provider, input.Model, input.Mode, string(input.ModelConfig), input.TurnID, unixMS(now), unixMS(now),
		); err != nil {
			return err
		}
		ev := event.Event{
			SessionID:       input.SessionID,
			Kind:            event.InputQueued,
			ClientMessageID: input.ClientMessageID,
			Text:            input.Text,
			Status:          string(input.Status),
		}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), in.SessionID); err != nil {
			return err
		}
		out = &store.QueueInputResult{Input: input, QueuedEvent: &ev}
		return nil
	})
	return out, err
}

func (s *Store) ListQueuedInputs(ctx context.Context, sessionID string) ([]*store.QueuedInput, error) {
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
	rows, err := tx.QueryContext(ctx,
		`SELECT session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at
		FROM queued_inputs
		WHERE session_id=? AND status IN (?,?)
		ORDER BY created_at ASC, rowid ASC`,
		sessionID, store.QueuedInputQueued, store.QueuedInputEditing,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.QueuedInput, 0)
	for rows.Next() {
		input, err := scanQueuedInput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, input)
	}
	return out, rows.Err()
}

func (s *Store) HasQueuedInputs(ctx context.Context, sessionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM queued_inputs WHERE session_id=? AND status IN (?,?))`,
		sessionID, store.QueuedInputQueued, store.QueuedInputEditing,
	).Scan(&exists)
	return exists, err
}

func (s *Store) UpdateQueuedInput(ctx context.Context, in store.UpdateQueuedInputInput) (*store.UpdateQueuedInputResult, error) {
	var out *store.UpdateQueuedInputResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		input, err := getQueuedInputTx(ctx, tx, in.SessionID, in.ClientMessageID)
		if err != nil {
			return err
		}
		if input.Status == store.QueuedInputPromoted {
			return store.ErrNotFound
		}
		if in.Text != nil {
			input.Text = *in.Text
		}
		if in.Status != nil {
			input.Status = *in.Status
		}
		if !validQueuedInputStatus(input.Status) || input.Status == store.QueuedInputPromoted {
			return store.ErrNotFound
		}
		input.UpdatedAt = time.Now()
		input.Parts = store.ReplaceUserInputText(input.Parts, input.Text)
		if _, err := tx.ExecContext(ctx,
			`UPDATE queued_inputs SET text=?, parts=?, status=?, updated_at=? WHERE session_id=? AND client_message_id=?`,
			input.Text, encodeParts(input.Parts), input.Status, unixMS(input.UpdatedAt), input.SessionID, input.ClientMessageID,
		); err != nil {
			return err
		}
		ev := event.Event{
			SessionID:       input.SessionID,
			Kind:            event.InputUpdated,
			ClientMessageID: input.ClientMessageID,
			Text:            input.Text,
			Status:          string(input.Status),
		}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		out = &store.UpdateQueuedInputResult{Input: input, Event: &ev}
		return nil
	})
	return out, err
}

func (s *Store) PromoteNextQueuedInput(ctx context.Context, in store.PromoteQueuedInputInput) (*store.PromoteQueuedInputResult, error) {
	var out *store.PromoteQueuedInputResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		if _, err := runningTurnTx(ctx, tx, in.SessionID); err == nil {
			return store.ErrTurnRunning
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}

		for {
			input, err := firstQueuedInputTx(ctx, tx, in.SessionID)
			if err != nil {
				return err
			}
			switch input.Status {
			case store.QueuedInputCancelled:
				now := time.Now()
				if _, err := tx.ExecContext(ctx,
					`UPDATE queued_inputs SET status=?, updated_at=? WHERE session_id=? AND client_message_id=?`,
					store.QueuedInputPromoted, unixMS(now), input.SessionID, input.ClientMessageID,
				); err != nil {
					return err
				}
				continue
			case store.QueuedInputEditing:
				return store.ErrQueueBlocked
			case store.QueuedInputQueued:
				now := time.Now()
				turn := &store.Turn{
					ID:              in.TurnID,
					SessionID:       input.SessionID,
					ClientMessageID: input.ClientMessageID,
					Status:          store.TurnRunning,
					Provider:        input.Provider,
					Model:           input.Model,
					Mode:            input.Mode,
					ModelConfig:     normalizeJSON(input.ModelConfig),
					CreatedAt:       now,
					UpdatedAt:       now,
				}
				msg := &store.Message{
					ID:              in.UserMessageID,
					SessionID:       input.SessionID,
					TurnID:          turn.ID,
					Role:            store.RoleUser,
					Kind:            store.MessageKindText,
					Text:            input.Text,
					Parts:           store.UserInputParts(input.Text, input.Parts),
					TurnIndex:       0,
					ClientMessageID: input.ClientMessageID,
					CreatedAt:       now,
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
					turn.ID, turn.SessionID, turn.ClientMessageID, turn.Status, turn.Provider, turn.Model, turn.Mode, string(turn.ModelConfig), turn.Error, unixMS(now), unixMS(now),
				); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx,
					`INSERT INTO messages(id,session_id,turn_id,role,kind,text,parts,turn_index,metadata,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
					msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Kind, msg.Text, encodeParts(msg.Parts), msg.TurnIndex, string(normalizeJSON(msg.Metadata)), msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
				); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx,
					`UPDATE queued_inputs SET status=?, turn_id=?, updated_at=? WHERE session_id=? AND client_message_id=?`,
					store.QueuedInputPromoted, turn.ID, unixMS(now), input.SessionID, input.ClientMessageID,
				); err != nil {
					return err
				}
				input.Status = store.QueuedInputPromoted
				input.TurnID = turn.ID
				input.UpdatedAt = now
				ev := event.Event{
					SessionID:       turn.SessionID,
					Kind:            event.TurnStarted,
					TurnID:          turn.ID,
					ClientMessageID: turn.ClientMessageID,
					UserMessageID:   msg.ID,
					Text:            input.Text,
				}
				if err := insertEventTx(ctx, tx, &ev); err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), in.SessionID); err != nil {
					return err
				}
				out = &store.PromoteQueuedInputResult{Input: input, Turn: turn, UserMessage: msg, StartedEvent: &ev}
				return nil
			default:
				return store.ErrNotFound
			}
		}
	})
	return out, err
}

func (s *Store) QueuedSessions(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx,
		`SELECT DISTINCT session_id FROM queued_inputs WHERE status=? ORDER BY session_id ASC`,
		store.QueuedInputQueued,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			return nil, err
		}
		out = append(out, sessionID)
	}
	return out, rows.Err()
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
		mode := turn.Mode
		if in.Mode != "" {
			if normalized := store.NormalizeAgentMode(in.Mode); normalized != "" {
				mode = normalized
			}
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE turns SET status=?, mode=?, error=?, updated_at=? WHERE id=?`,
			in.Status, mode, in.Error, unixMS(now), in.TurnID,
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
		maxIndex, firstOutputID, err := turnOutputStatsTx(ctx, tx, turn.SessionID, turn.ID)
		if err != nil {
			return err
		}
		segments := store.FinishAssistantOutputSegments(in)
		if maxIndex > 0 && len(in.AssistantParts) == 0 {
			segments = nil
		}
		messages, err := appendTurnOutputSegmentsTx(ctx, tx, turn, maxIndex, segments, in.Interrupted, now)
		if err != nil {
			return err
		}
		if firstOutputID != "" {
			ev.AssistantMessageID = firstOutputID
		}
		for i, msg := range messages {
			if ev.AssistantMessageID == "" && i == 0 {
				ev.AssistantMessageID = msg.ID
			}
			if i == 0 {
				res.AssistantMessage = msg
			}
			res.AssistantMessages = append(res.AssistantMessages, msg)
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

func (s *Store) AppendTurnOutput(ctx context.Context, in store.AppendTurnOutputInput) (*store.AppendTurnOutputResult, error) {
	var out *store.AppendTurnOutputResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		turn, err := getTurnTx(ctx, tx, in.TurnID)
		if err != nil {
			return err
		}
		if turn.Status != store.TurnRunning {
			return store.ErrNotFound
		}
		segments := store.AssistantOutputSegments(in.Parts)
		if len(segments) == 0 {
			out = &store.AppendTurnOutputResult{}
			return nil
		}
		now := time.Now()
		maxIndex, _, err := turnOutputStatsTx(ctx, tx, turn.SessionID, turn.ID)
		if err != nil {
			return err
		}
		messages, err := appendTurnOutputSegmentsTx(ctx, tx, turn, maxIndex, segments, false, now)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE turns SET updated_at=? WHERE id=?`, unixMS(now), turn.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), turn.SessionID); err != nil {
			return err
		}
		out = &store.AppendTurnOutputResult{Messages: messages}
		return nil
	})
	return out, err
}

func (s *Store) AppendCompactSummary(ctx context.Context, in store.AppendCompactSummaryInput) (*store.AppendCompactSummaryResult, error) {
	var out *store.AppendCompactSummaryResult
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, in.SessionID); err != nil {
			return err
		}
		if _, err := runningTurnTx(ctx, tx, in.SessionID); err == nil {
			return store.ErrTurnRunning
		} else if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		now := time.Now()
		mode := in.Mode
		if mode == "" {
			mode = store.ModeChat
		}
		mode = store.NormalizeAgentMode(mode)
		if mode == "" {
			mode = store.ModeChat
		}
		turn := &store.Turn{
			ID:              in.TurnID,
			SessionID:       in.SessionID,
			ClientMessageID: in.ClientMessageID,
			Status:          store.TurnCompleted,
			Provider:        in.Provider,
			Model:           in.Model,
			Mode:            mode,
			ModelConfig:     normalizeJSON(in.ModelConfig),
			CreatedAt:       now,
			UpdatedAt:       now,
		}
		msg := &store.Message{
			ID:        in.MessageID,
			SessionID: in.SessionID,
			TurnID:    in.TurnID,
			Role:      store.RoleSummary,
			Kind:      store.MessageKindSummary,
			Text:      in.Text,
			Parts:     store.TextPart(in.Text),
			TurnIndex: 1,
			Metadata:  normalizeJSON(in.Metadata),
			CreatedAt: now,
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			turn.ID, turn.SessionID, turn.ClientMessageID, turn.Status, turn.Provider, turn.Model, turn.Mode, string(turn.ModelConfig), turn.Error, unixMS(now), unixMS(now),
		); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages(id,session_id,turn_id,role,kind,text,parts,turn_index,metadata,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Kind, msg.Text, encodeParts(msg.Parts), msg.TurnIndex, string(normalizeJSON(msg.Metadata)), msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
		); err != nil {
			return err
		}
		ev := event.Event{
			SessionID:          in.SessionID,
			Kind:               event.TurnCompleted,
			TurnID:             in.TurnID,
			AssistantMessageID: in.MessageID,
		}
		if err := insertEventTx(ctx, tx, &ev); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET last_activity_at=? WHERE id=?`, unixMS(now), in.SessionID); err != nil {
			return err
		}
		out = &store.AppendCompactSummaryResult{Turn: turn, Message: msg, FinalEvent: &ev}
		return nil
	})
	return out, err
}

func (s *Store) RecordUsage(ctx context.Context, in store.UsageRecordInput) (*store.UsageHourlyStat, error) {
	when := in.OccurredAt
	if when.IsZero() {
		when = time.Now()
	}
	hour := when.UTC().Truncate(time.Hour)
	now := time.Now()
	requestCount := in.RequestCount
	if requestCount <= 0 {
		requestCount = 1
	}
	inputUncached := clampNonNegative(in.InputUncachedTokens)
	inputCached := clampNonNegative(in.InputCachedTokens)
	cacheCreation := clampNonNegative(in.CacheCreationTokens)
	outputContent := clampNonNegative(in.OutputContentTokens)
	outputReasoning := clampNonNegative(in.OutputReasoningTokens)
	model := in.Model

	err := s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO usage(
				hour_start_at,model,request_count,input_uncached_tokens,input_cached_tokens,cache_creation_tokens,
				output_content_tokens,output_reasoning_tokens,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?)
			ON CONFLICT(hour_start_at,model) DO UPDATE SET
				request_count=request_count+excluded.request_count,
				input_uncached_tokens=input_uncached_tokens+excluded.input_uncached_tokens,
				input_cached_tokens=input_cached_tokens+excluded.input_cached_tokens,
				cache_creation_tokens=cache_creation_tokens+excluded.cache_creation_tokens,
				output_content_tokens=output_content_tokens+excluded.output_content_tokens,
				output_reasoning_tokens=output_reasoning_tokens+excluded.output_reasoning_tokens,
				updated_at=excluded.updated_at`,
			unixMS(hour), model, requestCount, inputUncached, inputCached, cacheCreation,
			outputContent, outputReasoning, unixMS(now),
		)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.usageHourlyStat(ctx, hour, model)
}

func (s *Store) UsageHourlyStats(ctx context.Context, from, to time.Time) ([]*store.UsageHourlyStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	from = from.UTC().Truncate(time.Hour)
	var rows *sql.Rows
	var err error
	if to.IsZero() {
		rows, err = s.db.QueryContext(ctx,
			`SELECT hour_start_at,model,request_count,input_uncached_tokens,input_cached_tokens,cache_creation_tokens,
				output_content_tokens,output_reasoning_tokens,updated_at
			FROM usage
			WHERE hour_start_at>=?
			ORDER BY hour_start_at ASC, model ASC`,
			unixMS(from),
		)
	} else {
		to = to.UTC().Truncate(time.Hour)
		rows, err = s.db.QueryContext(ctx,
			`SELECT hour_start_at,model,request_count,input_uncached_tokens,input_cached_tokens,cache_creation_tokens,
				output_content_tokens,output_reasoning_tokens,updated_at
			FROM usage
			WHERE hour_start_at>=? AND hour_start_at<?
			ORDER BY hour_start_at ASC, model ASC`,
			unixMS(from), unixMS(to),
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]*store.UsageHourlyStat, 0)
	for rows.Next() {
		stat, err := scanUsageHourlyStat(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, stat)
	}
	return out, rows.Err()
}

func (s *Store) usageHourlyStat(ctx context.Context, hour time.Time, model string) (*store.UsageHourlyStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanUsageHourlyStat(s.db.QueryRowContext(ctx,
		`SELECT hour_start_at,model,request_count,input_uncached_tokens,input_cached_tokens,cache_creation_tokens,
			output_content_tokens,output_reasoning_tokens,updated_at
		FROM usage
		WHERE hour_start_at=? AND model=?`,
		unixMS(hour.UTC().Truncate(time.Hour)), model,
	))
}

func (s *Store) RecordSessionUsage(ctx context.Context, sessionID string, in store.UsageRecordInput) (*store.SessionUsageStat, error) {
	now := time.Now()
	requestCount := in.RequestCount
	if requestCount <= 0 {
		requestCount = 1
	}
	inputUncached := clampNonNegative(in.InputUncachedTokens)
	inputCached := clampNonNegative(in.InputCachedTokens)
	cacheCreation := clampNonNegative(in.CacheCreationTokens)
	outputContent := clampNonNegative(in.OutputContentTokens)
	outputReasoning := clampNonNegative(in.OutputReasoningTokens)

	err := s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := getSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO session_usage(
				session_id,request_count,
				last_input_uncached_tokens,last_input_cached_tokens,last_cache_creation_tokens,
				last_output_content_tokens,last_output_reasoning_tokens,
				cumulative_input_uncached_tokens,cumulative_input_cached_tokens,cumulative_cache_creation_tokens,
				cumulative_output_content_tokens,cumulative_output_reasoning_tokens,updated_at
			) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
			ON CONFLICT(session_id) DO UPDATE SET
				request_count=request_count+excluded.request_count,
				last_input_uncached_tokens=excluded.last_input_uncached_tokens,
				last_input_cached_tokens=excluded.last_input_cached_tokens,
				last_cache_creation_tokens=excluded.last_cache_creation_tokens,
				last_output_content_tokens=excluded.last_output_content_tokens,
				last_output_reasoning_tokens=excluded.last_output_reasoning_tokens,
				cumulative_input_uncached_tokens=cumulative_input_uncached_tokens+excluded.cumulative_input_uncached_tokens,
				cumulative_input_cached_tokens=cumulative_input_cached_tokens+excluded.cumulative_input_cached_tokens,
				cumulative_cache_creation_tokens=cumulative_cache_creation_tokens+excluded.cumulative_cache_creation_tokens,
				cumulative_output_content_tokens=cumulative_output_content_tokens+excluded.cumulative_output_content_tokens,
				cumulative_output_reasoning_tokens=cumulative_output_reasoning_tokens+excluded.cumulative_output_reasoning_tokens,
				updated_at=excluded.updated_at`,
			sessionID, requestCount,
			inputUncached, inputCached, cacheCreation,
			outputContent, outputReasoning,
			inputUncached, inputCached, cacheCreation,
			outputContent, outputReasoning, unixMS(now),
		)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.SessionUsage(ctx, sessionID)
}

func (s *Store) SessionUsage(ctx context.Context, sessionID string) (*store.SessionUsageStat, error) {
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
	stat, err := sessionUsageTx(ctx, tx, sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return &store.SessionUsageStat{SessionID: sessionID}, nil
	}
	if err != nil {
		return nil, err
	}
	return stat, nil
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
		`SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at
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
	page, err := s.ListMessagesPage(ctx, sessionID, "", limit)
	if err != nil {
		return nil, err
	}
	return page.Messages, nil
}

func (s *Store) ListMessagesPage(ctx context.Context, sessionID string, beforeMessageID string, limit int) (*store.MessagePage, error) {
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

	// rowid 作次级排序键:同毫秒落库的跨 turn 消息顺序必须按写入顺序确定。
	// turn_index 只在同一 turn 内排序(messagesForTurnTx)使用。
	query := `SELECT ` + messageSelectColumns + ` FROM messages WHERE session_id=? ORDER BY created_at ASC, rowid ASC`
	args := []any{sessionID}
	if beforeMessageID != "" {
		var beforeCreated int64
		var beforeRowID int64
		err := tx.QueryRowContext(ctx,
			`SELECT created_at,rowid FROM messages WHERE session_id=? AND id=?`,
			sessionID, beforeMessageID,
		).Scan(&beforeCreated, &beforeRowID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		query = `SELECT ` + messageSelectColumns + `
			FROM messages
			WHERE session_id=? AND (created_at < ? OR (created_at=? AND rowid < ?))
			ORDER BY created_at ASC, rowid ASC`
		args = []any{sessionID, beforeCreated, beforeCreated, beforeRowID}
	}
	fetchLimit := limit
	if fetchLimit > 0 {
		fetchLimit++
		if beforeMessageID == "" {
			query = `SELECT ` + messageSelectColumns + ` FROM (
				SELECT rowid AS rid, * FROM messages WHERE session_id=? ORDER BY created_at DESC, rowid DESC LIMIT ?
			) ORDER BY created_at ASC, rid ASC`
			args = []any{sessionID, fetchLimit}
		} else {
			query = `SELECT ` + messageSelectColumns + ` FROM (
				SELECT rowid AS rid, * FROM messages
				WHERE session_id=? AND (created_at < ? OR (created_at=? AND rowid < ?))
				ORDER BY created_at DESC, rowid DESC LIMIT ?
			) ORDER BY created_at ASC, rid ASC`
			args = append(args, fetchLimit)
		}
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasMore := false
	if limit > 0 && len(out) > limit {
		hasMore = true
		out = out[1:]
	}
	return &store.MessagePage{Messages: out, HasMore: hasMore}, nil
}

func (s *Store) GetMessage(ctx context.Context, sessionID string, messageID string) (*store.Message, error) {
	sessionID = strings.TrimSpace(sessionID)
	messageID = strings.TrimSpace(messageID)
	if sessionID == "" || messageID == "" {
		return nil, store.ErrNotFound
	}
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
	msg, err := scanMessage(tx.QueryRowContext(ctx,
		`SELECT `+messageSelectColumns+`
		FROM messages WHERE session_id=? AND id=?`,
		sessionID, messageID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return msg, err
}

func (s *Store) SearchMessages(ctx context.Context, in store.MessageSearchInput) ([]*store.Message, error) {
	if !historySearchAvailable() {
		return nil, store.ErrHistorySearchUnavailable
	}
	sessionID := strings.TrimSpace(in.SessionID)
	query := strings.TrimSpace(in.Query)
	if sessionID == "" {
		return nil, store.ErrInvalidSession
	}
	if query == "" {
		return nil, nil
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

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
	rows, err := tx.QueryContext(ctx,
		`SELECT `+messageSelectColumnsAliasM+`
		FROM messages_fts f
		JOIN messages m ON m.rowid = f.rowid
		WHERE f.text MATCH ? AND m.session_id=?
		ORDER BY rank
		LIMIT ?`,
		query, sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("sqlite: search messages: %w", err)
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
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) ListTurnsPage(ctx context.Context, sessionID string, beforeTurnID string, limit int) (*store.TurnPage, error) {
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

	query := `SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at FROM turns WHERE session_id=? ORDER BY created_at ASC, rowid ASC`
	args := []any{sessionID}
	if beforeTurnID != "" {
		var beforeCreated int64
		var beforeRowID int64
		err := tx.QueryRowContext(ctx,
			`SELECT created_at,rowid FROM turns WHERE session_id=? AND id=?`,
			sessionID, beforeTurnID,
		).Scan(&beforeCreated, &beforeRowID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		if err != nil {
			return nil, err
		}
		query = `SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at
			FROM turns
			WHERE session_id=? AND (created_at < ? OR (created_at=? AND rowid < ?))
			ORDER BY created_at ASC, rowid ASC`
		args = []any{sessionID, beforeCreated, beforeCreated, beforeRowID}
	}
	fetchLimit := limit
	if fetchLimit > 0 {
		fetchLimit++
		if beforeTurnID == "" {
			query = `SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at FROM (
				SELECT rowid AS rid, * FROM turns WHERE session_id=? ORDER BY created_at DESC, rowid DESC LIMIT ?
			) ORDER BY created_at ASC, rid ASC`
			args = []any{sessionID, fetchLimit}
		} else {
			query = `SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at FROM (
				SELECT rowid AS rid, * FROM turns
				WHERE session_id=? AND (created_at < ? OR (created_at=? AND rowid < ?))
				ORDER BY created_at DESC, rowid DESC LIMIT ?
			) ORDER BY created_at ASC, rid ASC`
			args = append(args, fetchLimit)
		}
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	turns := make([]*store.Turn, 0)
	for rows.Next() {
		turn, err := scanTurn(rows)
		if err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hasMore := false
	if limit > 0 && len(turns) > limit {
		hasMore = true
		turns = turns[1:]
	}
	out := make([]*store.ConversationTurn, 0, len(turns))
	for _, turn := range turns {
		messages, err := messagesForTurnTx(ctx, tx, turn.SessionID, turn.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, conversationTurnFromSQL(turn, messages))
	}
	return &store.TurnPage{Turns: out, HasMore: hasMore}, nil
}

func (s *Store) GetConversationTurn(ctx context.Context, sessionID string, turnID string) (*store.ConversationTurn, error) {
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
	row := tx.QueryRowContext(ctx,
		`SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at FROM turns WHERE session_id=? AND id=?`,
		sessionID, turnID,
	)
	turn, err := scanTurn(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	messages, err := messagesForTurnTx(ctx, tx, turn.SessionID, turn.ID)
	if err != nil {
		return nil, err
	}
	return conversationTurnFromSQL(turn, messages), nil
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
	var workspaceDirs string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,title,provider,model,reasoning_effort,reasoning_model_key,active_mode,mode_lease,workspace_dirs,pinned,pinned_order,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &sess.ReasoningEffort, &sess.ReasoningModelKey, &sess.ActiveMode, &sess.ModeLease, &workspaceDirs, &sess.Pinned, &sess.PinnedOrder, &created, &updated, &lastActivity, &sess.Running)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.ActiveMode = store.NormalizeAgentMode(sess.ActiveMode)
	if sess.ActiveMode == "" {
		sess.ActiveMode = store.ModeChat
	}
	sess.WorkspaceDirs = store.NormalizeWorkspaceDirs(decodeStringSlice(workspaceDirs))
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
	var workspaceDirs string
	err := tx.QueryRowContext(ctx,
		`SELECT id,title,provider,model,reasoning_effort,reasoning_model_key,active_mode,mode_lease,workspace_dirs,pinned,pinned_order,created_at,updated_at,last_activity_at,EXISTS(SELECT 1 FROM turns t WHERE t.session_id=sessions.id AND t.status='running') FROM sessions WHERE id=?`, id,
	).Scan(&sess.ID, &sess.Title, &sess.Provider, &sess.Model, &sess.ReasoningEffort, &sess.ReasoningModelKey, &sess.ActiveMode, &sess.ModeLease, &workspaceDirs, &sess.Pinned, &sess.PinnedOrder, &created, &updated, &lastActivity, &sess.Running)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	sess.ActiveMode = store.NormalizeAgentMode(sess.ActiveMode)
	if sess.ActiveMode == "" {
		sess.ActiveMode = store.ModeChat
	}
	sess.WorkspaceDirs = store.NormalizeWorkspaceDirs(decodeStringSlice(workspaceDirs))
	sess.CreatedAt, sess.UpdatedAt, sess.LastActivityAt = timeFromMS(created), timeFromMS(updated), timeFromMS(lastActivity)
	return &sess, nil
}

func getTurnTx(ctx context.Context, tx *sql.Tx, id string) (*store.Turn, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at FROM turns WHERE id=?`, id,
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
		`SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at
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
		`SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at
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
		`SELECT `+messageSelectColumns+`
		FROM messages WHERE session_id=? AND role=? AND client_message_id=?`,
		sessionID, store.RoleUser, clientMessageID,
	)
	msg, err := scanMessage(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return msg, err
}

func getQueuedInputTx(ctx context.Context, tx *sql.Tx, sessionID, clientMessageID string) (*store.QueuedInput, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at
		FROM queued_inputs WHERE session_id=? AND client_message_id=?`,
		sessionID, clientMessageID,
	)
	input, err := scanQueuedInput(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return input, err
}

func firstQueuedInputTx(ctx context.Context, tx *sql.Tx, sessionID string) (*store.QueuedInput, error) {
	row := tx.QueryRowContext(ctx,
		`SELECT session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at
		FROM queued_inputs
		WHERE session_id=? AND status IN (?,?,?)
		ORDER BY created_at ASC, rowid ASC
		LIMIT 1`,
		sessionID, store.QueuedInputQueued, store.QueuedInputEditing, store.QueuedInputCancelled,
	)
	input, err := scanQueuedInput(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return input, err
}

func messagesForTurnTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) ([]*store.Message, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT `+messageSelectColumns+`
		FROM messages WHERE session_id=? AND turn_id=? ORDER BY created_at ASC, turn_index ASC, rowid ASC`,
		sessionID, turnID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.Message, 0, 2)
	for rows.Next() {
		msg, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, rows.Err()
}

func conversationTurnFromSQL(turn *store.Turn, messages []*store.Message) *store.ConversationTurn {
	return &store.ConversationTurn{
		ID:              turn.ID,
		SessionID:       turn.SessionID,
		ClientMessageID: turn.ClientMessageID,
		Status:          turn.Status,
		Provider:        turn.Provider,
		Model:           turn.Model,
		Mode:            turn.Mode,
		Error:           turn.Error,
		CreatedAt:       turn.CreatedAt,
		UpdatedAt:       turn.UpdatedAt,
		Messages:        messages,
	}
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
		&turn.Mode,
		&modelConfig,
		&turn.Error,
		&created,
		&updated,
	)
	if err != nil {
		return nil, err
	}
	turn.ModelConfig = normalizeJSON(json.RawMessage(modelConfig))
	turn.Mode = store.NormalizeAgentMode(turn.Mode)
	if turn.Mode == "" {
		turn.Mode = store.ModeChat
	}
	turn.CreatedAt, turn.UpdatedAt = timeFromMS(created), timeFromMS(updated)
	return &turn, nil
}

func scanMessage(row messageScanner) (*store.Message, error) {
	var msg store.Message
	var parts string
	var metadata string
	var interrupted int
	var created int64
	err := row.Scan(
		&msg.ID,
		&msg.SessionID,
		&msg.TurnID,
		&msg.Role,
		&msg.Kind,
		&msg.Text,
		&parts,
		&msg.TurnIndex,
		&metadata,
		&msg.ClientMessageID,
		&interrupted,
		&created,
	)
	if err != nil {
		return nil, err
	}
	msg.Parts = decodeParts(parts)
	msg.Metadata = normalizeMessageMetadata(metadata)
	msg.Interrupted = interrupted != 0
	msg.CreatedAt = timeFromMS(created)
	return &msg, nil
}

func scanQueuedInput(row messageScanner) (*store.QueuedInput, error) {
	var input store.QueuedInput
	var parts string
	var modelConfig string
	var created, updated int64
	err := row.Scan(
		&input.SessionID,
		&input.ClientMessageID,
		&input.Text,
		&parts,
		&input.Status,
		&input.Provider,
		&input.Model,
		&input.Mode,
		&modelConfig,
		&input.TurnID,
		&created,
		&updated,
	)
	if err != nil {
		return nil, err
	}
	input.Parts = decodeParts(parts)
	input.ModelConfig = normalizeJSON(json.RawMessage(modelConfig))
	input.Mode = store.NormalizeAgentMode(input.Mode)
	if input.Mode == "" {
		input.Mode = store.ModeChat
	}
	input.CreatedAt, input.UpdatedAt = timeFromMS(created), timeFromMS(updated)
	return &input, nil
}

func scanUsageHourlyStat(row messageScanner) (*store.UsageHourlyStat, error) {
	var stat store.UsageHourlyStat
	var hourStart, updated int64
	err := row.Scan(
		&hourStart,
		&stat.Model,
		&stat.RequestCount,
		&stat.InputUncachedTokens,
		&stat.InputCachedTokens,
		&stat.CacheCreationTokens,
		&stat.OutputContentTokens,
		&stat.OutputReasoningTokens,
		&updated,
	)
	if err != nil {
		return nil, err
	}
	stat.HourStartAt = timeFromMS(hourStart)
	stat.UpdatedAt = timeFromMS(updated)
	return &stat, nil
}

func sessionUsageTx(ctx context.Context, tx *sql.Tx, sessionID string) (*store.SessionUsageStat, error) {
	return scanSessionUsageStat(tx.QueryRowContext(ctx,
		`SELECT session_id,request_count,
			last_input_uncached_tokens,last_input_cached_tokens,last_cache_creation_tokens,
			last_output_content_tokens,last_output_reasoning_tokens,
			cumulative_input_uncached_tokens,cumulative_input_cached_tokens,cumulative_cache_creation_tokens,
			cumulative_output_content_tokens,cumulative_output_reasoning_tokens,updated_at
		FROM session_usage WHERE session_id=?`,
		sessionID,
	))
}

func scanSessionUsageStat(row messageScanner) (*store.SessionUsageStat, error) {
	var stat store.SessionUsageStat
	var updated int64
	err := row.Scan(
		&stat.SessionID,
		&stat.RequestCount,
		&stat.LastInputUncachedTokens,
		&stat.LastInputCachedTokens,
		&stat.LastCacheCreationTokens,
		&stat.LastOutputContentTokens,
		&stat.LastOutputReasoningTokens,
		&stat.CumulativeInputUncachedTokens,
		&stat.CumulativeInputCachedTokens,
		&stat.CumulativeCacheCreationTokens,
		&stat.CumulativeOutputContentTokens,
		&stat.CumulativeOutputReasoningTokens,
		&updated,
	)
	if err != nil {
		return nil, err
	}
	stat.UpdatedAt = timeFromMS(updated)
	return &stat, nil
}

func validQueuedInputStatus(status store.QueuedInputStatus) bool {
	switch status {
	case store.QueuedInputQueued, store.QueuedInputEditing, store.QueuedInputCancelled, store.QueuedInputPromoted:
		return true
	default:
		return false
	}
}

func turnOutputStatsTx(ctx context.Context, tx *sql.Tx, sessionID, turnID string) (int, string, error) {
	var maxIndex int
	err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(turn_index),0) FROM messages WHERE session_id=? AND turn_id=? AND turn_index>0`,
		sessionID, turnID,
	).Scan(&maxIndex)
	if err != nil {
		return 0, "", err
	}
	var firstID string
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM messages WHERE session_id=? AND turn_id=? AND turn_index>0 ORDER BY turn_index ASC LIMIT 1`,
		sessionID, turnID,
	).Scan(&firstID)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return maxIndex, firstID, err
}

func appendTurnOutputSegmentsTx(ctx context.Context, tx *sql.Tx, turn *store.Turn, maxIndex int, segments []store.AssistantOutputSegment, interrupted bool, now time.Time) ([]*store.Message, error) {
	out := make([]*store.Message, 0, len(segments))
	for i, segment := range segments {
		turnIndex := maxIndex + i + 1
		msg := &store.Message{
			ID:          store.NewID("msg"),
			SessionID:   turn.SessionID,
			TurnID:      turn.ID,
			Role:        segment.Role,
			Kind:        segment.Kind,
			Text:        segment.Text,
			Parts:       store.CloneContentParts(segment.Parts),
			TurnIndex:   turnIndex,
			Interrupted: interrupted && i == len(segments)-1,
			CreatedAt:   now,
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO messages(id,session_id,turn_id,role,kind,text,parts,turn_index,metadata,client_message_id,interrupted,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			msg.ID, msg.SessionID, msg.TurnID, msg.Role, msg.Kind, msg.Text, encodeParts(msg.Parts), msg.TurnIndex, string(normalizeJSON(msg.Metadata)), msg.ClientMessageID, boolInt(msg.Interrupted), unixMS(now),
		); err != nil {
			return nil, err
		}
		out = append(out, msg)
	}
	return out, nil
}

func encodeParts(parts []store.ContentPart) string {
	normalized := store.NormalizeContentParts(parts)
	if len(normalized) == 0 {
		return "[]"
	}
	data, err := json.Marshal(normalized)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeParts(raw string) []store.ContentPart {
	var parts []store.ContentPart
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &parts)
	}
	return store.NormalizeContentParts(parts)
}

func encodeStringSlice(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func decodeStringSlice(raw string) []string {
	var out []string
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}

func sessionModelKey(providerName, model string) string {
	return strings.TrimSpace(providerName) + ":" + strings.TrimSpace(model)
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

func normalizeMessageMetadata(raw string) json.RawMessage {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil
	}
	return normalizeJSON(json.RawMessage(raw))
}

func unixMS(t time.Time) int64 { return t.UnixNano() / int64(time.Millisecond) }

func timeFromMS(ms int64) time.Time { return time.Unix(0, ms*int64(time.Millisecond)) }

func clampNonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

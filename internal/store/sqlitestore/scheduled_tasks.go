package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const scheduledTaskColumns = `id,session_id,name,prompt,schedule,enabled,deleted,revision,schedule_revision,next_at,created_at,updated_at,request_id,request_hash`
const scheduledRunColumns = `id,task_id,session_id,name,prompt,definition_revision,source,scheduled_for,accepted_at,client_message_id,handoff,reason,skipped_through,trigger_key,schedule`
const scheduledTurnColumns = `id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at,retry_of_turn_id`

func scanScheduledTask(row messageScanner) (*store.ScheduledTask, error) {
	t := &store.ScheduledTask{}
	var raw string
	var next, created, updated int64
	if err := row.Scan(&t.ID, &t.SessionID, &t.Name, &t.Prompt, &raw, &t.Enabled, &t.Deleted, &t.Revision, &t.ScheduleRevision, &next, &created, &updated, &t.RequestID, &t.RequestHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(raw), &t.Schedule); err != nil {
		return nil, err
	}
	t.CreatedAt = time.UnixMilli(created).UTC()
	t.UpdatedAt = time.UnixMilli(updated).UTC()
	if next != 0 {
		v := time.UnixMilli(next).UTC()
		t.NextAt = &v
	}
	return t, nil
}
func scheduledTaskArgs(t *store.ScheduledTask) []any {
	raw, _ := json.Marshal(t.Schedule)
	var next int64
	if t.NextAt != nil {
		next = t.NextAt.UnixMilli()
	}
	return []any{t.ID, t.SessionID, t.Name, t.Prompt, string(raw), t.Enabled, t.Deleted, t.Revision, t.ScheduleRevision, next, t.CreatedAt.UnixMilli(), t.UpdatedAt.UnixMilli(), t.RequestID, t.RequestHash}
}
func putScheduledTaskTx(ctx context.Context, tx *sql.Tx, t *store.ScheduledTask) error {
	args := scheduledTaskArgs(t)
	args = append(args[1:], t.ID)
	_, err := tx.ExecContext(ctx, `UPDATE scheduled_tasks SET session_id=?,name=?,prompt=?,schedule=?,enabled=?,deleted=?,revision=?,schedule_revision=?,next_at=?,created_at=?,updated_at=?,request_id=?,request_hash=? WHERE id=?`, args...)
	return err
}
func scheduledTargetTx(ctx context.Context, tx *sql.Tx, id string) error {
	if _, err := getSessionTx(ctx, tx, id); err != nil {
		return err
	}
	owner, err := parentSessionIDTx(ctx, tx, id)
	if err != nil {
		return err
	}
	if owner != "" {
		return store.ErrInvalidSessionRelation
	}
	return nil
}
func (s *Store) CreateScheduledTask(ctx context.Context, t *store.ScheduledTask) (*store.ScheduledTask, error) {
	var out *store.ScheduledTask
	err := s.tx(ctx, func(tx *sql.Tx) error {
		prev, err := scanScheduledTask(tx.QueryRowContext(ctx, `SELECT `+scheduledTaskColumns+` FROM scheduled_tasks WHERE session_id=? AND request_id=?`, t.SessionID, t.RequestID))
		if err == nil {
			if prev.RequestHash != t.RequestHash {
				return store.ErrScheduleConflict
			}
			out = prev
			return nil
		}
		if !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if err := scheduledTargetTx(ctx, tx, t.SessionID); err != nil {
			return err
		}
		if t.NextAt == nil {
			return store.ErrInvalidSchedule
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO scheduled_tasks(`+scheduledTaskColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, scheduledTaskArgs(t)...)
		out = t
		return err
	})
	return out, err
}
func (s *Store) GetScheduledTask(ctx context.Context, id string) (*store.ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanScheduledTask(s.db.QueryRowContext(ctx, `SELECT `+scheduledTaskColumns+` FROM scheduled_tasks WHERE id=?`, id))
}
func (s *Store) ListScheduledTasks(ctx context.Context, sessionID string, includeDeleted bool) ([]*store.ScheduledTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduledTaskColumns+` FROM scheduled_tasks WHERE (?='' OR session_id=?) AND (? OR deleted=0) ORDER BY created_at DESC,id`, sessionID, sessionID, includeDeleted)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.ScheduledTask, 0)
	for rows.Next() {
		t, err := scanScheduledTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) UpdateScheduledTask(ctx context.Context, id string, in store.ScheduledTaskUpdate, now time.Time) (*store.ScheduledTask, error) {
	var out *store.ScheduledTask
	err := s.tx(ctx, func(tx *sql.Tx) error {
		t, err := scanScheduledTask(tx.QueryRowContext(ctx, `SELECT `+scheduledTaskColumns+` FROM scheduled_tasks WHERE id=?`, id))
		if err != nil {
			return err
		}
		if in.Enabled != nil && *in.Enabled {
			if err := scheduledTargetTx(ctx, tx, t.SessionID); err != nil {
				return err
			}
		}
		if err := store.ApplyScheduledTaskUpdate(t, in, now); err != nil {
			return err
		}
		if err := putScheduledTaskTx(ctx, tx, t); err != nil {
			return err
		}
		out = t
		return nil
	})
	return out, err
}
func scanScheduledRun(row messageScanner) (*store.ScheduledTaskRun, error) {
	r := &store.ScheduledTaskRun{}
	var at, accepted, through int64
	var schedule string
	if err := row.Scan(&r.ID, &r.TaskID, &r.SessionID, &r.Name, &r.Prompt, &r.DefinitionRevision, &r.Source, &at, &accepted, &r.ClientMessageID, &r.Handoff, &r.Reason, &through, &r.Key, &schedule); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal([]byte(schedule), &r.Schedule); err != nil {
		return nil, err
	}
	r.ScheduledFor = time.UnixMilli(at).UTC()
	r.AcceptedAt = time.UnixMilli(accepted).UTC()
	if through != 0 {
		v := time.UnixMilli(through).UTC()
		r.SkippedThrough = &v
	}
	return r, nil
}
func (s *Store) AcceptScheduledTask(ctx context.Context, id string, in store.ScheduledTaskAccept) (*store.ScheduledTaskRun, error) {
	var out *store.ScheduledTaskRun
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if in.RequestID != "" {
			prev, err := scanScheduledRun(tx.QueryRowContext(ctx, `SELECT `+scheduledRunColumns+` FROM scheduled_task_runs WHERE task_id=? AND trigger_key=?`, id, "manual:"+in.RequestID))
			if err == nil {
				out = prev
				return nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		t, err := scanScheduledTask(tx.QueryRowContext(ctx, `SELECT `+scheduledTaskColumns+` FROM scheduled_tasks WHERE id=?`, id))
		if err != nil {
			return err
		}
		if err := scheduledTargetTx(ctx, tx, t.SessionID); err != nil {
			return err
		}
		r, err := store.PrepareScheduledTaskRun(t, in)
		if err != nil {
			return err
		}
		var through int64
		if r.SkippedThrough != nil {
			through = r.SkippedThrough.UnixMilli()
		}
		schedule, _ := json.Marshal(r.Schedule)
		_, err = tx.ExecContext(ctx, `INSERT INTO scheduled_task_runs(`+scheduledRunColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, r.TaskID, r.SessionID, r.Name, r.Prompt, r.DefinitionRevision, r.Source, r.ScheduledFor.UnixMilli(), r.AcceptedAt.UnixMilli(), r.ClientMessageID, r.Handoff, r.Reason, through, r.Key, string(schedule))
		if err != nil {
			return err
		}
		if err := putScheduledTaskTx(ctx, tx, t); err != nil {
			return err
		}
		out = r
		return nil
	})
	return out, err
}
func (s *Store) ListScheduledTaskRuns(ctx context.Context, taskID string, limit, offset int) ([]*store.ScheduledTaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduledRunColumns+` FROM scheduled_task_runs WHERE (?='' OR task_id=?) ORDER BY accepted_at DESC,rowid DESC LIMIT ? OFFSET ?`, taskID, taskID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.ScheduledTaskRun, 0)
	for rows.Next() {
		r, err := scanScheduledRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) PendingScheduledTaskRuns(ctx context.Context) ([]*store.ScheduledTaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT `+scheduledRunColumns+` FROM scheduled_task_runs WHERE handoff='pending' ORDER BY accepted_at,rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*store.ScheduledTaskRun, 0)
	for rows.Next() {
		r, err := scanScheduledRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (s *Store) SetScheduledTaskHandoff(ctx context.Context, id, state, reason string) error {
	if state != "submitted" && state != "failed" {
		return store.ErrInvalidSchedule
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE scheduled_task_runs SET handoff=?,reason=? WHERE id=? AND handoff='pending'`, state, reason, id)
		return err
	})
}
func (s *Store) FindInputTurn(ctx context.Context, sessionID, clientID string) (*store.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := scanTurn(s.db.QueryRowContext(ctx, `SELECT `+scheduledTurnColumns+` FROM turns WHERE session_id=? AND id IN (SELECT turn_id FROM messages WHERE session_id=? AND client_message_id=? AND role='user')`, sessionID, sessionID, clientID))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	return t, err
}
func (s *Store) FindRetryTurn(ctx context.Context, sessionID, turnID string) (*store.Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := scanTurn(s.db.QueryRowContext(ctx, `SELECT `+scheduledTurnColumns+` FROM turns WHERE session_id=? AND retry_of_turn_id=?`, sessionID, turnID))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	return t, err
}

func (s *Store) FindQueuedInput(ctx context.Context, sessionID, clientID string) (*store.QueuedInput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, err := scanQueuedInput(s.db.QueryRowContext(ctx, `SELECT session_id,client_message_id,text,parts,status,provider,model,mode,model_config,turn_id,created_at,updated_at FROM queued_inputs WHERE session_id=? AND client_message_id=?`, sessionID, clientID))
	if errors.Is(err, sql.ErrNoRows) {
		err = store.ErrNotFound
	}
	return q, err
}

func (s *Store) GetScheduledTaskRun(ctx context.Context, sessionID, id string) (*store.ScheduledTaskRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanScheduledRun(s.db.QueryRowContext(ctx, `SELECT `+scheduledRunColumns+` FROM scheduled_task_runs WHERE session_id=? AND id=?`, sessionID, id))
}
func (s *Store) GetScheduledTaskTurn(ctx context.Context, sessionID, id string) (*store.ConversationTurn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	t, err := scanTurn(tx.QueryRowContext(ctx, `SELECT id,session_id,client_message_id,status,provider,model,mode,model_config,error,created_at,updated_at,retry_of_turn_id FROM turns WHERE session_id=? AND id=?`, sessionID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	messages, err := messagesForTurnTx(ctx, tx, sessionID, id)
	if err != nil {
		return nil, err
	}
	return &store.ConversationTurn{ID: t.ID, SessionID: t.SessionID, Status: t.Status, Error: t.Error, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, Messages: messages}, nil
}

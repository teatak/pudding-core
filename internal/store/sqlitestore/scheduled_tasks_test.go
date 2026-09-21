package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestScheduledTasksPersistenceAndAdmission(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "tasks.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, st, "target")
	now := time.Now().UTC().Truncate(time.Millisecond)
	in := store.ScheduledTaskCreate{SessionID: "target", RequestID: "create", Name: "Daily check", Prompt: "Check the project", Schedule: store.TaskSchedule{Kind: "once", Timezone: "UTC"}, DelaySeconds: 60}
	prepared, err := store.PrepareScheduledTask(in, now)
	if err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateScheduledTask(ctx, prepared, nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := store.PrepareScheduledTask(in, now.Add(time.Hour))
	same, err := st.CreateScheduledTask(ctx, replay, nil)
	if err != nil || same.ID != task.ID || !same.NextAt.Equal(*task.NextAt) {
		t.Fatalf("idempotent creation: %+v %v", same, err)
	}
	bad := ""
	if _, err = st.UpdateScheduledTask(ctx, task.ID, store.ScheduledTaskUpdate{Revision: task.Revision, Name: &bad}, now); !errors.Is(err, store.ErrInvalidSchedule) {
		t.Fatal(err)
	}
	current, _ := st.GetScheduledTask(ctx, task.ID)
	if current.Name != task.Name {
		t.Fatal("invalid update partially persisted")
	}
	run, err := st.AcceptScheduledTask(ctx, task.ID, store.ScheduledTaskAccept{Revision: task.Revision, Now: now.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.AcceptScheduledTask(ctx, task.ID, store.ScheduledTaskAccept{Revision: task.Revision, Now: now.Add(time.Minute)}); !errors.Is(err, store.ErrScheduleConflict) {
		t.Fatalf("double accept: %v", err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	pending, err := st.PendingScheduledTaskRuns(ctx)
	if err != nil || len(pending) != 1 || pending[0].ID != run.ID {
		t.Fatalf("recovered pending: %+v %v", pending, err)
	}
	task, _ = st.GetScheduledTask(ctx, task.ID)
	if task.NextAt != nil {
		t.Fatal("once trigger not consumed")
	}
	if err = st.SetScheduledTaskHandoff(ctx, run.ID, "submitted", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = st.UpdateScheduledTask(ctx, task.ID, store.ScheduledTaskUpdate{Revision: task.Revision, Delete: true}, now); err != nil {
		t.Fatal(err)
	}
	visible, _ := st.ListScheduledTasks(ctx, "", false)
	history, _ := st.ListScheduledTaskRuns(ctx, task.ID, 50, 0)
	if len(visible) != 0 || len(history) != 1 || history[0].Handoff != "submitted" {
		t.Fatal("deletion lost history or kept plan visible")
	}
}

func TestScheduledTasksMigrationArchive(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "v22.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, st, "old-session")
	if _, err = st.db.Exec(`DROP TABLE scheduled_task_runs; DROP TABLE scheduled_tasks; PRAGMA user_version=22;`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err = st.GetSession(ctx, "old-session"); err != nil {
		t.Fatal("migration lost session", err)
	}
	task, _ := store.PrepareScheduledTask(store.ScheduledTaskCreate{SessionID: "old-session", RequestID: "once", Name: "test", Prompt: "test", Schedule: store.TaskSchedule{Kind: "daily", Timezone: "UTC", Time: "09:00"}}, time.Now())
	task, err = st.CreateScheduledTask(ctx, task, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.ArchiveSession(ctx, "old-session"); err != nil {
		t.Fatal(err)
	}
	task, err = st.GetScheduledTask(ctx, task.ID)
	if err != nil || task.Enabled {
		t.Fatalf("archive failed to pause %+v %v", task, err)
	}
}

func TestScheduledRunsVersion23Migration(t *testing.T) {
	for _, missingSchedule := range []bool{true, false} {
		t.Run(map[bool]string{true: "early-layout", false: "complete-layout"}[missingSchedule], func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "v23.db")
			st, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			createTestSession(t, st, "target")
			beginTestTurn(t, st, "target", "turn", "message", "input")
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted}); err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC().Truncate(time.Millisecond)
			prepared, err := store.PrepareScheduledTask(store.ScheduledTaskCreate{SessionID: "target", RequestID: "create", Name: "check", Prompt: "check", Schedule: store.TaskSchedule{Kind: "daily", Timezone: "Asia/Shanghai", Time: "09:00"}}, now)
			if err != nil {
				t.Fatal(err)
			}
			task, err := st.CreateScheduledTask(ctx, prepared, nil)
			if err != nil {
				t.Fatal(err)
			}
			run, err := st.AcceptScheduledTask(ctx, task.ID, store.ScheduledTaskAccept{Revision: task.Revision, Now: now, RequestID: "manual"})
			if err != nil {
				t.Fatal(err)
			}
			if !missingSchedule {
				// An existing snapshot must survive even when the plan has changed.
				task, _ = st.GetScheduledTask(ctx, task.ID)
				schedule := store.TaskSchedule{Kind: "daily", Timezone: "UTC", Time: "13:00"}
				if _, err := st.UpdateScheduledTask(ctx, task.ID, store.ScheduledTaskUpdate{Revision: task.Revision, Schedule: &schedule}, now); err != nil {
					t.Fatal(err)
				}
			} else if _, err := st.db.Exec(`ALTER TABLE scheduled_task_runs DROP COLUMN schedule`); err != nil {
				t.Fatal(err)
			}
			if _, err := st.db.Exec(`PRAGMA user_version=23`); err != nil {
				t.Fatal(err)
			}
			st.Close()
			for range 2 {
				st, err = Open(path)
				if err != nil {
					t.Fatal(err)
				}
				pending, err := st.PendingScheduledTaskRuns(ctx)
				if err != nil || len(pending) != 1 || !reflect.DeepEqual(pending[0], run) {
					t.Fatalf("pending run not preserved: %+v, %v; want %+v", pending, err, run)
				}
				if _, err := st.GetMessage(ctx, "target", "message"); err != nil {
					t.Fatal("migration lost conversation", err)
				}
				if err := validateCurrentSchema(st.db); err != nil {
					t.Fatal(err)
				}
				st.Close()
			}
		})
	}
}

func TestScheduledRunsMissingScheduleRejectedAtCurrentVersion(t *testing.T) {
	st, path := openTestStore(t)
	if _, err := st.db.Exec(`ALTER TABLE scheduled_task_runs DROP COLUMN schedule`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	reopened, err := Open(path)
	if reopened != nil {
		reopened.Close()
	}
	if !errors.Is(err, ErrUnsupportedSchema) || !strings.Contains(err.Error(), "scheduled_task_runs is missing column schedule") {
		t.Fatalf("missing schedule must fail startup validation: %v", err)
	}
}

func TestScheduledRunsVersion23MigrationRollback(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "target")
	// A broken parent reference makes snapshot backfill fail. Neither the
	// original rows nor the schema version may be changed by that failure.
	if _, err := st.db.Exec(`PRAGMA foreign_keys=OFF;
ALTER TABLE scheduled_task_runs DROP COLUMN schedule;
INSERT INTO scheduled_task_runs(id,task_id,session_id,name,prompt,definition_revision,source,scheduled_for,accepted_at,client_message_id,handoff,trigger_key)
VALUES('run','missing-task','target','check','check',1,'manual',1,1,'input','pending','manual:run');
PRAGMA user_version=23;`); err != nil {
		t.Fatal(err)
	}
	st.Close()
	reopened, err := Open(path)
	if reopened != nil {
		reopened.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "migrate to v24") {
		t.Fatalf("expected migration failure: %v", err)
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	version, err := schemaVersion(db)
	if err != nil || version != 23 {
		t.Fatalf("failed migration changed version: %d %v", version, err)
	}
	columns, err := tableColumns(db, "scheduled_task_runs")
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := columns["schedule"]; exists {
		t.Fatal("failed migration retained new column")
	}
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM scheduled_task_runs WHERE id='run' AND client_message_id='input'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("failed migration lost run: %d %v", count, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name='scheduled_task_runs_v23'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("failed migration retained temporary table: %d %v", count, err)
	}
}

package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
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
	task, err := st.CreateScheduledTask(ctx, prepared)
	if err != nil {
		t.Fatal(err)
	}
	replay, _ := store.PrepareScheduledTask(in, now.Add(time.Hour))
	same, err := st.CreateScheduledTask(ctx, replay)
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
	task, err = st.CreateScheduledTask(ctx, task)
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

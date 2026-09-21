package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestScheduledTaskNewSessionAtomicReplay(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var st store.Store = memstore.New()
			path := filepath.Join(t.TempDir(), "tasks.db")
			if kind == "sqlite" {
				db, err := sqlitestore.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				st = db
			}
			in := store.ScheduledTaskCreate{RequestID: "new-session", Name: "Daily check", Prompt: "Check", NewSession: &store.ScheduledTaskNewSession{Provider: "mock", Model: "model"}, Schedule: store.TaskSchedule{Kind: "once", Timezone: "UTC"}, DelaySeconds: 60}
			now := time.Now().UTC().Truncate(time.Millisecond)
			create := func(in store.ScheduledTaskCreate, at time.Time) (*store.ScheduledTask, error) {
				task, err := store.PrepareScheduledTask(in, at)
				if err != nil {
					return nil, err
				}
				return st.CreateScheduledTask(ctx, task, &store.Session{ID: task.SessionID, Title: task.Name, Provider: in.NewSession.Provider, Model: in.NewSession.Model})
			}
			// A failed schedule must roll back the session inserted in the same transaction.
			past := now.Add(-time.Minute)
			invalid := in
			invalid.DelaySeconds, invalid.Schedule.At = 0, &past
			if _, err := create(invalid, now); !errors.Is(err, store.ErrInvalidSchedule) {
				t.Fatalf("invalid time: %v", err)
			}
			sessions, _ := st.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListAll})
			if len(sessions) != 0 {
				t.Fatal("failed creation left an empty session")
			}
			task, err := create(in, now)
			if err != nil {
				t.Fatal(err)
			}
			session, err := st.GetSession(ctx, task.SessionID)
			if err != nil || session.Title != in.Name || session.ActiveMode != store.ModeChat || session.ModeLease != store.ModeLeaseNone || session.ProjectID != "" || len(session.LoadedAppIDs) != 0 {
				t.Fatalf("new session defaults: %+v %v", session, err)
			}
			if kind == "sqlite" {
				st.(*sqlitestore.Store).Close()
				db, err := sqlitestore.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				st = db
			}
			// Replay after restart and after the original due time returns the saved identity.
			same, err := create(in, now.Add(time.Hour))
			if err != nil || same.ID != task.ID || same.SessionID != task.SessionID || !same.NextAt.Equal(*task.NextAt) {
				t.Fatalf("replay duplicated or rescheduled task: %+v %v", same, err)
			}
			in.Prompt = "Changed"
			if _, err := create(in, now); !errors.Is(err, store.ErrScheduleConflict) {
				t.Fatalf("changed replay: %v", err)
			}
			sessions, _ = st.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListAll})
			if len(sessions) != 1 {
				t.Fatalf("session count: %d", len(sessions))
			}
		})
	}
}

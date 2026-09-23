package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestSessionArchiveRetryStores(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var st store.Store = memstore.New()
			if kind == "sqlite" {
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "archive.db"))
				if err != nil {
					t.Fatal(err)
				}
				st = db
				t.Cleanup(func() { db.Close() })
			}
			if err := st.CreateSession(ctx, &store.Session{ID: "parent", Provider: "mock", Model: "m"}); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateChildSession(ctx, "parent", &store.Session{ID: "child", Provider: "mock", Model: "m"}); err != nil {
				t.Fatal(err)
			}
			prepared, err := store.PrepareScheduledTask(store.ScheduledTaskCreate{
				SessionID: "parent", RequestID: "scheduled", Name: "daily", Prompt: "check",
				Schedule: store.TaskSchedule{Kind: "daily", Timezone: "UTC", Time: "09:00"},
			}, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			task, err := st.CreateScheduledTask(ctx, prepared, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.ArchiveSession(ctx, "child"); !errors.Is(err, store.ErrInvalidSessionRelation) {
				t.Fatalf("active child archived independently: %v", err)
			}
			first, err := st.ArchiveSession(ctx, "parent")
			if err != nil || first.ArchivedAt == nil {
				t.Fatalf("archive: %+v %v", first, err)
			}
			children, err := st.ListChildSessions(ctx, "parent")
			if err != nil || len(children) != 1 || children[0].ArchivedAt == nil {
				t.Fatalf("archived children: %+v %v", children, err)
			}
			paused, err := st.GetScheduledTask(ctx, task.ID)
			if err != nil || paused.Enabled || paused.Revision != task.Revision+1 {
				t.Fatalf("archive did not pause the task once: %+v %v", paused, err)
			}
			// SQLite stores millisecond timestamps. Advance beyond that precision
			// so an accidental timestamp refresh cannot look unchanged.
			time.Sleep(2 * time.Millisecond)
			for attempt := 1; attempt <= 2; attempt++ {
				retry, err := st.ArchiveSession(ctx, "parent")
				if err != nil {
					t.Fatalf("archive retry %d: %v", attempt, err)
				}
				if !reflect.DeepEqual(retry, first) {
					t.Fatalf("retry changed the archived snapshot: got %+v, want %+v", retry, first)
				}
				retryChildren, err := st.ListChildSessions(ctx, "parent")
				if err != nil || !reflect.DeepEqual(retryChildren, children) {
					t.Fatalf("retry changed archived children: %+v %v", retryChildren, err)
				}
				retryTask, err := st.GetScheduledTask(ctx, task.ID)
				if err != nil || !reflect.DeepEqual(retryTask, paused) {
					t.Fatalf("retry changed the paused task: %+v %v; want %+v", retryTask, err, paused)
				}
			}
			if _, err := st.ArchiveSession(ctx, "child"); !errors.Is(err, store.ErrInvalidSessionRelation) {
				t.Fatalf("archived child accepted independent retry: %v", err)
			}
			if _, err := st.ArchiveSession(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("missing archive: %v", err)
			}
		})
	}
}

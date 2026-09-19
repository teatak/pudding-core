package sqlitestore

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestProjectActivityContract(t *testing.T) {
	for _, backend := range []string{"sqlite", "memory"} {
		t.Run(backend, func(t *testing.T) {
			var st store.Store = memstore.New()
			if backend == "sqlite" {
				st, _ = openTestStore(t)
			}
			ctx := context.Background()
			project := &store.Project{ID: "p", Name: "Project", RootDirs: []string{"/p"}}
			if err := st.CreateProject(ctx, project); err != nil {
				t.Fatal(err)
			}
			activity := func(id string) time.Time {
				t.Helper()
				p, err := st.GetProject(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				return p.LastActivityAt
			}
			assertActivity := func(id string, want time.Time) {
				t.Helper()
				if got := activity(id); got.UnixMilli() != want.UnixMilli() {
					t.Fatalf("%s activity = %v, want %v", id, got, want)
				}
			}
			assertActivity("p", project.CreatedAt)
			name := "Renamed"
			if _, err := st.UpdateProject(ctx, "p", store.ProjectUpdate{Name: &name}); err != nil {
				t.Fatal(err)
			}
			assertActivity("p", project.CreatedAt)
			session := &store.Session{ID: "s", Provider: "mock", Model: "mock", ProjectID: "p"}
			if err := st.CreateSession(ctx, session); err != nil {
				t.Fatal(err)
			}
			assertActivity("p", session.LastActivityAt)
			begin := store.BeginTurnInput{SessionID: "s", TurnID: "t", UserMessageID: "m", ClientMessageID: "cm", UserText: "hello"}
			advance := func(label string, fn func() error) {
				t.Helper()
				before := activity("p")
				time.Sleep(2 * time.Millisecond) // SQLite timestamps have millisecond precision.
				if err := fn(); err != nil {
					t.Fatalf("%s: %v", label, err)
				}
				if !activity("p").After(before) {
					t.Fatalf("%s did not advance project activity", label)
				}
				sess, err := st.GetSession(ctx, "s")
				if err != nil {
					t.Fatal(err)
				}
				assertActivity("p", sess.LastActivityAt)
			}
			advance("submit", func() error { _, err := st.BeginTurn(ctx, begin); return err })
			before := activity("p")
			if result, err := st.BeginTurn(ctx, begin); err != nil || !result.Duplicate {
				t.Fatalf("duplicate submit: %+v %v", result, err)
			}
			assertActivity("p", before)
			advance("queue", func() error {
				_, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "s", ClientMessageID: "q", Text: "next"})
				return err
			})
			advance("steer queued input", func() error {
				_, err := st.SteerQueuedInput(ctx, store.SteerQueuedInputInput{SessionID: "s", TurnID: "t", ClientMessageID: "q", UserMessageID: "qm"})
				return err
			})
			advance("append output", func() error {
				_, err := st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "t", Parts: store.TextPart("reply")})
				return err
			})
			advance("append steer", func() error {
				_, err := st.AppendTurnSteer(ctx, store.AppendTurnSteerInput{SessionID: "s", TurnID: "t", UserMessageID: "steer", ClientMessageID: "steer", UserText: "clarification"})
				return err
			})
			advance("finish", func() error {
				_, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t", Status: store.TurnCompleted})
				return err
			})
			advance("queue next turn", func() error {
				_, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "s", ClientMessageID: "q2", Text: "next turn"})
				return err
			})
			advance("promote", func() error {
				_, err := st.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: "s", TurnID: "t2", UserMessageID: "m2"})
				return err
			})
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t2", Status: store.TurnCancelled}); err != nil {
				t.Fatal(err)
			}
			advance("compact", func() error {
				_, err := st.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{SessionID: "s", TurnID: "compact", MessageID: "summary", ClientMessageID: "compact", ExpectedLastMessageID: "m2", Text: "summary"})
				return err
			})
			advance("system turn", func() error {
				_, err := st.BeginSystemTurn(ctx, store.BeginSystemTurnInput{SessionID: "s", TurnID: "sys", SystemMessageID: "sys", ClientMessageID: "sys", Text: "system"})
				return err
			})
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "sys", Status: store.TurnCompleted}); err != nil {
				t.Fatal(err)
			}
			time.Sleep(2 * time.Millisecond)
			cloned, err := st.CloneSession(ctx, store.CloneSessionInput{SourceSessionID: "s", TargetSessionID: "clone", ThroughMessageID: "m", TitleSuffix: " copy"})
			if err != nil {
				t.Fatal(err)
			}
			assertActivity("p", cloned.LastActivityAt)

			// Moving older content into a newer project must not move its clock backwards.
			time.Sleep(2 * time.Millisecond)
			other := &store.Project{ID: "other", RootDirs: []string{"/other"}}
			if err := st.CreateProject(ctx, other); err != nil {
				t.Fatal(err)
			}
			if _, err := st.UpdateSession(ctx, "s", store.SessionUpdate{ProjectID: &other.ID}); err != nil {
				t.Fatal(err)
			}
			assertActivity("other", other.CreatedAt)
			assertActivity("p", cloned.LastActivityAt)
			if _, err := st.UpdateSession(ctx, "s", store.SessionUpdate{ProjectID: &project.ID}); err != nil {
				t.Fatal(err)
			}
			assertActivity("p", cloned.LastActivityAt)
			before = activity("p")
			effort, model, pinned := "high", "new-model", true
			if _, err := st.UpdateSession(ctx, "clone", store.SessionUpdate{Title: &name, Model: &model, ReasoningEffort: &effort, Pinned: &pinned}); err != nil {
				t.Fatal(err)
			}
			assertActivity("p", before)
			if _, err := st.ArchiveSession(ctx, "clone"); err != nil {
				t.Fatal(err)
			}
			assertActivity("p", before)
			if _, err := st.RestoreSession(ctx, "clone"); err != nil {
				t.Fatal(err)
			}
			assertActivity("p", before)
			for _, id := range []string{"clone", "s"} {
				if err := st.DeleteSession(ctx, id); err != nil {
					t.Fatal(err)
				}
				assertActivity("p", before)
			}
			projects, err := st.ListProjects(ctx)
			if err != nil || len(projects) != 2 || projects[0].ID != "other" || projects[1].LastActivityAt.UnixMilli() != before.UnixMilli() {
				t.Fatalf("project order after deletion: %+v %v", projects, err)
			}
			// Merge retains activity even when the source no longer has sessions.
			merged, err := st.MergeProjects(ctx, "p", "other", store.ProjectUpdate{Name: &name, RootDirs: &other.RootDirs})
			if err != nil || merged.LastActivityAt.UnixMilli() != other.CreatedAt.UnixMilli() {
				t.Fatalf("merge lost activity: %+v %v", merged, err)
			}
			assertActivity("p", other.CreatedAt)
		})
	}
}

func TestProjectActivityTransactionRollback(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	project := &store.Project{ID: "p", Name: "Project"}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	sess := &store.Session{ID: "s", Provider: "mock", Model: "mock", ProjectID: "p"}
	if err := st.CreateSession(ctx, sess); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`CREATE TRIGGER reject_activity BEFORE UPDATE OF last_activity_at ON projects BEGIN SELECT RAISE(ABORT,'test failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: "t", UserMessageID: "m", ClientMessageID: "cm", UserText: "hello"}); err == nil {
		t.Fatal("expected activity write failure")
	}
	for _, table := range []string{"turns", "messages", "events"} {
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("partial %s write committed: %d %v", table, count, err)
		}
	}
	got, err := st.GetSession(ctx, "s")
	if err != nil || got.LastActivityAt.UnixMilli() != sess.LastActivityAt.UnixMilli() {
		t.Fatalf("session activity not rolled back: %+v %v", got, err)
	}
	p, err := st.GetProject(ctx, "p")
	if err != nil || p.LastActivityAt.UnixMilli() != sess.LastActivityAt.UnixMilli() {
		t.Fatalf("project activity not rolled back: %+v %v", p, err)
	}
}

func TestProjectActivityMigrationFromV18(t *testing.T) {
	st, path := openTestStore(t)
	if _, err := st.db.Exec(`ALTER TABLE projects DROP COLUMN last_activity_at;
INSERT INTO projects(id,name,created_at,updated_at) VALUES('p','Keep project',10,999),('empty','Empty',20,999),('new','New',200,999);
INSERT INTO sessions(id,provider,model,project_id,created_at,updated_at,last_activity_at,archived_at)
VALUES('active','mock','mock','p',10,999,100,0),('archive','mock','mock','p',10,999,150,998),('old','mock','mock','new',10,999,50,0);
PRAGMA user_version=18;
CREATE TRIGGER reject_activity_migration BEFORE UPDATE ON projects BEGIN SELECT RAISE(ABORT,'test migration failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	if failed, err := Open(path); err == nil {
		failed.Close()
		t.Fatal("expected migration failure")
	}
	db := openMigrationTestDB(t, path)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "18")
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM pragma_table_info('projects') WHERE name='last_activity_at'", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT name FROM projects WHERE id='p'", "Keep project")
	if _, err := db.Exec(`DROP TRIGGER reject_activity_migration`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	for range 2 {
		reopened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		projects, err := reopened.ListProjects(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, p := range projects {
			ids = append(ids, p.ID)
			want := map[string]int64{"p": 150, "empty": 20, "new": 200}[p.ID]
			if p.LastActivityAt.UnixMilli() != want || p.UpdatedAt.UnixMilli() != 999 {
				t.Fatalf("migrated project: %+v", p)
			}
		}
		if !reflect.DeepEqual(ids, []string{"new", "p", "empty"}) {
			t.Fatalf("migrated project order: %v", ids)
		}
		// Deleting all sessions and reopening must not recalculate the stored clock.
		if _, err := reopened.db.Exec(`DELETE FROM sessions`); err != nil {
			t.Fatal(err)
		}
		reopened.Close()
	}
}

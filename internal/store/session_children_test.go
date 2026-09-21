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

func TestChildSessionStores(t *testing.T) {
	for _, kind := range []string{"memory", "sqlite"} {
		t.Run(kind, func(t *testing.T) {
			ctx := context.Background()
			var st store.Store = memstore.New()
			if kind == "sqlite" {
				db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "children.db"))
				if err != nil {
					t.Fatal(err)
				}
				st = db
				t.Cleanup(func() { db.Close() })
			}
			newSession := func(id string) *store.Session {
				return &store.Session{ID: id, Title: id, Provider: "mock", Model: "model"}
			}
			if err := st.CreateProject(ctx, &store.Project{ID: "project", Name: "Project", RootDirs: []string{t.TempDir()}}); err != nil {
				t.Fatal(err)
			}
			parent := newSession("parent")
			parent.ProjectID = "project"
			for _, session := range []*store.Session{parent, newSession("other")} {
				if err := st.CreateSession(ctx, session); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.GrantComputerApp(ctx, "parent", "com.example.Editor"); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateChildSession(ctx, "parent", newSession("child")); err != nil {
				t.Fatal(err)
			}
			child, err := st.GetSession(ctx, "child")
			if err != nil || child.ProjectID != "project" {
				t.Fatalf("child project: %+v %v", child, err)
			}
			if granted, err := st.HasComputerAppGrant(ctx, "child", "com.example.Editor"); err != nil || granted {
				t.Fatalf("parent permission copied to child: %v %v", granted, err)
			}
			for id, want := range map[string]string{"parent": "", "child": "parent", "other": ""} {
				if got, err := st.ParentSessionID(ctx, id); err != nil || got != want {
					t.Fatalf("parent of %s: %q %v", id, got, err)
				}
			}
			if _, err := st.ParentSessionID(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("missing child: %v", err)
			}
			for _, tc := range []struct {
				name, parentID string
				child          *store.Session
			}{
				{"missing parent", "missing", newSession("rejected")},
				{"recursive delegation", "child", newSession("rejected")},
				{"self", "parent", newSession("parent")},
				{"nil child", "parent", nil},
				{"empty ID", "parent", newSession("")},
				{"duplicate ID", "other", newSession("child")},
				{"existing root", "parent", newSession("other")},
				{"cross project", "parent", &store.Session{ID: "rejected", Provider: "mock", Model: "model", ProjectID: "other-project"}},
				{"pinned child", "parent", &store.Session{ID: "rejected", Provider: "mock", Model: "model", Pinned: true}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if err := st.CreateChildSession(ctx, tc.parentID, tc.child); err == nil {
						t.Fatal("accepted invalid child creation")
					}
				})
			}
			if parentID, _ := st.ParentSessionID(ctx, "child"); parentID != "parent" {
				t.Fatal("rejected creation changed ownership")
			}
			if _, err := st.GetSession(ctx, "rejected"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("rejected creation left orphan: %v", err)
			}
			if children, err := st.ListChildSessions(ctx, "other"); err != nil || len(children) != 0 {
				t.Fatalf("cross-parent children: %+v %v", children, err)
			}
			if _, err := st.ListChildSessions(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("missing parent lookup: %v", err)
			}
			if list, err := st.ListSessions(ctx); err != nil || len(list) != 2 {
				t.Fatalf("sidebar roots: %+v %v", list, err)
			}
			if list, err := st.ListSessions(ctx, store.SessionListOptions{Query: "child"}); err != nil || len(list) != 0 {
				t.Fatalf("sidebar search leaked child: %+v %v", list, err)
			}
			if list, err := st.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListAll}); err != nil || len(list) != 3 {
				t.Fatalf("maintenance omitted children: %+v %v", list, err)
			}
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "child", TurnID: "child-turn", ClientMessageID: "input", UserMessageID: "message", UserText: "child task"}); err != nil {
				t.Fatal(err)
			}
			children, err := st.ListChildSessions(ctx, "parent")
			if err != nil || len(children) != 1 || !children[0].Running {
				t.Fatalf("running derived from turn: %+v %v", children, err)
			}
			children[0].Title = "local mutation"
			if child, _ := st.GetSession(ctx, "child"); child.Title != "child" {
				t.Fatal("query result modified stored session")
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "child-turn", Status: store.TurnCompleted}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "child", ClientMessageID: "queued", Text: "follow-up"}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.ArchiveSession(ctx, "child"); !errors.Is(err, store.ErrInvalidSessionRelation) {
				t.Fatalf("child archived independently: %v", err)
			}
			if _, err := st.ArchiveSession(ctx, "parent"); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateChildSession(ctx, "parent", newSession("late")); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("archived parent accepted child: %v", err)
			}
			children, err = st.ListChildSessions(ctx, "parent")
			if err != nil || len(children) != 1 || children[0].ArchivedAt == nil || children[0].Running {
				t.Fatalf("archived group: %+v %v", children, err)
			}
			if list, err := st.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListArchived}); err != nil || len(list) != 1 || list[0].ID != "parent" {
				t.Fatalf("archived list leaked child: %+v %v", list, err)
			}
			if ids, err := st.ListExpiredArchivedSessionIDs(ctx, time.Now().Add(time.Hour)); err != nil || len(ids) != 1 || ids[0] != "parent" {
				t.Fatalf("cleanup must select group root: %+v %v", ids, err)
			}
			if _, err := st.RestoreSession(ctx, "child"); !errors.Is(err, store.ErrInvalidSessionRelation) {
				t.Fatalf("child restored independently: %v", err)
			}
			if _, err := st.RestoreSession(ctx, "parent"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.GetSession(ctx, "child"); err != nil {
				t.Fatalf("child not restored: %v", err)
			}
			if queued, err := st.HasQueuedInputs(ctx, "child"); err != nil || queued {
				t.Fatalf("archived queue restarted: %v %v", queued, err)
			}
			if err := st.CreateChildSession(ctx, "parent", newSession("sibling")); err != nil {
				t.Fatal(err)
			}
			if err := st.DeleteSession(ctx, "sibling"); err != nil {
				t.Fatal(err)
			}
			if _, err := st.GetSession(ctx, "child"); err != nil {
				t.Fatal("deleting sibling affected child", err)
			}
			if err := st.DeleteSession(ctx, "parent"); err != nil {
				t.Fatal(err)
			}
			for _, id := range []string{"parent", "child", "sibling"} {
				if _, err := st.ParentSessionID(ctx, id); !errors.Is(err, store.ErrNotFound) {
					t.Fatalf("deleted group retained %s: %v", id, err)
				}
			}
			if _, err := st.GetSession(ctx, "other"); err != nil {
				t.Fatal("deleting group affected unrelated session", err)
			}
			if list, err := st.ListSessions(ctx, store.SessionListOptions{Scope: store.SessionListAll}); err != nil || len(list) != 1 {
				t.Fatalf("deleted group retained sessions: %+v %v", list, err)
			}
		})
	}
}

package sqlitestore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

func createTestSession(t *testing.T, st store.Store, id string) {
	t.Helper()
	if err := st.CreateSession(context.Background(), &store.Session{ID: id, Title: id, Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
}

func TestComputerAppGrantPersistsAndCascadesWithSession(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_computer_grant")
	if err := st.GrantComputerApp(ctx, "sess_computer_grant", "com.example.Calculator"); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if granted, err := reopened.HasComputerAppGrant(ctx, "sess_computer_grant", "com.example.Calculator"); err != nil || !granted {
		t.Fatalf("persisted Computer Use grant: granted=%v err=%v", granted, err)
	}
	if err := reopened.DeleteSession(ctx, "sess_computer_grant"); err != nil {
		t.Fatal(err)
	}
	if granted, err := reopened.HasComputerAppGrant(ctx, "sess_computer_grant", "com.example.Calculator"); err != nil || granted {
		t.Fatalf("deleted session grant: granted=%v err=%v", granted, err)
	}
}

func TestSessionProjectPersists(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "project")
	if err := st.CreateProject(ctx, &store.Project{
		ID:           "proj_project",
		Name:         "project",
		RootDirs:     []string{root, root + "/.", "relative"},
		ApprovalMode: store.ApprovalAuto,
	}); err != nil {
		t.Fatal(err)
	}
	project, err := st.GetProject(ctx, "proj_project")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(project.RootDirs, []string{root}) {
		t.Fatalf("project roots not normalized: %+v", project.RootDirs)
	}
	if err := st.CreateSession(ctx, &store.Session{
		ID:        "sess_project",
		Title:     "project",
		Provider:  "mock",
		Model:     "mock",
		ProjectID: project.ID,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "sess_project")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != project.ID {
		t.Fatalf("session project not stored: %+v", got.ProjectID)
	}

	other := filepath.Join(t.TempDir(), "other")
	dirs := []string{other, other}
	if _, err := st.UpdateProject(ctx, project.ID, store.ProjectUpdate{RootDirs: &dirs}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.GetSession(ctx, "sess_project")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProjectID != project.ID {
		t.Fatalf("session project not persisted: %+v", got.ProjectID)
	}
	project, err = reopened.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(project.RootDirs, []string{other}) {
		t.Fatalf("project roots not persisted: %+v", project.RootDirs)
	}
}

func TestProjectWithoutDirectoriesPersists(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	project := &store.Project{
		ID:           "proj_empty",
		Name:         "Empty project",
		ApprovalMode: store.ApprovalAuto,
	}
	if err := st.CreateProject(ctx, project); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetProject(ctx, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != project.Name || len(got.RootDirs) != 0 {
		t.Fatalf("empty project not persisted: %+v", got)
	}
}

func TestProjectsUseLatestSessionActivity(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	older := &store.Project{ID: "proj_older", Name: "Older"}
	if err := st.CreateProject(ctx, older); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	newer := &store.Project{ID: "proj_newer", Name: "Newer"}
	if err := st.CreateProject(ctx, newer); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	session := &store.Session{
		ID:        "sess_activity",
		Provider:  "mock",
		Model:     "mock",
		ProjectID: older.ID,
	}
	if err := st.CreateSession(ctx, session); err != nil {
		t.Fatal(err)
	}

	projects, err := st.ListProjects(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(projects) != 2 || projects[0].ID != older.ID {
		t.Fatalf("projects not sorted by session activity: %+v", projects)
	}
	if projects[0].LastActivityAt == nil ||
		projects[0].LastActivityAt.UnixMilli() != session.LastActivityAt.UnixMilli() {
		t.Fatalf("last activity = %v, want %v", projects[0].LastActivityAt, session.LastActivityAt)
	}
	if projects[1].LastActivityAt != nil {
		t.Fatalf("project without sessions has activity: %v", projects[1].LastActivityAt)
	}
}

func TestSessionReasoningEffortPersistsAndClearsOnModelChange(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_reasoning")

	effort := "high"
	updated, err := st.UpdateSession(ctx, "sess_reasoning", store.SessionUpdate{ReasoningEffort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ReasoningEffort != "high" || updated.ReasoningModelKey != "mock:mock" {
		t.Fatalf("reasoning effort not bound to current model: %+v", updated)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSession(ctx, "sess_reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "high" || got.ReasoningModelKey != "mock:mock" {
		t.Fatalf("reasoning effort not persisted: %+v", got)
	}

	model := "next"
	got, err = reopened.UpdateSession(ctx, "sess_reasoning", store.SessionUpdate{Model: &model})
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "" || got.ReasoningModelKey != "" {
		t.Fatalf("reasoning effort must clear on model change: %+v", got)
	}
}

func TestSessionLoadedAppIDsPersist(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{
		ID: "sess_apps", Provider: "mock", Model: "mock",
		LoadedAppIDs: []string{"terminal", "browser", "browser"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "sess_apps")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got.LoadedAppIDs, []string{"browser", "terminal"}) {
		t.Fatalf("created loaded app ids = %+v", got.LoadedAppIDs)
	}
	loaded := []string{"browser"}
	if _, err := st.UpdateSession(ctx, "sess_apps", store.SessionUpdate{LoadedAppIDs: &loaded}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.GetSession(ctx, "sess_apps")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got.LoadedAppIDs, []string{"browser"}) {
		t.Fatalf("persisted loaded app ids = %+v", got.LoadedAppIDs)
	}
}

func TestCanvasItemsAreSessionScoped(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_left")
	createTestSession(t, st, "sess_right")

	item, err := st.PutCanvasItem(ctx, store.CanvasItemInput{
		ID:             "canvas_1",
		ActorSessionID: "sess_left",
		Kind:           "markdown",
		Title:          "Note",
		Item:           []byte(`{"kind":"markdown","content":"hello"}`),
		Window:         []byte(`{"x":1,"y":2,"w":300,"h":200,"z":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceSessionID != "sess_left" || item.CreatedBySessionID != "sess_left" || item.UpdatedBySessionID != "sess_left" {
		t.Fatalf("unexpected actor fields: %+v", item)
	}

	visible, err := st.ListCanvasItems(ctx, "sess_right")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("right session should not see left canvas item: %+v", visible)
	}

	_, err = st.UpdateCanvasItemWindow(ctx, store.CanvasItemWindowPatch{
		ActorSessionID: "sess_right",
		ItemID:         "canvas_1",
		Window:         []byte(`{"x":9,"y":8,"w":320,"h":240,"z":2}`),
	})
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("right session update error = %v, want not found", err)
	}

	if err := st.DeleteSession(ctx, "sess_left"); err != nil {
		t.Fatal(err)
	}
	visible, err = st.ListCanvasItems(ctx, "sess_right")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("deleted session canvas should not survive: %+v", visible)
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	visible, err = reopened.ListCanvasItems(ctx, "sess_right")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("deleted session canvas item persisted: %+v", visible)
	}
}

func TestCanvasItemListOrderIgnoresWindowUpdates(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_canvas_order")

	for _, id := range []string{"canvas_a", "canvas_b"} {
		if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{
			ID:             id,
			ActorSessionID: "sess_canvas_order",
			Kind:           "markdown",
			Title:          id,
			Item:           []byte(`{"kind":"markdown","content":"hello"}`),
			Window:         []byte(`{"x":1,"y":2,"w":300,"h":200,"z":1}`),
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}

	visible, err := st.ListCanvasItems(ctx, "sess_canvas_order")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 2 || visible[0].ID != "canvas_a" || visible[1].ID != "canvas_b" {
		t.Fatalf("canvas items should use stable created order: %+v", visible)
	}

	if _, err := st.UpdateCanvasItemWindow(ctx, store.CanvasItemWindowPatch{
		ActorSessionID: "sess_canvas_order",
		ItemID:         "canvas_b",
		Window:         []byte(`{"x":9,"y":8,"w":320,"h":240,"z":99}`),
	}); err != nil {
		t.Fatal(err)
	}
	visible, err = st.ListCanvasItems(ctx, "sess_canvas_order")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 2 || visible[0].ID != "canvas_a" || visible[1].ID != "canvas_b" {
		t.Fatalf("window updates should not reorder canvas items: %+v", visible)
	}
}

func TestBrowserStatePersistsAndClears(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_browser")

	state, err := st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID:  "sess_browser",
		TabID:      "tab_1",
		URL:        "https://www.sohu.com/",
		Title:      "搜狐",
		FaviconURL: "https://www.sohu.com/favicon.ico",
		Mode:       "headless",
	})
	if err != nil {
		t.Fatal(err)
	}
	if state.SessionID != "sess_browser" || state.URL != "https://www.sohu.com/" || state.Title != "搜狐" {
		t.Fatalf("unexpected browser state: %+v", state)
	}
	state, err = st.PutBrowserState(ctx, store.BrowserStateInput{
		SessionID: "sess_browser",
		TabID:     "tab_blank",
		URL:       "about:blank",
		Mode:      "headless",
	})
	if err != nil {
		t.Fatalf("about:blank should persist as blank browser tab: %v", err)
	}
	if state.TabID != "tab_blank" || state.URL != "about:blank" || state.Title != "" {
		t.Fatalf("unexpected blank browser state: %+v", state)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	state, err = reopened.GetBrowserState(ctx, "sess_browser")
	if err != nil {
		t.Fatal(err)
	}
	if state.TabID != "tab_blank" || state.URL != "about:blank" || state.Title != "" || state.Mode != "headless" {
		t.Fatalf("browser state not persisted: %+v", state)
	}
	states, err := reopened.ListBrowserStates(ctx, "sess_browser")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 2 {
		t.Fatalf("expected two persisted browser tabs: %+v", states)
	}
	first, err := reopened.GetBrowserTabState(ctx, "sess_browser", "tab_1")
	if err != nil || first.URL != "https://www.sohu.com/" {
		t.Fatalf("first browser tab not persisted: state=%+v err=%v", first, err)
	}
	if err := reopened.DeleteBrowserState(ctx, "sess_browser", "tab_blank"); err != nil {
		t.Fatal(err)
	}
	state, err = reopened.GetBrowserState(ctx, "sess_browser")
	if err != nil || state.TabID != "tab_1" {
		t.Fatalf("deleting one tab should preserve the other: state=%+v err=%v", state, err)
	}
	if err := reopened.ClearBrowserState(ctx, "sess_browser"); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.GetBrowserState(ctx, "sess_browser"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cleared browser state should be missing: %v", err)
	}
}

func TestBrowserHistoryIsGlobalSearchableAndDeletable(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Add(-time.Hour)
	first, err := st.PutBrowserHistory(ctx, store.BrowserHistoryInput{
		URL:       "https://example.com/older",
		Title:     "Example older",
		VisitedAt: base,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := st.PutBrowserHistory(ctx, store.BrowserHistoryInput{
		URL:        "https://pudding.local/docs",
		Title:      "Pudding docs",
		FaviconURL: "https://pudding.local/favicon.ico",
		VisitedAt:  base.Add(time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := st.PutBrowserHistory(ctx, store.BrowserHistoryInput{
		URL:       first.URL,
		Title:     "Updated example",
		VisitedAt: base.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != first.ID || updated.Title != "Updated example" {
		t.Fatalf("same URL should update one history entry: %+v", updated)
	}
	entries, err := st.ListBrowserHistory(ctx, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != first.ID || entries[1].ID != second.ID {
		t.Fatalf("history should be globally ordered by latest visit: %+v", entries)
	}
	if err := st.UpdateBrowserHistoryMetadata(ctx, store.BrowserHistoryInput{
		URL:        second.URL,
		Title:      "Pudding documentation",
		FaviconURL: second.FaviconURL,
	}); err != nil {
		t.Fatal(err)
	}
	entries, err = st.ListBrowserHistory(ctx, "", 20)
	if err != nil || len(entries) != 2 || entries[0].ID != first.ID || entries[1].VisitedAt != second.VisitedAt {
		t.Fatalf("metadata updates must not change visit ordering: entries=%+v err=%v", entries, err)
	}
	filtered, err := st.ListBrowserHistory(ctx, "PUDDING", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered) != 1 || filtered[0].ID != second.ID {
		t.Fatalf("history search should match title or URL case-insensitively: %+v", filtered)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err = reopened.ListBrowserHistory(ctx, "", 20)
	if err != nil || len(entries) != 2 {
		t.Fatalf("history should persist: entries=%+v err=%v", entries, err)
	}
	if err := reopened.DeleteBrowserHistory(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if err := reopened.DeleteBrowserHistory(ctx, second.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleting missing history should return not found: %v", err)
	}
	if err := reopened.UpdateBrowserHistoryMetadata(ctx, store.BrowserHistoryInput{URL: second.URL, Title: "late title"}); err != nil {
		t.Fatal(err)
	}
	entries, err = reopened.ListBrowserHistory(ctx, "", 20)
	if err != nil || len(entries) != 1 || entries[0].ID != first.ID {
		t.Fatalf("metadata updates must not recreate deleted history: entries=%+v err=%v", entries, err)
	}
	if err := reopened.ClearBrowserHistory(ctx); err != nil {
		t.Fatal(err)
	}
	entries, err = reopened.ListBrowserHistory(ctx, "", 20)
	if err != nil || len(entries) != 0 {
		t.Fatalf("cleared history should be empty: entries=%+v err=%v", entries, err)
	}
}

func TestRenameDoesNotAffectRecentOrdering(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "older")
	time.Sleep(2 * time.Millisecond)
	createTestSession(t, st, "newer")
	time.Sleep(2 * time.Millisecond)

	title := "renamed older"
	if _, err := st.UpdateSession(context.Background(), "older", store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	pinned := true
	pinnedOrder := int64(7)
	updated, err := st.UpdateSession(context.Background(), "older", store.SessionUpdate{
		Pinned:      &pinned,
		PinnedOrder: &pinnedOrder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Pinned {
		t.Fatal("pinned flag was not persisted")
	}
	if updated.PinnedOrder != pinnedOrder {
		t.Fatalf("pinned order was not persisted: %d", updated.PinnedOrder)
	}
	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "newer" || sessions[1].ID != "older" {
		t.Fatalf("rename must not affect recent ordering: %+v", sessions)
	}
}

func TestArchiveSessionLifecycle(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateProject(ctx, &store.Project{
		ID:           "proj_archive",
		Name:         "Archive Project",
		RootDirs:     []string{t.TempDir()},
		ApprovalMode: store.ApprovalAuto,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(ctx, &store.Session{
		ID:        "sess_archive",
		Title:     "Archived conversation",
		Provider:  "mock",
		Model:     "mock",
		ProjectID: "proj_archive",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{
		SessionID:       "sess_archive",
		ClientMessageID: "queued_archive",
		Text:            "queued",
		Provider:        "mock",
		Model:           "mock",
	}); err != nil {
		t.Fatal(err)
	}

	archived, err := st.ArchiveSession(ctx, "sess_archive")
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archivedAt was not recorded")
	}
	if _, err := st.GetSession(ctx, "sess_archive"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("archived session remained active: %v", err)
	}
	active, err := st.ListSessions(ctx)
	if err != nil || len(active) != 0 {
		t.Fatalf("active sessions = %+v, err=%v", active, err)
	}
	archivedList, err := st.ListSessions(ctx, store.SessionListOptions{
		Scope: store.SessionListArchived,
		Query: "archive project",
	})
	if err != nil || len(archivedList) != 1 || archivedList[0].ID != "sess_archive" {
		t.Fatalf("archived sessions = %+v, err=%v", archivedList, err)
	}
	queued, err := st.QueuedSessions(ctx)
	if err != nil || len(queued) != 0 {
		t.Fatalf("archived queued sessions = %+v, err=%v", queued, err)
	}
	projects, err := st.ListProjects(ctx)
	if err != nil || len(projects) != 1 || projects[0].LastActivityAt != nil {
		t.Fatalf("archived activity leaked into project: projects=%+v err=%v", projects, err)
	}
	expired, err := st.ListExpiredArchivedSessionIDs(ctx, time.Now().Add(time.Second))
	if err != nil || len(expired) != 1 || expired[0] != "sess_archive" {
		t.Fatalf("expired archives = %+v, err=%v", expired, err)
	}

	restored, err := st.RestoreSession(ctx, "sess_archive")
	if err != nil {
		t.Fatal(err)
	}
	if restored.ArchivedAt != nil {
		t.Fatalf("restored archivedAt = %v", restored.ArchivedAt)
	}
	if _, err := st.GetSession(ctx, "sess_archive"); err != nil {
		t.Fatalf("restored session is unavailable: %v", err)
	}
}

func beginTestTurn(t *testing.T, st store.Store, sessionID, turnID, msgID, clientID string) *store.BeginTurnResult {
	t.Helper()
	res, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		UserMessageID:   msgID,
		ClientMessageID: clientID,
		UserText:        "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestGetMessageRequiresSessionScope(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_1")
	createTestSession(t, st, "sess_2")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")

	msg, err := st.GetMessage(ctx, "sess_1", "msg_1")
	if err != nil {
		t.Fatal(err)
	}
	if msg.ID != "msg_1" || msg.SessionID != "sess_1" || msg.Text != "hello" {
		t.Fatalf("unexpected message: %+v", msg)
	}
	if _, err := st.GetMessage(ctx, "sess_2", "msg_1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong session must not read message: %v", err)
	}
}

func TestSearchMessagesLiteralSupportsShortCJKTerms(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_search_literal")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_search_literal",
		TurnID:          "turn_search_literal",
		UserMessageID:   "msg_search_literal",
		ClientMessageID: "client_search_literal",
		UserText:        "法国队与西班牙队将在今晚进行比赛。",
	}); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"法国", "西班牙", "法国 比赛"} {
		hits, err := st.SearchMessages(ctx, store.MessageSearchInput{
			SessionID: "sess_search_literal",
			Query:     query,
			Limit:     10,
			Literal:   true,
		})
		if err != nil {
			t.Fatalf("literal search %q: %v", query, err)
		}
		if len(hits) != 1 || hits[0].ID != "msg_search_literal" {
			t.Fatalf("literal search %q returned %+v", query, hits)
		}
	}
}

func TestSearchMessagesExactUsesCompleteQuery(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_search_exact")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_search_exact",
		TurnID:          "turn_search_exact_1",
		UserMessageID:   "msg_search_exact_1",
		ClientMessageID: "client_search_exact_1",
		UserText:        "alpha beta",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "turn_search_exact_1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("done"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_search_exact",
		TurnID:          "turn_search_exact_2",
		UserMessageID:   "msg_search_exact_2",
		ClientMessageID: "client_search_exact_2",
		UserText:        "alpha middle beta",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID: "sess_search_exact",
		Query:     "ALPHA BETA",
		Limit:     10,
		Literal:   true,
		Exact:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "msg_search_exact_1" {
		t.Fatalf("exact search returned %+v", hits)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "turn_search_exact_2",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{{
			Type: store.ContentPartThought,
			Text: "alpha beta hidden thought",
		}},
	}); err != nil {
		t.Fatal(err)
	}
	hits, err = st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID:             "sess_search_exact",
		Query:                 "ALPHA BETA",
		Limit:                 10,
		Literal:               true,
		Exact:                 true,
		VisibleTranscriptOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "msg_search_exact_1" {
		t.Fatalf("visible transcript exact search returned %+v", hits)
	}
}

func TestSearchMessagesExactNoLimitReturnsLongConversation(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	const sessionID = "sess_search_exact_unlimited"
	const matchCount = 125
	createTestSession(t, st, sessionID)
	for i := 1; i <= matchCount; i++ {
		turnID := fmt.Sprintf("turn_search_exact_unlimited_%03d", i)
		if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
			SessionID:       sessionID,
			TurnID:          turnID,
			UserMessageID:   fmt.Sprintf("msg_search_exact_unlimited_%03d", i),
			ClientMessageID: fmt.Sprintf("client_search_exact_unlimited_%03d", i),
			UserText:        fmt.Sprintf("needle %03d", i),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := st.FinishTurn(ctx, store.FinishTurnInput{
			TurnID:         turnID,
			Status:         store.TurnCompleted,
			AssistantParts: store.TextPart("done"),
		}); err != nil {
			t.Fatal(err)
		}
	}

	hits, err := st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID:             sessionID,
		Query:                 "needle",
		Literal:               true,
		Exact:                 true,
		VisibleTranscriptOnly: true,
		NoLimit:               true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != matchCount {
		t.Fatalf("search returned %d matches, want %d", len(hits), matchCount)
	}
}

func TestSearchMessagesLiteralSupportsUnspacedMixedTerms(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_search_mixed")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_search_mixed",
		TurnID:          "turn_search_mixed",
		UserMessageID:   "msg_search_mixed",
		ClientMessageID: "client_search_mixed",
		UserText:        "DeepSeek模型的GPT4配置已经完成。",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID: "sess_search_mixed",
		Query:     "DeepSeek模型GPT4",
		Limit:     10,
		Literal:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].ID != "msg_search_mixed" {
		t.Fatalf("mixed-script search returned %+v", hits)
	}
}

func TestOpenLeavesExistingMessagesUnindexed(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_search_forward_only")
	if _, err := st.db.Exec(`
		INSERT INTO messages(id,session_id,role,text,created_at)
		VALUES('msg_search_existing','sess_search_forward_only','user','DeepSeek模型配置',1)`); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var tokens string
	if err := reopened.db.QueryRow(
		`SELECT search_tokens FROM messages WHERE id='msg_search_existing'`,
	).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if tokens != "" {
		t.Fatalf("existing message should remain unindexed: %q", tokens)
	}

	if _, err := reopened.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_search_forward_only",
		TurnID:          "turn_search_forward_only",
		UserMessageID:   "msg_search_new",
		ClientMessageID: "client_search_forward_only",
		UserText:        "OpenAI模型配置",
	}); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow(
		`SELECT search_tokens FROM messages WHERE id='msg_search_new'`,
	).Scan(&tokens); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tokens, "openai") || !strings.Contains(tokens, "模型") {
		t.Fatalf("new message should be indexed: %q", tokens)
	}
}

func appendCompletedTestTurn(t *testing.T, st store.Store, sessionID string, index int) {
	t.Helper()
	suffix := strconv.Itoa(index)
	turnID := "turn_" + suffix
	beginTestTurn(t, st, sessionID, turnID, "msg_"+suffix, "client_"+suffix)
	text := "assistant " + suffix
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         turnID,
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBeginTurnIdempotencyPrecedesRunningConflict(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")

	first := beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if first.Duplicate || first.StartedEvent == nil || first.StartedEvent.Seq != 1 {
		t.Fatalf("unexpected first begin: %+v", first)
	}

	duplicate, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_other",
		UserMessageID:   "msg_other",
		ClientMessageID: "client_1",
		UserText:        "retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Turn.ID != "turn_1" || duplicate.UserMessage.ID != "msg_1" || duplicate.StartedEvent != nil {
		t.Fatalf("duplicate should return original turn/message without event: %+v", duplicate)
	}

	_, err = st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_2",
		UserMessageID:   "msg_2",
		ClientMessageID: "client_2",
		UserText:        "conflict",
	})
	if !errors.Is(err, store.ErrTurnRunning) {
		t.Fatalf("want ErrTurnRunning, got %v", err)
	}
}

func TestQueuedInputPersistsParts(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_attach")
	attachment := store.Attachment{
		ID:            "att_1",
		Name:          "note.txt",
		AttachmentKey: "sessions/sess_attach/blobs/note.txt",
		URL:           "/sessions/sess_attach/attachments/blobs/note.txt",
		MIME:          "text/plain",
		Size:          12,
		Origin:        "upload",
	}
	folder := store.LocalFolder{
		ID:     "folder_1",
		Name:   "files",
		Path:   "/Users/me/files",
		Origin: "local_path",
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{
		SessionID:       "sess_attach",
		ClientMessageID: "client_attach",
		Text:            "look",
		Parts:           []store.ContentPart{store.LocalFolderPart(folder), store.AttachmentPart(attachment), store.ContentPart{Type: store.ContentPartText, Text: "look"}},
		Provider:        "mock",
		Model:           "mock",
		Mode:            store.ModeChat,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	queued, err := reopened.ListQueuedInputs(ctx, "sess_attach")
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 {
		t.Fatalf("queued attachment/folder was not persisted: %+v", queued)
	}
	queuedAttachments := store.AttachmentsFromParts(queued[0].Parts)
	queuedFolders := store.LocalFoldersFromParts(queued[0].Parts)
	if len(queuedAttachments) != 1 || queuedAttachments[0].AttachmentKey != attachment.AttachmentKey || len(queuedFolders) != 1 || queuedFolders[0].Path != folder.Path {
		t.Fatalf("queued attachment/folder was not persisted: %+v", queued)
	}
	if len(queued[0].Parts) != 3 || queued[0].Parts[0].Type != store.ContentPartLocalFolder || queued[0].Parts[1].Type != store.ContentPartAttachment || queued[0].Parts[2].Type != store.ContentPartText {
		t.Fatalf("queued parts order was not persisted: %+v", queued[0].Parts)
	}
	promoted, err := reopened.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{
		SessionID:     "sess_attach",
		TurnID:        "turn_attach",
		UserMessageID: "msg_attach",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := store.AttachmentsFromParts(promoted.UserMessage.Parts)
	if len(got) != 1 || got[0].AttachmentKey != attachment.AttachmentKey {
		t.Fatalf("promoted message lost attachment: %+v", promoted.UserMessage.Parts)
	}
	gotFolders := store.LocalFoldersFromParts(promoted.UserMessage.Parts)
	if len(gotFolders) != 1 || gotFolders[0].Path != folder.Path {
		t.Fatalf("promoted message lost local folder: %+v", promoted.UserMessage.Parts)
	}
	if len(promoted.UserMessage.Parts) != 3 || promoted.UserMessage.Parts[0].Type != store.ContentPartLocalFolder || promoted.UserMessage.Parts[1].Type != store.ContentPartAttachment || promoted.UserMessage.Parts[2].Type != store.ContentPartText {
		t.Fatalf("promoted message lost parts order: %+v", promoted.UserMessage.Parts)
	}
}

func TestQueuedInputPersistsBrowserSelectionContext(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_browser_selection")
	const selectionText = "selected browser text"
	parts := []store.ContentPart{
		{
			Type:          store.ContentPartUIContext,
			Surface:       "browser",
			Resource:      "browser_tab",
			CallID:        "tab_1",
			Name:          "Example",
			URL:           "https://example.com/",
			SelectionText: selectionText,
			ResourceKind:  "webview",
		},
		{Type: store.ContentPartText, Text: "translate"},
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{
		SessionID:       "sess_browser_selection",
		ClientMessageID: "client_browser_selection",
		Text:            "translate",
		Parts:           parts,
		Provider:        "mock",
		Model:           "mock",
		Mode:            store.ModeChat,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	queued, err := reopened.ListQueuedInputs(ctx, "sess_browser_selection")
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || len(queued[0].Parts) != 2 || queued[0].Parts[0].SelectionText != selectionText {
		t.Fatalf("queued browser selection context was not persisted: %+v", queued)
	}
	promoted, err := reopened.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{
		SessionID:     "sess_browser_selection",
		TurnID:        "turn_browser_selection",
		UserMessageID: "msg_browser_selection",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted.UserMessage.Parts) != 2 || promoted.UserMessage.Parts[0].SelectionText != selectionText {
		t.Fatalf("promoted browser selection context was not persisted: %+v", promoted.UserMessage.Parts)
	}
}

func TestListMessagesPageUsesStableOrder(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	for i := 1; i <= 4; i++ {
		appendCompletedTestTurn(t, st, "sess_1", i)
	}

	first, err := st.ListMessagesPage(context.Background(), "sess_1", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore {
		t.Fatal("first recent page should report older messages")
	}
	got := messageLabels(first.Messages)
	want := []string{"user:hello", "assistant:assistant 3", "user:hello", "assistant:assistant 4"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}

	older, err := st.ListMessagesPage(context.Background(), "sess_1", first.Messages[0].ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	got = messageLabels(older.Messages)
	want = []string{"user:hello", "assistant:assistant 1", "user:hello", "assistant:assistant 2"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}

	_, err = st.ListMessagesPage(context.Background(), "sess_1", "missing", 4)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing before message should return ErrNotFound, got %v", err)
	}
}

func TestCloneSessionCopiesCanonicalPrefixAsIndependentHistory(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{
		ID:              "sess_source",
		Title:           "Source",
		Provider:        "mock",
		Model:           "model",
		ReasoningEffort: "high",
		ActiveMode:      store.ModeCode,
		ModeLease:       store.ModeLeaseSession,
		LoadedAppIDs:    []string{"app_1"},
	}); err != nil {
		t.Fatal(err)
	}
	appendCompletedTestTurn(t, st, "sess_source", 1)
	appendCompletedTestTurn(t, st, "sess_source", 2)
	sourceTurns, err := st.ListTurnsPage(ctx, "sess_source", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	boundary := sourceTurns.Turns[0].Messages[len(sourceTurns.Turns[0].Messages)-1]

	cloned, err := st.CloneSession(ctx, store.CloneSessionInput{
		SourceSessionID:  "sess_source",
		ThroughMessageID: boundary.ID,
		TargetSessionID:  "sess_clone",
		TitleSuffix:      "（副本）",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cloned.ID != "sess_clone" || cloned.Title != "Source（副本）" || cloned.Provider != "mock" || cloned.Model != "model" || cloned.ReasoningEffort != "high" {
		t.Fatalf("cloned session config = %+v", cloned)
	}
	if cloned.Pinned || cloned.ArchivedAt != nil || cloned.Running || !sameStrings(cloned.LoadedAppIDs, []string{"app_1"}) {
		t.Fatalf("cloned session runtime state = %+v", cloned)
	}
	targetTurns, err := st.ListTurnsPage(ctx, cloned.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetTurns.Turns) != 1 {
		t.Fatalf("cloned turns = %+v", targetTurns.Turns)
	}
	if got := messageLabels(targetTurns.Turns[0].Messages); !sameStrings(got, []string{"user:hello", "assistant:assistant 1"}) {
		t.Fatalf("cloned messages = %v", got)
	}
	for i, message := range targetTurns.Turns[0].Messages {
		if message.SessionID != cloned.ID || message.ID == sourceTurns.Turns[0].Messages[i].ID {
			t.Fatalf("message was not independently cloned: %+v", message)
		}
	}
	if seq, err := st.LatestSeq(ctx, cloned.ID); err != nil || seq != 0 {
		t.Fatalf("clone should not copy lifecycle events: seq=%d err=%v", seq, err)
	}
}

func TestCloneSessionKeepsTrailingProtocolState(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_protocol_source")
	beginTestTurn(t, st, "sess_protocol_source", "turn_protocol", "msg_protocol_user", "client_protocol")
	state := &store.ProviderState{
		Provider: "openai",
		Model:    "gpt-test",
		Kind:     "openai_responses",
		Data:     json.RawMessage(`[{"type":"reasoning","encrypted_content":"cipher"}]`),
	}
	output, err := st.AppendTurnOutput(ctx, store.AppendTurnOutputInput{
		TurnID: "turn_protocol",
		Parts: []store.ContentPart{{
			Type:    store.ContentPartToolResult,
			CallID:  "call_1",
			Name:    "tool",
			Ok:      true,
			Content: `{"ok":true}`,
		}},
		ProviderState: state,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(output.Messages) != 2 || !store.IsProtocolOnlyMessage(output.Messages[1]) {
		t.Fatalf("source protocol anchor = %+v", output.Messages)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn_protocol", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	cloned, err := st.CloneSession(ctx, store.CloneSessionInput{
		SourceSessionID:  "sess_protocol_source",
		ThroughMessageID: output.Messages[0].ID,
		TargetSessionID:  "sess_protocol_clone",
		TitleSuffix:      "（副本）",
	})
	if err != nil {
		t.Fatal(err)
	}
	messages, err := st.ListMessages(ctx, cloned.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || !store.IsProtocolOnlyMessage(messages[2]) || string(messages[2].ProviderState.Data) != string(state.Data) {
		t.Fatalf("cloned protocol state = %+v", messages)
	}
}

func TestListTurnsPageUsesStableOrder(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	for i := 1; i <= 4; i++ {
		appendCompletedTestTurn(t, st, "sess_1", i)
	}

	first, err := st.ListTurnsPage(context.Background(), "sess_1", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore {
		t.Fatal("first recent page should report older turns")
	}
	got := turnIDs(first.Turns)
	want := []string{"turn_3", "turn_4"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}
	gotMessages := messageLabels(first.Turns[0].Messages)
	wantMessages := []string{"user:hello", "assistant:assistant 3"}
	if !sameStrings(gotMessages, wantMessages) {
		t.Fatalf("turn should include complete messages: got %v want %v", gotMessages, wantMessages)
	}

	older, err := st.ListTurnsPage(context.Background(), "sess_1", first.Turns[0].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	got = turnIDs(older.Turns)
	want = []string{"turn_1", "turn_2"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}

	_, err = st.ListTurnsPage(context.Background(), "sess_1", "missing", 2)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing before turn should return ErrNotFound, got %v", err)
	}
}

func TestFinishTurnPersistsHistoricalFileChanges(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_changes")
	beginTestTurn(t, st, "sess_changes", "turn_changes", "msg_changes", "client_changes")

	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_changes",
		Status: store.TurnCompleted,
		FileChanges: []store.TurnFileChangeInput{{
			RootPath: "/tmp/project", Path: "main.go", Kind: store.FileChangeModified,
			Origin:    store.FileChangeOriginCommandObserved,
			Additions: 1, Deletions: 1, OldSize: 12, NewSize: 11,
			OldContent: "package old\n", NewContent: "package new\n",
			SnapshotVersion: 1,
			OldDigest:       fmt.Sprintf("%x", sha256.Sum256([]byte("package old\n"))),
			NewDigest:       fmt.Sprintf("%x", sha256.Sum256([]byte("package new\n"))),
			OldMode:         0o644, NewMode: 0o644, OldType: "file", NewType: "file",
		}},
	}); err != nil {
		t.Fatal(err)
	}

	turn, err := st.GetConversationTurn(context.Background(), "sess_changes", "turn_changes")
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.FileChanges) != 1 {
		t.Fatalf("file changes = %+v", turn.FileChanges)
	}
	summary := turn.FileChanges[0]
	if summary.Path != "main.go" || summary.Kind != store.FileChangeModified || !summary.Reversible ||
		summary.Origin != store.FileChangeOriginCommandObserved || summary.OldContent != "" || summary.NewContent != "" {
		t.Fatalf("file change summary = %+v", summary)
	}
	detail, err := st.GetTurnFileChange(context.Background(), "sess_changes", "turn_changes", summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Origin != store.FileChangeOriginCommandObserved ||
		detail.OldContent != "package old\n" || detail.NewContent != "package new\n" || detail.OldMode != 0o644 {
		t.Fatalf("file change detail = %+v", detail)
	}
	if _, err := st.GetTurnFileChange(context.Background(), "other_session", "turn_changes", summary.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-session file change error = %v", err)
	}
	if turn.FileChangeState != store.TurnFileChangesApplied {
		t.Fatalf("file change state = %q", turn.FileChangeState)
	}
	if err := st.UpdateTurnFileChangeState(context.Background(), "sess_changes", "turn_changes", store.TurnFileChangesApplied, store.TurnFileChangesUndone); err != nil {
		t.Fatal(err)
	}
	turn, err = st.GetConversationTurn(context.Background(), "sess_changes", "turn_changes")
	if err != nil || turn.FileChangeState != store.TurnFileChangesUndone {
		t.Fatalf("updated file change state = %q err=%v", turn.FileChangeState, err)
	}
	if err := st.UpdateTurnFileChangeState(context.Background(), "sess_changes", "turn_changes", store.TurnFileChangesApplied, store.TurnFileChangesUndone); !errors.Is(err, store.ErrTurnFileChangeConflict) {
		t.Fatalf("stale state update error = %v", err)
	}
}

func TestFinishTurnPersistsBinarySnapshots(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_binary_changes")
	beginTestTurn(t, st, "sess_binary_changes", "turn_binary_changes", "msg_binary_changes", "client_binary_changes")
	oldData, newData := []byte{0, 1, 2}, []byte{0, 1, 3}
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_binary_changes", Status: store.TurnCompleted,
		FileChanges: []store.TurnFileChangeInput{{
			RootPath: "/tmp/project", Path: "image.bin", Kind: store.FileChangeModified,
			Binary: true, OldBinary: true, NewBinary: true, SnapshotVersion: 1,
			OldDigest: fmt.Sprintf("%x", sha256.Sum256(oldData)), NewDigest: fmt.Sprintf("%x", sha256.Sum256(newData)),
			OldMode: 0o644, NewMode: 0o644, OldType: "file", NewType: "file", OldData: oldData, NewData: newData,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	turn, err := st.GetConversationTurn(context.Background(), "sess_binary_changes", "turn_binary_changes")
	if err != nil || len(turn.FileChanges) != 1 || !turn.FileChanges[0].Reversible {
		t.Fatalf("binary summary=%+v err=%v", turn.FileChanges, err)
	}
	detail, err := st.GetTurnFileChange(context.Background(), "sess_binary_changes", "turn_binary_changes", turn.FileChanges[0].ID)
	if err != nil || string(detail.OldData) != string(oldData) || string(detail.NewData) != string(newData) || !detail.OldBinary || !detail.NewBinary {
		t.Fatalf("binary detail=%+v err=%v", detail, err)
	}
}

func TestFinishTurnTerminalStatesAndEventsAfter(t *testing.T) {
	st, _ := openTestStore(t)

	cases := []struct {
		sessionID   string
		turnID      string
		msgID       string
		clientID    string
		status      store.TurnStatus
		kind        event.Kind
		text        *string
		interrupted bool
		errorText   string
	}{
		{"sess_completed", "turn_completed", "msg_completed", "client_completed", store.TurnCompleted, event.TurnCompleted, strptr("done"), false, ""},
		{"sess_failed", "turn_failed", "msg_failed", "client_failed", store.TurnFailed, event.TurnFailed, strptr("partial"), true, "boom"},
		{"sess_cancelled", "turn_cancelled", "msg_cancelled", "client_cancelled", store.TurnCancelled, event.TurnCancelled, strptr("partial"), true, ""},
	}

	for _, tc := range cases {
		createTestSession(t, st, tc.sessionID)
		beginTestTurn(t, st, tc.sessionID, tc.turnID, tc.msgID, tc.clientID)
		var parts []store.ContentPart
		if tc.text != nil {
			parts = store.TextPart(*tc.text)
		}
		res, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
			TurnID:         tc.turnID,
			Status:         tc.status,
			AssistantParts: parts,
			Interrupted:    tc.interrupted,
			Error:          tc.errorText,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.FinalEvent.Kind != tc.kind || res.FinalEvent.Seq != 2 {
			t.Fatalf("unexpected final event: %+v", res.FinalEvent)
		}
		if tc.text != nil {
			if res.AssistantMessage == nil || res.FinalEvent.AssistantMessageID != res.AssistantMessage.ID {
				t.Fatalf("final event must reference assistant message: %+v", res)
			}
			if res.AssistantMessage.Interrupted != tc.interrupted {
				t.Fatalf("unexpected interrupted flag: %+v", res.AssistantMessage)
			}
		}

		afterStart, err := st.EventsAfter(context.Background(), tc.sessionID, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(afterStart) != 1 || afterStart[0].Kind != tc.kind || afterStart[0].Seq != 2 {
			t.Fatalf("EventsAfter should return final event only: %+v", afterStart)
		}
	}
}

func TestAppendCompactSummaryPersistsMetadataAndEvent(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_compact")

	res, err := st.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{
		SessionID:       "sess_compact",
		TurnID:          "turn_compact",
		MessageID:       "msg_compact",
		ClientMessageID: "compact:turn_compact",
		Provider:        "mock",
		Model:           "mock",
		Text:            "summary @message(msg_1)",
		Metadata:        store.CompactMessageMetadata([]string{"msg_1", "msg_2"}, []string{"msg_3"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalEvent == nil || res.FinalEvent.Kind != event.TurnCompleted || res.FinalEvent.AssistantMessageID != "msg_compact" {
		t.Fatalf("unexpected compact event: %+v", res.FinalEvent)
	}
	if _, err := st.RunningTurn(ctx, "sess_compact"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("compact must not leave a running turn: %v", err)
	}
	msgs, err := st.ListMessages(ctx, "sess_compact", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != store.RoleSummary || msgs[0].Kind != store.MessageKindSummary {
		t.Fatalf("unexpected compact message: %+v", msgs)
	}
	meta, ok := store.CompactMetadataFromMessage(msgs[0])
	if !ok {
		t.Fatalf("compact metadata missing: %+v", msgs[0])
	}
	if !sameStrings(meta.SourceMessageIDs, []string{"msg_1", "msg_2"}) || !sameStrings(meta.TailMessageIDs, []string{"msg_3"}) {
		t.Fatalf("unexpected compact metadata: %+v", meta)
	}
	events, err := st.EventsAfter(ctx, "sess_compact", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != event.TurnCompleted || events[0].TurnID != "turn_compact" {
		t.Fatalf("unexpected compact events: %+v", events)
	}
}

func TestPersistenceAndSeqContinuation(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	text := "first"
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         "turn_1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	sessions, err := reopened.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess_1" {
		t.Fatalf("sessions not persisted: %+v", sessions)
	}
	msgs, err := reopened.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Text != "first" {
		t.Fatalf("messages not persisted: %+v", msgs)
	}
	evs, err := reopened.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Seq != 2 {
		t.Fatalf("events not persisted: %+v", evs)
	}

	beginTestTurn(t, reopened, "sess_1", "turn_2", "msg_2", "client_2")
	evs, err = reopened.EventsAfter(context.Background(), "sess_1", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Seq != 3 || evs[0].Kind != event.TurnStarted {
		t.Fatalf("seq did not continue after reopen: %+v", evs)
	}
}

func TestMessagePartsPersist(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartThought, Text: "thinking"},
			{Type: store.ContentPartToolUse, CallID: "call_1", Name: "web_fetch", Args: []byte(`{"url":"https://example.com"}`)},
			{Type: store.ContentPartToolResult, CallID: "call_1", Name: "web_fetch", Ok: true, Content: `{"ok":true}`},
			{Type: store.ContentPartText, Text: "done"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	msgs, err := reopened.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	thought := msgs[1]
	if thought.Role != store.RoleAssistant || thought.Kind != store.MessageKindThought || thought.Text != "thinking" {
		t.Fatalf("thought message not persisted: %+v", thought)
	}
	toolUse := msgs[2]
	if toolUse.Role != store.RoleAssistant || toolUse.Kind != store.MessageKindToolUse || len(toolUse.Parts) != 1 || toolUse.Parts[0].Type != store.ContentPartToolUse {
		t.Fatalf("tool_use message not persisted: %+v", toolUse)
	}
	toolResult := msgs[3]
	if toolResult.Role != store.RoleTool || toolResult.Kind != store.MessageKindToolResult || len(toolResult.Parts) != 1 || toolResult.Parts[0].Type != store.ContentPartToolResult {
		t.Fatalf("tool_result message not persisted: %+v", toolResult)
	}
	assistant := msgs[4]
	if assistant.Role != store.RoleAssistant || assistant.Kind != store.MessageKindText || assistant.Text != "done" {
		t.Fatalf("text message not persisted: %+v", assistant)
	}
	turns, err := reopened.ListTurnsPage(context.Background(), "sess_1", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns.Turns) != 1 {
		t.Fatalf("unexpected turns: %+v", turns.Turns)
	}
	got := messageLabels(turns.Turns[0].Messages)
	want := []string{"user:hello", "assistant:thinking", "assistant:web_fetch", "tool:{\"ok\":true}", "assistant:done"}
	if !sameStrings(got, want) {
		t.Fatalf("turn should group all messages in turn_index order: got %v want %v", got, want)
	}
}

func TestAppendTurnOutputBeforeFinish(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")

	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts:  store.TextPart("first"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts: []store.ContentPart{{
			Type:   store.ContentPartToolUse,
			CallID: "call_1",
			Name:   "builtin_time_get_current",
			Args:   []byte(`{"timezone":"Asia/Singapore"}`),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_1",
		Status: store.TurnCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := messageLabels(msgs), []string{"user:hello", "assistant:first", "assistant:builtin_time_get_current"}; !sameStrings(got, want) {
		t.Fatalf("finish must not duplicate appended output: got %v want %v", got, want)
	}
	if msgs[1].Text != "first" || msgs[2].Kind != store.MessageKindToolUse {
		t.Fatalf("unexpected appended messages: %+v", msgs)
	}
	evs, err := st.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[1].Kind != event.TurnCompleted || evs[1].AssistantMessageID != msgs[1].ID {
		t.Fatalf("final event should point at first appended output: %+v", evs)
	}
}

func TestAppendTurnOutputPersistsToolResultDisplayAttachments(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	item := store.Attachment{
		ID:            "att_photo",
		Name:          "photo.jpg",
		AttachmentKey: "sessions/sess_1/blobs/photo.jpg",
		URL:           "/sessions/sess_1/attachments/blobs/photo.jpg",
		MIME:          "image/jpeg",
		Size:          4,
		Origin:        "tool",
	}

	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts: []store.ContentPart{{
			Type:        store.ContentPartToolResult,
			CallID:      "call_1",
			Name:        "builtin_camera_capture",
			Content:     `{"ok":true}`,
			Ok:          true,
			Attachments: []store.Attachment{item},
		}},
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || len(msgs[1].Parts) != 1 || len(msgs[1].Parts[0].Attachments) != 1 ||
		msgs[1].Parts[0].Attachments[0].AttachmentKey != item.AttachmentKey {
		t.Fatalf("tool result display attachment was not persisted: %+v", msgs)
	}
}

func TestAppendTurnOutputPersistsHiddenProviderState(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	state := &store.ProviderState{
		Provider: "openai",
		Model:    "gpt-test",
		Kind:     "openai_responses",
		Data:     json.RawMessage(`[{"type":"reasoning","encrypted_content":"cipher"}]`),
	}
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID:        "turn_1",
		Parts:         store.TextPart("done"),
		ProviderState: state,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_1",
		Status: store.TurnCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	assertHiddenState := func(messages []*store.Message) {
		t.Helper()
		if len(messages) != 2 || messages[1].ProviderState == nil ||
			string(messages[1].ProviderState.Data) != string(state.Data) {
			t.Fatalf("provider state missing: %+v", messages)
		}
		if len(messages[1].Metadata) != 0 {
			t.Fatalf("hidden provider state must not create public metadata: %s", messages[1].Metadata)
		}
		encoded, err := json.Marshal(messages[1])
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "cipher") ||
			strings.Contains(string(encoded), "_provider_state") ||
			strings.Contains(string(encoded), `"metadata"`) {
			t.Fatalf("provider state leaked through public JSON: %s", encoded)
		}
	}

	messages, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertHiddenState(messages)

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	messages, err = reopened.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertHiddenState(messages)
}

func TestAppendTurnOutputPreservesStateOnlyContinuationAfterToolResult(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	firstState := &store.ProviderState{
		Provider: "openai",
		Model:    "gpt-test",
		Kind:     "openai_responses",
		Data:     json.RawMessage(`[{"type":"reasoning","encrypted_content":"first"}]`),
	}
	secondState := &store.ProviderState{
		Provider: "openai",
		Model:    "gpt-test",
		Kind:     "openai_responses",
		Data:     json.RawMessage(`[{"type":"reasoning","encrypted_content":"second"}]`),
	}
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts: []store.ContentPart{{
			Type:   store.ContentPartToolUse,
			CallID: "call_1",
			Name:   "builtin_time_get_current",
			Args:   json.RawMessage(`{}`),
		}},
		ProviderState: firstState,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts: []store.ContentPart{{
			Type:    store.ContentPartToolResult,
			CallID:  "call_1",
			Name:    "builtin_time_get_current",
			Content: `{"time":"now"}`,
			Ok:      true,
		}},
		ProviderState: secondState,
	}); err != nil {
		t.Fatal(err)
	}

	messages, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 4 {
		t.Fatalf("messages = %+v, want user, tool use, tool result, state anchor", messages)
	}
	if got := string(messages[1].ProviderState.Data); got != string(firstState.Data) {
		t.Fatalf("first provider state was overwritten: %s", got)
	}
	if messages[3].Role != store.RoleAssistant || !store.IsProtocolOnlyMessage(messages[3]) ||
		string(messages[3].ProviderState.Data) != string(secondState.Data) {
		t.Fatalf("state-only continuation was not anchored separately: %+v", messages[3])
	}

	turn, err := st.GetConversationTurn(context.Background(), "sess_1", "turn_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(turn.Messages) != 3 {
		t.Fatalf("conversation turn leaked protocol-only anchor: %+v", turn.Messages)
	}
	for _, message := range turn.Messages {
		if store.IsProtocolOnlyMessage(message) {
			t.Fatalf("conversation turn contains protocol-only message: %+v", message)
		}
	}
}

func TestAppendTurnSteerPersistsUserMessageAndEvent(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts:  store.TextPart("first"),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := st.AppendTurnSteer(context.Background(), store.AppendTurnSteerInput{
		SessionID:       "sess_1",
		TurnID:          "turn_1",
		UserMessageID:   "msg_steer",
		ClientMessageID: "client_steer",
		UserText:        "change direction",
		UserParts:       store.TextPart("change direction"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate || res.UserMessage == nil || res.Event == nil || res.Event.Kind != event.InputSteered {
		t.Fatalf("unexpected steer result: %+v", res)
	}
	duplicate, err := st.AppendTurnSteer(context.Background(), store.AppendTurnSteerInput{
		SessionID:       "sess_1",
		TurnID:          "turn_1",
		UserMessageID:   "msg_other",
		ClientMessageID: "client_steer",
		UserText:        "ignored",
		UserParts:       store.TextPart("ignored"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Event != nil || duplicate.UserMessage.ID != "msg_steer" {
		t.Fatalf("steer retry should be idempotent: %+v", duplicate)
	}
	if err := st.ApplyTurnSteers(context.Background(), store.ApplyTurnSteersInput{
		TurnID:     "turn_1",
		MessageIDs: []string{"msg_steer"},
		Events:     []*event.Event{res.Event},
	}); err != nil {
		t.Fatal(err)
	}
	if res.Event.Seq == 0 {
		t.Fatal("steer event should receive its seq at the safe sampling boundary")
	}
	msgs, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := messageLabels(msgs), []string{"user:hello", "assistant:first", "user:change direction"}; !sameStrings(got, want) {
		t.Fatalf("steer message order: got %v want %v", got, want)
	}
	events, err := st.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Kind != event.InputSteered || events[1].UserMessageID != "msg_steer" {
		t.Fatalf("unexpected steer events: %+v", events)
	}
}

func TestSteerQueuedInputPromotesIntoRunningTurnAtomically(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts:  store.TextPart("first"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.QueueInput(context.Background(), store.QueueInputInput{
		SessionID:       "sess_1",
		ClientMessageID: "client_queued",
		Text:            "guide from queue",
		Parts:           store.TextPart("guide from queue"),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := st.SteerQueuedInput(context.Background(), store.SteerQueuedInputInput{
		SessionID:       "sess_1",
		TurnID:          "turn_missing",
		ClientMessageID: "client_queued",
		UserMessageID:   "msg_wrong",
	}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("wrong turn should fail without consuming queue: %v", err)
	}
	queued, err := st.ListQueuedInputs(context.Background(), "sess_1")
	if err != nil || len(queued) != 1 {
		t.Fatalf("queued input should remain after failed steer: %+v err=%v", queued, err)
	}

	res, err := st.SteerQueuedInput(context.Background(), store.SteerQueuedInputInput{
		SessionID:       "sess_1",
		TurnID:          "turn_1",
		ClientMessageID: "client_queued",
		UserMessageID:   "msg_queued_steer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate || res.Input.Status != store.QueuedInputPromoted || res.Input.TurnID != "turn_1" {
		t.Fatalf("unexpected queued steer result: %+v", res)
	}
	if res.UpdatedEvent == nil || res.UpdatedEvent.Kind != event.InputUpdated || res.UpdatedEvent.Status != string(store.QueuedInputPromoted) {
		t.Fatalf("missing promoted event: %+v", res.UpdatedEvent)
	}
	if res.SteeredEvent == nil || res.SteeredEvent.Kind != event.InputSteered {
		t.Fatalf("missing steered event: %+v", res.SteeredEvent)
	}
	queued, err = st.ListQueuedInputs(context.Background(), "sess_1")
	if err != nil || len(queued) != 0 {
		t.Fatalf("promoted input should leave queue: %+v err=%v", queued, err)
	}
	if _, err := st.QueueInput(context.Background(), store.QueueInputInput{
		SessionID:       "sess_1",
		ClientMessageID: "client_after",
		Text:            "later queue update",
		Parts:           store.TextPart("later queue update"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApplyTurnSteers(context.Background(), store.ApplyTurnSteersInput{
		TurnID:     "turn_1",
		MessageIDs: []string{"msg_queued_steer"},
		Events:     []*event.Event{res.SteeredEvent},
	}); err != nil {
		t.Fatal(err)
	}
	msgs, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := messageLabels(msgs), []string{"user:hello", "assistant:first", "user:guide from queue"}; !sameStrings(got, want) {
		t.Fatalf("queued steer must stay in original turn: got %v want %v", got, want)
	}
	events, err := st.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[len(events)-1].Kind != event.InputSteered {
		t.Fatalf("steered event must be sequenced at the apply boundary: %+v", events)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Seq <= events[i-1].Seq {
			t.Fatalf("event sequence must stay monotonic: %+v", events)
		}
	}
}

func TestTurnModelConfigPersists(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	cfg := []byte(`{"contextWindow":1000,"openai":{"temperature":0.6}}`)
	if _, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_1",
		UserMessageID:   "msg_1",
		ClientMessageID: "client_1",
		UserText:        "hello",
		Provider:        "default",
		Model:           "m1",
		ModelConfig:     cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	turn, err := reopened.RunningTurn(context.Background(), "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if string(turn.ModelConfig) != string(cfg) {
		t.Fatalf("model config not persisted: %s", turn.ModelConfig)
	}
	turns, err := reopened.ListTurnsPage(context.Background(), "sess_1", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns.Turns) != 1 {
		t.Fatalf("unexpected turns: %+v", turns.Turns)
	}
	if turns.Turns[0].Provider != "default" || turns.Turns[0].Model != "m1" {
		t.Fatalf("conversation turn model snapshot wrong: provider=%q model=%q", turns.Turns[0].Provider, turns.Turns[0].Model)
	}
}

func TestDeleteSessionCascades(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	text := "done"
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         "turn_1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession(context.Background(), "sess_1"); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"turns", "messages", "events"} {
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s should cascade delete, count=%d", table, count)
		}
	}
}

func TestSchemaDoesNotStoreConfigTables(t *testing.T) {
	st, _ := openTestStore(t)
	rows, err := st.db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == "settings" || name == "provider_profiles" {
			t.Fatalf("config table %q must not be in SQLite runtime store", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestUsageHourlyStatsRecordAndQuery(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 6, 23, 10, 37, 0, 0, time.FixedZone("SGT", 8*60*60))

	first, err := st.RecordUsage(ctx, store.UsageRecordInput{
		OccurredAt:            at,
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantHour := at.UTC().Truncate(time.Hour)
	if !first.HourStartAt.Equal(wantHour) || first.RequestCount != 1 {
		t.Fatalf("first stat = %+v want hour %s count 1", first, wantHour)
	}
	if first.TotalTokens() != 150 {
		t.Fatalf("first total = %d want 150", first.TotalTokens())
	}

	second, err := st.RecordUsage(ctx, store.UsageRecordInput{
		OccurredAt:            at.Add(10 * time.Minute),
		RequestCount:          2,
		InputUncachedTokens:   -1,
		InputCachedTokens:     1,
		OutputContentTokens:   2,
		OutputReasoningTokens: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.RequestCount != 3 || second.InputUncachedTokens != 10 || second.InputCachedTokens != 21 || second.TotalTokens() != 156 {
		t.Fatalf("merged stat wrong: %+v", second)
	}

	third, err := st.RecordUsage(ctx, store.UsageRecordInput{
		OccurredAt:          at.Add(20 * time.Minute),
		Model:               "model-b",
		InputUncachedTokens: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if third.Model != "model-b" || third.RequestCount != 1 || third.TotalTokens() != 7 {
		t.Fatalf("model stat wrong: %+v", third)
	}

	stats, err := st.UsageHourlyStats(ctx, wantHour, wantHour.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Model != "" || stats[0].RequestCount != 3 || stats[0].TotalTokens() != 156 || stats[1].Model != "model-b" || stats[1].TotalTokens() != 7 {
		t.Fatalf("queried stats wrong: %+v", stats)
	}
}

func TestSessionUsageRecordAndQuery(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_usage")

	empty, err := st.SessionUsage(ctx, "sess_usage")
	if err != nil {
		t.Fatal(err)
	}
	if empty.SessionID != "sess_usage" || empty.RequestCount != 0 || empty.CumulativeTotalTokens() != 0 {
		t.Fatalf("empty session usage wrong: %+v", empty)
	}

	first, err := st.RecordSessionUsage(ctx, "sess_usage", store.UsageRecordInput{
		Provider:              "profile-a",
		Model:                 "model-a",
		EstimatedInputTokens:  100,
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.RequestCount != 1 || first.LastProvider != "profile-a" || first.LastModel != "model-a" ||
		first.LastEstimatedInputTokens != 100 || first.LastInputTokens() != 60 || first.LastOutputTokens() != 90 || first.CumulativeTotalTokens() != 150 {
		t.Fatalf("first session usage wrong: %+v", first)
	}

	second, err := st.RecordSessionUsage(ctx, "sess_usage", store.UsageRecordInput{
		Provider:              "profile-b",
		Model:                 "model-b",
		EstimatedInputTokens:  40,
		RequestCount:          2,
		InputUncachedTokens:   -1,
		InputCachedTokens:     1,
		OutputContentTokens:   2,
		OutputReasoningTokens: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.RequestCount != 3 || second.LastProvider != "profile-b" || second.LastModel != "model-b" ||
		second.LastEstimatedInputTokens != 40 || second.LastInputUncachedTokens != 0 || second.LastInputTokens() != 1 ||
		second.LastOutputTokens() != 5 || second.CumulativeTotalTokens() != 156 {
		t.Fatalf("merged session usage wrong: %+v", second)
	}

	if err := st.DeleteSession(ctx, "sess_usage"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionUsage(ctx, "sess_usage"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted session usage should be not found, got %v", err)
	}
}

func strptr(s string) *string { return &s }

func messageLabels(messages []*store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, string(msg.Role)+":"+msg.Text)
	}
	return out
}

func turnIDs(turns []*store.ConversationTurn) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.ID)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

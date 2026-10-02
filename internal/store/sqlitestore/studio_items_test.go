package sqlitestore

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestStudioItemVersionsSurviveSessionDeletionAndRestart(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "widget.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "origin")
	now := time.Now().UTC()
	w, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "wb_1", Name: "Test", SourceSessionID: "origin", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	r := &store.StudioItemRevision{ItemID: w.ID, Hash: "hash_a", ClientRequestID: "save_a", CreatedAt: now}
	w, err = s.SaveStudioItemRevision(ctx, r, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveStudioItemRevision(ctx, r, ""); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
	r.ClientRequestID = "save_b"
	w, err = s.SaveStudioItemRevision(ctx, r, w.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.SaveStudioItemRevision(ctx, r, ""); err != nil {
		t.Fatalf("same package new request retry: %v", err)
	}
	r.Hash = "hash_b"
	if _, err = s.SaveStudioItemRevision(ctx, r, w.HeadRevision); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatalf("request ID reused for other source: %v", err)
	}
	w.ActiveRevision = "hash_a"
	if _, err = s.UpdateStudioItem(ctx, w, w.Revision); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatalf("activated unbuilt source: %v", err)
	}
	if err = s.PutWidgetBuildReceipt(ctx, w.ID, "hash_a", []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w, err = s.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateStudioItem(ctx, w, w.Revision-1); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatalf("stale update accepted: %v", err)
	}
	if err = s.DeleteSession(ctx, "origin"); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, err = s.GetStudioItem(ctx, w.ID)
	if err != nil {
		t.Fatal(err)
	}
	if w.SourceSessionID != "" || w.ActiveRevision != "hash_a" {
		t.Fatalf("wrong recovered widget: %+v", w)
	}
	versions, err := s.ListStudioItemRevisions(ctx, w.ID)
	if err != nil || len(versions) != 1 {
		t.Fatalf("duplicate source versions: %+v %v", versions, err)
	}
}

func TestStudioItemAppearanceSurvivesMountAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "widget.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "origin")
	w, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "appearance", Name: "Weather", Icon: "cloud-sun", IconColor: "blue", SourceSessionID: "origin"})
	if err != nil {
		t.Fatal(err)
	}
	w.Icon, w.IconColor = "chart-column", "teal"
	w, err = s.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	item, err := s.OpenStudioItem(ctx, "origin", w.ID, "view")
	if err != nil || item.Icon != "chart-column" || item.IconColor != "teal" {
		t.Fatalf("mount appearance: %+v %v", item, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w, err = s.GetStudioItem(ctx, "appearance")
	if err != nil || w.Icon != "chart-column" || w.IconColor != "teal" {
		t.Fatalf("reopened appearance: %+v %v", w, err)
	}
	items, err := s.ListStudioMounts(ctx, "origin")
	if err != nil || len(items) != 1 || items[0].Icon != w.Icon || items[0].IconColor != w.IconColor {
		t.Fatalf("reopened mounts: %+v %v", items, err)
	}
}

func TestStudioItemMetadataChangesKeepListOrder(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "widget.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	start := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)
	older, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "older", Name: "Older", CreatedAt: start, UpdatedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "newer", Name: "Newer", CreatedAt: start.Add(time.Minute), UpdatedAt: start.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	order := func() string {
		t.Helper()
		list, err := s.ListStudioItems(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ids := []string{}
		for _, w := range list {
			ids = append(ids, w.ID)
		}
		return strings.Join(ids, ",")
	}
	older.Icon, older.IconColor = "chart-column", "teal"
	older, err = s.UpdateStudioItem(ctx, older, older.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if !older.UpdatedAt.Equal(start) || order() != "newer,older" {
		t.Fatalf("appearance change reordered the list: %s %v", order(), older.UpdatedAt)
	}
	if _, err := s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: "older", Hash: "edit", ClientRequestID: "edit", CreatedAt: start.Add(2 * time.Minute)}, ""); err != nil {
		t.Fatal(err)
	}
	if order() != "older,newer" {
		t.Fatalf("a new revision did not move the widget first: %s", order())
	}
}

func TestSaveStudioItemRevisionIgnoresMetadataRevision(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "widget.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	w, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "canvas_metadata", Name: "First", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: "first", ClientRequestID: "save-first", CreatedAt: now}, "")
	if err != nil {
		t.Fatal(err)
	}
	w.Name = "Renamed"
	w, err = s.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	w, err = s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: "second", ClientRequestID: "save-second", CreatedAt: now}, "first")
	if err != nil || w.HeadRevision != "second" || w.Name != "Renamed" {
		t.Fatalf("source save conflicted with metadata change: %+v %v", w, err)
	}
	if _, err = s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: "stale", ClientRequestID: "save-stale", CreatedAt: now}, "first"); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatalf("stale source base was accepted: %v", err)
	}
}

func TestWidgetActionClaimAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "actions.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	w, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "widget-actions", Name: "Actions", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	w, err = s.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: w.ID, Hash: hash, ClientRequestID: "save", CreatedAt: now}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = s.PutWidgetBuildReceipt(ctx, w.ID, hash, json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	w.ActiveRevision = hash
	w, err = s.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	a := &store.WidgetAction{ID: "action", ItemID: w.ID, ClientRequestID: "request", RequestHash: "intent", State: "prepared", CreatedAt: now, Spec: store.WidgetActionSpec{RevisionHash: hash, ResourceRevision: w.Revision, BindingVersion: w.BindingVersion, Request: json.RawMessage(`{}`)}}
	if _, err = s.CreateWidgetAction(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimWidgetAction(ctx, w.ID, a.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ClaimWidgetAction(ctx, w.ID, a.ID); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal("duplicate claim", err)
	}
	_ = s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.GetWidgetAction(ctx, w.ID, a.ID)
	if err != nil || restored.State != "unknown" {
		t.Fatalf("restart %+v %v", restored, err)
	}
	if err = s.ClaimWidgetAction(ctx, w.ID, a.ID); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal("unknown action retried", err)
	}
	a.ID = "another"
	a.RequestHash = "changed"
	if _, err = s.CreateWidgetAction(ctx, a); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal("idempotency payload collision accepted", err)
	}
}

func TestWidgetLinksRetainOriginalAccounts(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "links.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	w, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: store.StudioItemKindWidget, ID: "wb_links", Name: "Links", CreatedAt: now, UpdatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	link := &store.WidgetLink{ID: "link_one", ItemID: w.ID, Left: store.WidgetEntity{PluginID: "mail", ConnectionID: "old-mail", EntityType: "message", EntityID: "one"}, Right: store.WidgetEntity{PluginID: "issues", ConnectionID: "old-issues", EntityType: "issue", EntityID: "one"}, CreatedAt: now}
	if err = s.PutWidgetLink(ctx, link, w.Revision); err != nil {
		t.Fatal(err)
	}
	w.Bindings = map[string]string{"mail": "new-mail", "issues": "new-issues"}
	w.BindingVersion++
	w, err = s.UpdateStudioItem(ctx, w, w.Revision)
	if err != nil {
		t.Fatal(err)
	}
	links, err := s.ListWidgetLinks(ctx, w.ID)
	if err != nil || len(links) != 1 || links[0].Left.ConnectionID != "old-mail" || links[0].Right.ConnectionID != "old-issues" {
		t.Fatalf("link changed account %+v %v", links, err)
	}
	if err = s.PutWidgetLink(ctx, link, w.Revision-1); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatal("stale link accepted", err)
	}
	if err = s.DeleteWidgetLink(ctx, w.ID, link.ID, w.Revision); err != nil {
		t.Fatal(err)
	}
	links, err = s.ListWidgetLinks(ctx, w.ID)
	if err != nil || len(links) != 0 {
		t.Fatal("link removal failed", err)
	}
}

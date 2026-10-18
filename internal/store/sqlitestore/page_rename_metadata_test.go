package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

// UpdatedAt tracks saved content, not metadata. Renaming a Page must keep that
// contract just like appearance changes, while still advancing its CAS revision.
func TestPageRenameKeepsContentTimestampAndRejectsStaleRevision(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "page-rename.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	start := time.Date(2026, time.October, 18, 0, 0, 0, 0, time.UTC)
	older, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: "page", ID: "older", Name: "Original", CreatedAt: start, UpdatedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateStudioItem(ctx, &store.StudioItem{Kind: "page", ID: "newer", Name: "Newer", CreatedAt: start.Add(time.Minute), UpdatedAt: start.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}

	expected := older.Revision
	older.Name = "Renamed"
	renamed, err := s.UpdateStudioItem(ctx, older, expected)
	if err != nil {
		t.Fatal(err)
	}
	assertMetadata := func(item *store.StudioItem) {
		t.Helper()
		if item.Name != "Renamed" || item.Revision != expected+1 || !item.UpdatedAt.Equal(start) || !item.CreatedAt.Equal(start) {
			t.Fatalf("rename changed content metadata or lost the name/revision: %+v", item)
		}
		if item.HeadRevision != older.HeadRevision || item.ActiveRevision != older.ActiveRevision {
			t.Fatalf("rename changed content revisions: %+v", item)
		}
	}
	assertMetadata(renamed)
	items, err := s.ListStudioItems(ctx, store.StudioItemsActive)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ID != "newer" || items[1].ID != "older" {
		t.Fatalf("rename reordered the recent-content list: %+v", items)
	}

	older.Name = "Stale rename"
	if _, err := s.UpdateStudioItem(ctx, older, expected); !errors.Is(err, store.ErrStudioItemConflict) {
		t.Fatalf("stale rename was not rejected: %v", err)
	}
	persisted, err := s.GetStudioItem(ctx, older.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertMetadata(persisted)
}

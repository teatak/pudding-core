package sqlitestore

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestAssistantAppearanceHistory(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	in := assistantInput(0)
	in.Appearance["extension"] = map[string]any{"custom": true}
	initial, err := s.SaveAssistant(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	current := initial
	change := func(palette, group string, want ...string) {
		t.Helper()
		in.ExpectedRevision = &current.Assistant.Revision
		in.Appearance = maps.Clone(in.Appearance)
		in.Appearance["palette"] = palette
		in.AppearanceEditGroup = group
		current, err = s.SaveAssistant(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, previous := range current.Assistant.AppearanceHistory {
			got = append(got, previous["palette"].(string))
			if !reflect.DeepEqual(previous["extension"], in.Appearance["extension"]) {
				t.Fatal("lost client extension")
			}
		}
		if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
			t.Fatalf("history: %v, want %v", got, want)
		}
	}
	finish := func(group string, want ...string) {
		t.Helper()
		current, err = s.FinishAssistantAppearanceEdit(ctx, group)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, previous := range current.Assistant.AppearanceHistory {
			got = append(got, previous["palette"].(string))
		}
		if len(got) != len(want) || (len(want) > 0 && !reflect.DeepEqual(got, want)) {
			t.Fatalf("finished history: %v, want %v", got, want)
		}
	}
	change("blue", "edit-1")
	change("gold", "edit-1")
	change("mint", "edit-1")
	finish("edit-1") // Returning to the opening look creates no history, even on close.
	change("gold", "edit-2")
	finish("edit-2", "mint")
	finish("edit-2", "mint") // Lost close responses can be retried.
	change("coral", "edit-3", "mint")
	finish("edit-3", "gold", "mint")
	change("crimson", "", "coral", "gold", "mint") // One-click outfit is already a completed operation.
	change("apricot", "", "crimson", "coral", "gold", "mint")
	change("coral", "restore-1", "crimson", "coral", "gold", "mint")
	finish("restore-1", "apricot", "crimson", "gold", "mint")
	change("violet", "edit-4", "apricot", "crimson", "gold", "mint")
	finish("edit-4", "coral", "apricot", "crimson", "gold", "mint")
	finish("unchanged-dialog", "coral", "apricot", "crimson", "gold", "mint")
	change("blue", "", "violet", "coral", "apricot", "crimson", "gold", "mint")
	change("emerald", "", "blue", "violet", "coral", "apricot", "crimson", "gold") // Seventh entry evicts the oldest.
	change("blue", "", "emerald", "violet", "coral", "apricot", "crimson", "gold") // Restores exclude the active appearance.

	favorite, err := s.SaveAssistantAppearanceFavorite(ctx, initial.Assistant.Appearance)
	if err != nil {
		t.Fatal(err)
	}
	if len(favorite.Assistant.AppearanceFavorites) != 1 {
		t.Fatal("favorite missing")
	}
	current = favorite
	before := current
	in.ExpectedRevision = &before.Assistant.Revision
	in.Name = "New name"
	current, err = s.SaveAssistant(ctx, in)
	if err != nil || !reflect.DeepEqual(current.Assistant.AppearanceHistory, before.Assistant.AppearanceHistory) {
		t.Fatalf("name-only save changed history: %v", err)
	}
	// A replay after a lost response must not append history or increment revision.
	replay, err := s.SaveAssistant(ctx, in)
	if err != nil || !reflect.DeepEqual(replay, current) {
		t.Fatalf("replayed save: %v", err)
	}
	stale := in
	stale.Appearance = maps.Clone(in.Appearance)
	stale.Appearance["palette"] = "coral"
	if _, err := s.SaveAssistant(ctx, stale); !errors.Is(err, store.ErrAssistantConflict) {
		t.Fatalf("stale write: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	restored, err := s.GetAssistant(ctx)
	if err != nil || !reflect.DeepEqual(restored, current) {
		t.Fatalf("restart lost history: %v", err)
	}
	if err := s.DeleteAssistant(ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM assistant_appearance_favorites`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("favorites survived deletion: %d %v", count, err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM assistant_appearance_history`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("history survived deletion: %d %v", count, err)
	}
}

func TestAssistantAppearanceHistoryMigrationFrom56(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	before, err := s.SaveAssistant(ctx, assistantInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DROP TABLE assistant_appearance_history; DROP TABLE assistant_appearance_favorites`); err != nil {
		t.Fatal(err)
	}
	if err := setSchemaVersion(s.db, 56); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	after, err := s.GetAssistant(ctx)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("migration changed existing profile: %v", err)
	}
}

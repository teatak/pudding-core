package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func assistantInput(revision int64) store.SaveAssistantInput {
	return store.SaveAssistantInput{ExpectedRevision: &revision, Name: "小布", UserName: "主人", Appearance: store.AssistantAppearance{"version": 1, "palette": "mint", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "none", "eyewear": "none", "faceSticker": "none"}, CommunicationPreference: "简短回答"}
}

func TestAssistantAppearanceMigrationFrom55PreservesConfiguration(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	initial := assistantInput(0)
	if _, err := s.SaveAssistant(ctx, initial); err != nil {
		t.Fatal(err)
	}
	old, err := s.OpenAssistant(ctx, "小布", "mock", "mock", initial.Appearance)
	if err != nil {
		t.Fatal(err)
	}
	// Include a client extension so migration cannot silently reconstruct known parts only.
	if _, err := s.db.Exec(`UPDATE assistants SET appearance=json_set(json_remove(appearance,'$.version'),'$.extension',json('{"custom":true}'))`); err != nil {
		t.Fatal(err)
	}
	if err := setSchemaVersion(s.db, 55); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetAssistant(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := maps.Clone(initial.Appearance)
	want["extension"] = map[string]any{"custom": true}
	if !got.Assistant.Appearance.Equal(want) || got.Assistant.Revision != old.Assistant.Revision || got.Assistant.SessionID != old.Assistant.SessionID || got.Assistant.Name != old.Assistant.Name || len(got.Memories) != len(old.Memories) {
		t.Fatalf("migration lost profile: %+v", got)
	}
	// Restart again without rewriting version or appearance.
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	read, err := again.GetAssistant(ctx)
	if err != nil || !read.Assistant.Appearance.Equal(want) {
		t.Fatalf("restart lost config: %+v %v", read, err)
	}
}

func TestAssistantPermanentDeletion(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	profile, err := s.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil {
		t.Fatal(err)
	}
	id := profile.Assistant.SessionID
	if _, err := s.SaveAssistant(ctx, assistantInput(1)); err != nil {
		t.Fatal(err)
	}
	beginTestTurn(t, s, id, "assistant-delete-turn", "assistant-delete-message", "assistant-delete-client")
	if _, err := s.SaveAssistantMemory(ctx, id, store.AssistantMemoryWrite{Content: "喜欢喝茶", SourceTurnID: "assistant-delete-turn"}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateChildSession(ctx, id, &store.Session{ID: "assistant-child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "ordinary-kept")
	if err := s.PrepareAssistantDeletion(ctx, "ordinary-kept"); !errors.Is(err, store.ErrAssistantToolUnavailable) {
		t.Fatalf("ordinary admission: %v", err)
	}
	if err := s.PrepareAssistantDeletion(ctx, id); err != nil {
		t.Fatal(err)
	}
	// Profile, memories and conversation must roll back together on a write failure.
	if _, err := s.db.Exec(`CREATE TRIGGER reject_assistant_delete BEFORE DELETE ON assistants BEGIN SELECT RAISE(ABORT,'test failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAssistant(ctx); err == nil {
		t.Fatal("expected transaction failure")
	}
	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM sessions WHERE id=?`, id).Scan(&count); err != nil || count != 1 {
		t.Fatalf("conversation lost on rollback: %d %v", count, err)
	}
	if _, err := s.db.Exec(`DROP TRIGGER reject_assistant_delete`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAssistant(ctx); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"assistants", "assistant_sessions", "assistant_memories", "assistant_memory_terms", "messages", "turns", "session_children"} {
		if err := s.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s retained rows: %d %v", table, count, err)
		}
	}
	for _, deleted := range []string{id, "assistant-child"} {
		if _, err := s.GetSession(ctx, deleted); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("session remains: %s %v", deleted, err)
		}
		if _, err := s.RestoreSession(ctx, deleted); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("deleted session restored: %s %v", deleted, err)
		}
	}
	if _, err := s.GetSession(ctx, "ordinary-kept"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAssistant(ctx); err != nil {
		t.Fatal("retry", err)
	}
	fresh, err := s.OpenAssistant(ctx, "Puddy", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Assistant.SessionID == id || fresh.Assistant.Name != "" || fresh.Assistant.UserName != "" || fresh.Assistant.Appearance["palette"] != "original" || len(fresh.Memories) != 0 {
		t.Fatalf("defaults not reset: %+v", fresh)
	}
}

func TestAssistantDeletionWithoutConversation(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveAssistant(ctx, assistantInput(0)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteAssistant(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAssistant(ctx)
	if err != nil || got.Assistant != nil || len(got.Memories) != 0 {
		t.Fatalf("delete profile: %+v %v", got, err)
	}
}

func TestAssistantSingletonAndPersistence(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	initial, err := s.GetAssistant(ctx)
	if err != nil || initial.Assistant != nil || len(initial.Memories) != 0 {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.SaveAssistant(ctx, assistantInput(0))
			if err != nil || got.Assistant.ID != store.PersonalAssistantID || got.Assistant.Revision != 1 {
				t.Errorf("initialize: %+v %v", got, err)
			}
		}()
	}
	wg.Wait()
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM assistants`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count=%d: %v", count, err)
	}
	if _, err := s.db.Exec(`INSERT INTO assistants SELECT 'second',name,user_name,appearance,revision,created_at,updated_at FROM assistants`); err == nil {
		t.Fatal("database allowed a second identity")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetAssistant(ctx)
	if err != nil || got.Assistant.Name != "小布" || got.Assistant.UserName != "主人" || !got.Assistant.Appearance.Equal(assistantInput(0).Appearance) || len(got.Memories) != 1 || got.Memories[0].Content != "简短回答" || got.Memories[0].Source != "user_settings" {
		t.Fatalf("reopened: %+v %v", got, err)
	}
}

func TestAssistantStaleSaveDoesNotOverwrite(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveAssistant(ctx, assistantInput(0)); err != nil {
		t.Fatal(err)
	}
	updated := assistantInput(1)
	updated.Name = "小布"
	updated.CommunicationPreference = ""
	got, err := s.SaveAssistant(ctx, updated)
	if err != nil || got.Assistant.Revision != 2 || len(got.Memories) != 0 {
		t.Fatalf("update: %+v %v", got, err)
	}
	if _, err := s.SaveAssistant(ctx, assistantInput(0)); !errors.Is(err, store.ErrAssistantConflict) {
		t.Fatalf("late setup: %v", err)
	}
	if _, err := s.SaveAssistant(ctx, assistantInput(1)); !errors.Is(err, store.ErrAssistantConflict) {
		t.Fatalf("stale edit: %v", err)
	}
	got, err = s.SaveAssistant(ctx, updated)
	if err != nil || got.Assistant.Name != "小布" || got.Assistant.Revision != 2 {
		t.Fatalf("lost response retry: %+v %v", got, err)
	}
}

func TestAssistantSetupRollsBackWhenMemoryWriteFails(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec(`CREATE TRIGGER fail_assistant_memory BEFORE INSERT ON assistant_memories BEGIN SELECT RAISE(ABORT,'memory unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAssistant(ctx, assistantInput(0)); err == nil {
		t.Fatal("failed memory write reported success")
	}
	got, err := s.GetAssistant(ctx)
	if err != nil || got.Assistant != nil || len(got.Memories) != 0 {
		t.Fatalf("partial setup: %+v %v", got, err)
	}
}

func TestAssistantMigrationFrom50PreservesSessions(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, s, "existing")
	if _, err := s.db.Exec(`DROP TABLE assistant_memory_terms; DROP TABLE assistant_memories; DROP TABLE assistants;`); err != nil {
		t.Fatal(err)
	}
	if err := setSchemaVersion(s.db, 50); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.GetSession(ctx, "existing"); err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetAssistant(ctx)
	if err != nil || got.Assistant != nil {
		t.Fatalf("migrated: %+v %v", got, err)
	}
	if _, err := reopened.SaveAssistant(ctx, assistantInput(0)); err != nil {
		t.Fatal(err)
	}
}

func TestOpenAssistantDefaultsAndOneConversation(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	initial, err := s.OpenAssistant(ctx, "Puddy", "", "", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil || initial.Assistant.Name != "" || initial.Assistant.SessionID != "" || initial.Assistant.Appearance["eyewear"] != "classic" {
		t.Fatalf("default without model: %+v %v", initial, err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- got.Assistant.SessionID
		}()
	}
	wg.Wait()
	close(ids)
	var id string
	for candidate := range ids {
		if candidate == "" || (id != "" && id != candidate) {
			t.Fatalf("duplicate conversation: %q %q", id, candidate)
		}
		id = candidate
	}
	if listed, err := s.ListSessions(ctx); err != nil || len(listed) != 0 {
		t.Fatalf("assistant in recent list: %+v %v", listed, err)
	}
	if _, err := s.ArchiveSession(ctx, id); !errors.Is(err, store.ErrAssistantConversationProtected) {
		t.Fatalf("archive assistant: %v", err)
	}
	if err := s.PrepareSessionDeletion(ctx, id); !errors.Is(err, store.ErrAssistantConversationProtected) {
		t.Fatalf("prepare assistant deletion: %v", err)
	}
	if err := s.DeleteSession(ctx, id); !errors.Is(err, store.ErrAssistantConversationProtected) {
		t.Fatalf("delete assistant: %v", err)
	}
	input := assistantInput(1)
	input.Name = "自定义名字"
	input.Appearance["eyewear"] = "goggles"
	if _, err := s.SaveAssistant(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.OpenAssistant(ctx, "Puddy", "different", "different", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil || got.Assistant.Name != input.Name || got.Assistant.SessionID != id || !got.Assistant.Appearance.Equal(input.Appearance) {
		t.Fatalf("reopen reset profile: %+v %v", got, err)
	}
	session, err := reopened.GetSession(ctx, id)
	if err != nil || session.Provider != "mock" || session.Model != "mock" {
		t.Fatalf("reopen reset model: %+v %v", session, err)
	}
}

func TestAssistantMigrationFromBothV51Layouts(t *testing.T) {
	for _, legacy := range []bool{true, false} {
		t.Run(fmt.Sprint("palette=", legacy), func(t *testing.T) {
			s, path := openTestStore(t)
			ctx := context.Background()
			input := assistantInput(0)
			if legacy {
				input.Appearance["arms"] = "classic"
				input.Appearance["faceSticker"] = "blush"
			}
			if _, err := s.SaveAssistant(ctx, input); err != nil {
				t.Fatal(err)
			}
			createTestSession(t, s, "existing")
			if _, err := s.db.Exec(`DROP TABLE assistant_sessions`); err != nil {
				t.Fatal(err)
			}
			if legacy {
				if _, err := s.db.Exec(`UPDATE assistants SET appearance=json_extract(appearance,'$.palette'); ALTER TABLE assistants RENAME COLUMN appearance TO palette;`); err != nil {
					t.Fatal(err)
				}
			}
			restoreAssistantMemoriesV53(t, s)
			if err := setSchemaVersion(s.db, 51); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			input.Appearance["eyewear"] = "classic"
			got, err := reopened.GetAssistant(ctx)
			if err != nil || got.Assistant.Name != input.Name || !got.Assistant.Appearance.Equal(input.Appearance) || got.Assistant.Revision != 2 || len(got.Memories) != 1 || got.Memories[0].Content != input.CommunicationPreference {
				t.Fatalf("migration changed data: %+v %v", got, err)
			}
			if _, err := reopened.GetSession(ctx, "existing"); err != nil {
				t.Fatal(err)
			}
			if _, err := reopened.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssistantMaskMigrationFrom52(t *testing.T) {
	for _, oldEyewear := range []string{"none", "goggles"} {
		t.Run(oldEyewear, func(t *testing.T) {
			s, path := openTestStore(t)
			ctx := context.Background()
			input := assistantInput(0)
			input.Appearance["eyewear"] = oldEyewear
			if _, err := s.SaveAssistant(ctx, input); err != nil {
				t.Fatal(err)
			}
			old, err := s.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
			if err != nil {
				t.Fatal(err)
			}
			restoreAssistantMemoriesV53(t, s)
			if err := setSchemaVersion(s.db, 52); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			got, err := reopened.GetAssistant(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := maps.Clone(input.Appearance)
			wantRevision := int64(1)
			if oldEyewear == "none" {
				want["eyewear"] = "classic"
				wantRevision++
			}
			if !got.Assistant.Appearance.Equal(want) || got.Assistant.Revision != wantRevision || got.Assistant.Name != old.Assistant.Name || got.Assistant.SessionID != old.Assistant.SessionID || len(got.Memories) != 1 || got.Memories[0].Content != input.CommunicationPreference {
				t.Fatalf("migration: %+v", got)
			}
			if oldEyewear == "none" {
				stale := input
				stale.ExpectedRevision = &old.Assistant.Revision
				if _, err := reopened.SaveAssistant(ctx, stale); !errors.Is(err, store.ErrAssistantConflict) {
					t.Fatalf("stale edit: %v", err)
				}
			}
			input.ExpectedRevision = &got.Assistant.Revision
			input.Appearance["eyewear"] = "none"
			if _, err := reopened.SaveAssistant(ctx, input); err != nil {
				t.Fatal(err)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			again, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			persisted, err := again.GetAssistant(ctx)
			if err != nil || persisted.Assistant.Appearance["eyewear"] != "none" {
				t.Fatalf("new bare-face choice was remapped: %+v %v", persisted, err)
			}
		})
	}
}

func TestAssistantPaletteMigrationFrom54(t *testing.T) {
	for _, palette := range []string{"christmas", "apricot", "crimson"} {
		t.Run(palette, func(t *testing.T) {
			s, path := openTestStore(t)
			ctx := context.Background()
			input := assistantInput(0)
			if _, err := s.SaveAssistant(ctx, input); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE assistants SET appearance=json_set(appearance,'$.palette',?)`, palette); err != nil {
				t.Fatal(err)
			}
			before, err := s.GetAssistant(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if err := setSchemaVersion(s.db, 54); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			got, err := reopened.GetAssistant(ctx)
			if err != nil {
				t.Fatal(err)
			}
			want := before.Assistant.Appearance
			revision := before.Assistant.Revision
			if palette == "christmas" {
				want["palette"] = "apricot"
				revision++
				if !got.Assistant.UpdatedAt.After(before.Assistant.UpdatedAt) {
					t.Fatal("migration did not advance timestamp")
				}
			}
			if !got.Assistant.Appearance.Equal(want) || got.Assistant.Revision != revision || got.Assistant.Name != before.Assistant.Name || got.Assistant.UserName != before.Assistant.UserName || len(got.Memories) != len(before.Memories) {
				t.Fatalf("unexpected palette migration: %+v", got)
			}
			input.Appearance = got.Assistant.Appearance
			input.ExpectedRevision = &got.Assistant.Revision
			if _, err := reopened.SaveAssistant(ctx, input); err != nil {
				t.Fatal(err)
			}
		})
	}
}

package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func assistantInput(revision int64) store.SaveAssistantInput {
	return store.SaveAssistantInput{ExpectedRevision: &revision, Name: "小布", UserName: "主人", Appearance: store.AssistantAppearance{Palette: "mint", Body: "classic", Eyes: "classic", Mouth: "classic", Headwear: "antenna", Arms: "none", Eyewear: "none", FaceSticker: "none"}, CommunicationPreference: "简短回答"}
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
	if err != nil || got.Assistant.Name != "小布" || got.Assistant.UserName != "主人" || got.Assistant.Appearance != assistantInput(0).Appearance || len(got.Memories) != 1 || got.Memories[0].Content != "简短回答" || got.Memories[0].Source != "user_settings" {
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
	if _, err := s.db.Exec(`DROP TABLE assistant_memories; DROP TABLE assistants;`); err != nil {
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
	initial, err := s.OpenAssistant(ctx, "Puddy", "", "")
	if err != nil || initial.Assistant.Name != "Puddy" || initial.Assistant.SessionID != "" || initial.Assistant.Appearance.Eyewear != "classic" {
		t.Fatalf("default without model: %+v %v", initial, err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := s.OpenAssistant(ctx, "小布", "mock", "mock")
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
	input.Appearance.Eyewear = "goggles"
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
	got, err := reopened.OpenAssistant(ctx, "Puddy", "different", "different")
	if err != nil || got.Assistant.Name != input.Name || got.Assistant.SessionID != id || got.Assistant.Appearance != input.Appearance {
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
				input.Appearance.Arms = "classic"
				input.Appearance.FaceSticker = "blush"
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
			input.Appearance.Eyewear = "classic"
			got, err := reopened.GetAssistant(ctx)
			if err != nil || got.Assistant.Name != input.Name || got.Assistant.Appearance != input.Appearance || got.Assistant.Revision != 2 || len(got.Memories) != 1 || got.Memories[0].Content != input.CommunicationPreference {
				t.Fatalf("migration changed data: %+v %v", got, err)
			}
			if _, err := reopened.GetSession(ctx, "existing"); err != nil {
				t.Fatal(err)
			}
			if _, err := reopened.OpenAssistant(ctx, "小布", "mock", "mock"); err != nil {
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
			input.Appearance.Eyewear = oldEyewear
			if _, err := s.SaveAssistant(ctx, input); err != nil {
				t.Fatal(err)
			}
			old, err := s.OpenAssistant(ctx, "小布", "mock", "mock")
			if err != nil {
				t.Fatal(err)
			}
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
			want := input.Appearance
			wantRevision := int64(1)
			if oldEyewear == "none" {
				want.Eyewear = "classic"
				wantRevision++
			}
			if got.Assistant.Appearance != want || got.Assistant.Revision != wantRevision || got.Assistant.Name != old.Assistant.Name || got.Assistant.SessionID != old.Assistant.SessionID || len(got.Memories) != 1 || got.Memories[0].Content != input.CommunicationPreference {
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
			input.Appearance.Eyewear = "none"
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
			if err != nil || persisted.Assistant.Appearance.Eyewear != "none" {
				t.Fatalf("new bare-face choice was remapped: %+v %v", persisted, err)
			}
		})
	}
}

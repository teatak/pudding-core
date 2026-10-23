package sqlitestore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func restoreAssistantMemoriesV53(t *testing.T, s *Store) {
	t.Helper()
	_, err := s.db.Exec(`DROP TABLE assistant_memory_terms;
ALTER TABLE assistant_memories DROP COLUMN project_id;
ALTER TABLE assistant_memories DROP COLUMN revision;
ALTER TABLE assistant_memories DROP COLUMN source_session_id;
ALTER TABLE assistant_memories DROP COLUMN source_turn_id;`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAssistantMemoriesLifecycleAndIsolation(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	profile, err := s.OpenAssistant(ctx, "小布", "mock", "mock")
	if err != nil {
		t.Fatal(err)
	}
	sid := profile.Assistant.SessionID
	beginTestTurn(t, s, sid, "source-turn", "source-message", "source-client")
	for _, id := range []string{"project-a", "project-b"} {
		if err := s.CreateProject(ctx, &store.Project{ID: id, Name: id}); err != nil {
			t.Fatal(err)
		}
	}
	save := func(content, project string) *store.AssistantMemory {
		t.Helper()
		m, err := s.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{Content: content, ProjectID: project, SourceTurnID: "source-turn"})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	personal := save("用户居住在杭州", "")
	project := save("杭州项目发布日期是周五", "project-a")
	save("杭州项目使用 TypeScript", "project-b")
	if personal.Revision != 1 || personal.Source != "conversation" || personal.SourceSessionID != sid || personal.SourceTurnID != "source-turn" {
		t.Fatalf("missing provenance: %+v", personal)
	}
	if duplicate := save(personal.Content, ""); !reflect.DeepEqual(personal, duplicate) {
		t.Fatal("retry created a duplicate")
	}
	search := func(query, projectID string) []store.AssistantMemory {
		t.Helper()
		ms, err := s.SearchAssistantMemories(ctx, sid, store.AssistantMemoryQuery{Query: query, ProjectID: projectID, Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		return ms
	}
	if found := search("杭州", ""); len(found) != 1 || found[0].ID != personal.ID {
		t.Fatalf("personal scope: %+v", found)
	}
	if found := search("杭州", "project-a"); len(found) != 1 || found[0].ID != project.ID {
		t.Fatalf("project scope: %+v", found)
	}
	if found := search("TYPESCRIPT", "project-b"); len(found) != 1 || found[0].Content != "杭州项目使用 TypeScript" {
		t.Fatalf("English term normalization: %+v", found)
	}
	if found := search("!!!", ""); len(found) != 0 {
		t.Fatal("punctuation matched all memories")
	}
	updated, err := s.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{ID: personal.ID, ExpectedRevision: 1, Content: "用户现在居住在上海", SourceTurnID: "source-turn"})
	if err != nil || updated.Revision != 2 || updated.CreatedAt != personal.CreatedAt {
		t.Fatalf("update: %+v %v", updated, err)
	}
	if len(search("杭州", "")) != 0 || len(search("上海", "")) != 1 {
		t.Fatal("search index retained stale content")
	}
	_, err = s.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{ID: personal.ID, ExpectedRevision: 1, Content: "过期信息", SourceTurnID: "source-turn"})
	if !errors.Is(err, store.ErrAssistantMemoryConflict) {
		t.Fatal("stale update allowed", err)
	}
	if err := s.DeleteAssistantMemory(ctx, sid, personal.ID, 1); !errors.Is(err, store.ErrAssistantMemoryConflict) {
		t.Fatal("stale delete allowed", err)
	}
	createTestSession(t, s, "ordinary")
	if err := s.CreateChildSession(ctx, sid, &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for _, foreign := range []string{"ordinary", "child"} {
		if _, err := s.SearchAssistantMemories(ctx, foreign, store.AssistantMemoryQuery{Limit: 20}); !errors.Is(err, store.ErrAssistantToolUnavailable) {
			t.Fatal("foreign read", err)
		}
		if _, err := s.SaveAssistantMemory(ctx, foreign, store.AssistantMemoryWrite{Content: "wrong", SourceTurnID: "source-turn"}); !errors.Is(err, store.ErrAssistantToolUnavailable) {
			t.Fatal("foreign write", err)
		}
		if err := s.DeleteAssistantMemory(ctx, foreign, personal.ID, 2); !errors.Is(err, store.ErrAssistantToolUnavailable) {
			t.Fatal("foreign delete", err)
		}
	}
	if _, err := s.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{Content: "invalid source", SourceTurnID: "missing"}); !errors.Is(err, store.ErrInvalidAssistantMemory) {
		t.Fatal("untracked write", err)
	}
	if _, err := s.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{Content: strings.Repeat("字", 2001), SourceTurnID: "source-turn"}); !errors.Is(err, store.ErrInvalidAssistantMemory) {
		t.Fatal("oversized write", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if found := search("上海", ""); len(found) != 1 || !reflect.DeepEqual(found[0], *updated) {
		t.Fatal("memory lost on restart", found)
	}
	if err := s.DeleteAssistantMemory(ctx, sid, personal.ID, 2); err != nil {
		t.Fatal(err)
	}
	if len(search("上海", "")) != 0 {
		t.Fatal("forgotten fact remained searchable")
	}
	var terms int
	if err := s.db.QueryRow(`SELECT count(*) FROM assistant_memory_terms WHERE memory_id=?`, personal.ID).Scan(&terms); err != nil || terms != 0 {
		t.Fatal("orphan index", terms, err)
	}
	if _, err := s.GetConversationTurn(ctx, sid, "source-turn"); err != nil {
		t.Fatal("forget removed source conversation", err)
	}
}

func TestAssistantPreferenceMemorySharesSettingsAndRevision(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	p, err := s.OpenAssistant(ctx, "小布", "mock", "mock")
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Assistant.SessionID
	beginTestTurn(t, s, sid, "turn", "msg", "client")
	m, err := s.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{ID: communicationPreferenceID, Content: "回答请简短", SourceTurnID: "turn"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.GetAssistant(ctx)
	if err != nil || current.Assistant.Revision != p.Assistant.Revision+1 || len(current.Memories) != 1 || current.Memories[0].Content != m.Content {
		t.Fatal("settings not synchronized", current, err)
	}
	stale := store.SaveAssistantInput{ExpectedRevision: &p.Assistant.Revision, Name: "stale name", Appearance: p.Assistant.Appearance}
	if _, err := s.SaveAssistant(ctx, stale); !errors.Is(err, store.ErrAssistantConflict) {
		t.Fatal("stale form erased memory", err)
	}
	stale.ExpectedRevision = &current.Assistant.Revision
	stale.CommunicationPreference = "先给结论"
	updated, err := s.SaveAssistant(ctx, stale)
	if err != nil || updated.Memories[0].Revision != 2 || updated.Memories[0].Source != "user_settings" || updated.Memories[0].SourceTurnID != "" {
		t.Fatal("settings edit did not replace memory", updated, err)
	}
	if err := s.DeleteAssistantMemory(ctx, sid, m.ID, 1); !errors.Is(err, store.ErrAssistantMemoryConflict) {
		t.Fatal("stale tool deleted new settings", err)
	}
	if err := s.DeleteAssistantMemory(ctx, sid, m.ID, 2); err != nil {
		t.Fatal(err)
	}
	deleted, err := s.GetAssistant(ctx)
	if err != nil || len(deleted.Memories) != 0 || deleted.Assistant.Revision != updated.Assistant.Revision+1 {
		t.Fatal("preference deletion not synced", deleted, err)
	}
}

func TestAssistantMemoryIndexFailureRollsBack(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	p, err := s.OpenAssistant(ctx, "小布", "mock", "mock")
	if err != nil {
		t.Fatal(err)
	}
	beginTestTurn(t, s, p.Assistant.SessionID, "turn", "msg", "client")
	if _, err := s.db.Exec(`CREATE TRIGGER fail_memory_index BEFORE INSERT ON assistant_memory_terms BEGIN SELECT RAISE(ABORT,'index unavailable'); END;`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveAssistantMemory(ctx, p.Assistant.SessionID, store.AssistantMemoryWrite{Content: "Memory must not partially persist", SourceTurnID: "turn"}); err == nil {
		t.Fatal("failed index reported success")
	}
	current, err := s.GetAssistant(ctx)
	if err != nil || !reflect.DeepEqual(p, current) {
		t.Fatal("partial memory write", current, err)
	}
}

func TestAssistantMemoryMigrationFrom53(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	if _, err := s.SaveAssistant(ctx, assistantInput(0)); err != nil {
		t.Fatal(err)
	}
	p, err := s.OpenAssistant(ctx, "小布", "mock", "mock")
	if err != nil {
		t.Fatal(err)
	}
	restoreAssistantMemoriesV53(t, s)
	if err := setSchemaVersion(s.db, 53); err != nil {
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
	memories, err := s.SearchAssistantMemories(ctx, p.Assistant.SessionID, store.AssistantMemoryQuery{Query: "简短回答", Limit: 20})
	if err != nil || len(memories) != 1 || memories[0].Revision != 1 || memories[0].Source != "user_settings" || memories[0].Content != p.Memories[0].Content {
		t.Fatal("migration lost preferences or index", memories, err)
	}
}

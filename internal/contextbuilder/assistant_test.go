package contextbuilder

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestAssistantProfileIsCurrentAndLimitedToItsConversation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "assistant.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	opened, err := db.OpenAssistant(ctx, "Puddy", "fixture", "first")
	if err != nil {
		t.Fatal(err)
	}
	sid := opened.Assistant.SessionID
	if err := db.CreateSession(ctx, &store.Session{ID: "ordinary", Provider: "fixture", Model: "first"}); err != nil {
		t.Fatal(err)
	}
	b := New(db, nil)
	build := func(session, model, mode string) provider.Request {
		t.Helper()
		req, err := b.Build(ctx, session, model, mode)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	for _, mode := range []string{"chat", "work", "code"} {
		if !strings.Contains(build(sid, "first", mode).System, "Chinese name is 小布") {
			t.Fatal("default identity missing")
		}
		if strings.Contains(build("ordinary", "first", mode).System, "## Personal Assistant Profile") {
			t.Fatal("assistant identity leaked to ordinary conversation")
		}
	}
	input := store.SaveAssistantInput{ExpectedRevision: &opened.Assistant.Revision, Name: "小星", UserName: "杨同学",
		Appearance: opened.Assistant.Appearance, CommunicationPreference: "用中文，先给结论"}
	input.Appearance.Headwear = "hat"
	input.Appearance.Eyewear = "goggles"
	saved, err := db.SaveAssistant(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	req := build(sid, "second", "chat")
	for _, want := range []string{`"name":"小星"`, `"user_name":"杨同学"`, "用中文，先给结论", "red hat", "gold-rimmed goggles"} {
		if !strings.Contains(req.System, want) {
			t.Fatalf("updated profile missing %q", want)
		}
		if strings.Contains(build("ordinary", "second", "chat").System, want) {
			t.Fatalf("private value leaked: %s", want)
		}
	}
	if len(req.Messages) != 0 {
		t.Fatal("profile was written as conversation history")
	}

	// Summary/budget projection must use the same fresh profile as a live request,
	// even when historical text confidently claims a different identity.
	history := []*store.Message{{ID: "summary", Role: store.RoleSummary, Parts: store.TextPart("My name is Pudding; call me 布丁.")}}
	projected, err := b.BuildForProviderWithHistory(ctx, sid, "fixture", "second", "chat", nil, history, provider.ModelConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if projected.System != req.System {
		t.Fatal("summary projection lost the current profile")
	}

	input.ExpectedRevision = &saved.Assistant.Revision
	input.Name, input.UserName, input.CommunicationPreference = "", "", ""
	if _, err := db.SaveAssistant(ctx, input); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b = New(db, nil)
	reset := build(sid, "third", "chat").System
	for _, stale := range []string{"小星", "杨同学", "用中文，先给结论", `"communication_preference"`} {
		if strings.Contains(reset, stale) {
			t.Fatalf("removed profile value survived restart: %s", stale)
		}
	}
	if !strings.Contains(reset, "Chinese name is 小布") || !strings.Contains(reset, "English name is Puddy") {
		t.Fatal("empty name did not restore defaults")
	}
}

func TestAssistantMemoryRetrievalSurvivesRestartAndProjection(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "memories.db")
	db, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	p, err := db.OpenAssistant(ctx, "小布", "mock", "mock")
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Assistant.SessionID
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "source", UserMessageID: "source-msg", ClientMessageID: "source-client", UserText: "我居住在杭州，喜欢咖啡"}); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"用户居住在杭州", "用户喜欢咖啡"} {
		if _, err := db.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{Content: content, SourceTurnID: "source"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreateProject(ctx, &store.Project{ID: "project", Name: "Private project"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.SaveAssistantMemory(ctx, sid, store.AssistantMemoryWrite{Content: "杭州项目的秘密代号是 GREEN", ProjectID: "project", SourceTurnID: "source"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ordinary", "child"} {
		sess := &store.Session{ID: id, Provider: "mock", Model: "mock"}
		if id == "child" {
			err = db.CreateChildSession(ctx, sid, sess)
		} else {
			err = db.CreateSession(ctx, sess)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	b := New(db, nil)
	history := []*store.Message{{Role: store.RoleSummary, Parts: store.TextPart("Earlier conversation was compacted.")}, {Role: store.RoleUser, Parts: store.TextPart("杭州周末可以做些什么？")}}
	for _, mode := range []string{"chat", "work", "code"} {
		for _, id := range []string{sid, "ordinary", "child"} {
			req, err := b.BuildForProviderWithHistory(ctx, id, "mock", "another-model", mode, nil, history, provider.ModelConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(req.System, "用户居住在杭州") != (id == sid) {
				t.Fatal("retrieval scope or persistence", id, mode, req.System)
			}
			for _, unrelated := range []string{"用户喜欢咖啡", "GREEN"} {
				if strings.Contains(req.System, unrelated) {
					t.Fatal("unrelated memory leaked", unrelated)
				}
			}
		}
	}
}

type failedAssistantStore struct {
	store.Store
	err error
}

func (s failedAssistantStore) GetAssistant(context.Context) (*store.AssistantSnapshot, error) {
	return nil, s.err
}

func TestAssistantProfileReadFailureIsNotHidden(t *testing.T) {
	db, err := sqlitestore.Open(filepath.Join(t.TempDir(), "assistant.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	opened, err := db.OpenAssistant(ctx, "Puddy", "fixture", "first")
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("profile unavailable")
	_, err = New(failedAssistantStore{Store: db, err: want}, nil).Build(ctx, opened.Assistant.SessionID, "first", "chat")
	if !errors.Is(err, want) {
		t.Fatalf("profile failure was hidden: %v", err)
	}
}

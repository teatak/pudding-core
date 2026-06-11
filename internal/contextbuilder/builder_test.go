package contextbuilder

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestBuildUsesSettingsSystemPrompt(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "hi",
	}); err != nil {
		t.Fatal(err)
	}
	b := New(ms)

	req, err := b.Build(ctx, "s1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if req.System != defaultSystemPrompt {
		t.Fatalf("want default system prompt, got %q", req.System)
	}

	if err := ms.SetSettings(ctx, map[string]string{SettingSystemPrompt: "你是布丁"}); err != nil {
		t.Fatal(err)
	}
	req, err = b.Build(ctx, "s1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if req.System != "你是布丁" {
		t.Fatalf("want settings system prompt, got %q", req.System)
	}
	if len(req.Messages) != 1 || req.Messages[0].Text != "hi" {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
}

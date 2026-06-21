package contextbuilder

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestBuildUsesSettingsSystemPrompt(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "hi",
	}); err != nil {
		t.Fatal(err)
	}
	b := New(ms, ms)

	req, err := b.Build(ctx, "s1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if req.System != defaultSystemPrompt {
		t.Fatalf("want default system prompt, got %q", req.System)
	}

	if err := ms.SetSettings(ctx, map[string]string{store.SettingSystemPrompt: "你是布丁"}); err != nil {
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

func TestBuildStripsThoughtParts(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "hi",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "t1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartThought, Text: "private reasoning"},
			{Type: store.ContentPartText, Text: "answer"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, ms).Build(ctx, "s1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	assistant := req.Messages[1]
	if assistant.Text != "answer" {
		t.Fatalf("text column should remain final answer: %+v", assistant)
	}
	if len(assistant.Parts) != 1 || assistant.Parts[0].Type != provider.PartText || assistant.Parts[0].Text != "answer" {
		t.Fatalf("thought must be stripped from provider context: %+v", assistant.Parts)
	}
}

func TestBuildKeepsToolParts(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "time",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "t1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "call_1", Name: "builtin_time_get_current", Args: []byte(`{"timezone":"Asia/Singapore"}`)},
			{Type: store.ContentPartToolResult, CallID: "call_1", Name: "builtin_time_get_current", Ok: true, Content: `{"iso":"now"}`},
			{Type: store.ContentPartText, Text: "done"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, ms).Build(ctx, "s1", "m")
	if err != nil {
		t.Fatal(err)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("unexpected messages: %+v", req.Messages)
	}
	parts := req.Messages[1].Parts
	if len(parts) != 3 || parts[0].Type != provider.PartToolUse || parts[1].Type != provider.PartToolResult || parts[2].Type != provider.PartText {
		t.Fatalf("unexpected parts: %+v", parts)
	}
	if parts[1].CallID != "call_1" || parts[1].Name != "builtin_time_get_current" || !parts[1].Ok || parts[1].Content == "" {
		t.Fatalf("tool result not preserved: %+v", parts[1])
	}
}

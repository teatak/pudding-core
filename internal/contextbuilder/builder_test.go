package contextbuilder

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestBuildUsesCoreAndUserPrompt(t *testing.T) {
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
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "pudding.md"), []byte("请尽量简短。"), 0o600); err != nil {
		t.Fatal(err)
	}
	b := New(ms, prompt.NewLoader(home))

	req, err := b.Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(req.System, "You are Pudding") || !strings.Contains(req.System, "请尽量简短。") {
		t.Fatalf("unexpected system prompt: %q", req.System)
	}

	if err := ms.SetSettings(ctx, map[string]string{"system_prompt": "你是布丁"}); err != nil {
		t.Fatal(err)
	}
	req, err = b.Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(req.System, "你是布丁") {
		t.Fatalf("settings system_prompt must not affect contextbuilder prompt: %q", req.System)
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

	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
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

	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
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

func TestBuildFiltersToolPartsOutsideMode(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1",
		ClientMessageID: "c1", UserText: "weather",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID: "t1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "call_1", Name: tool.WebSearch, Args: []byte(`{"query":"北京天气"}`)},
			{Type: store.ContentPartToolResult, CallID: "call_1", Name: tool.WebSearch, Ok: true, Content: `{"answer":"sunny"}`},
			{Type: store.ContentPartText, Text: "sunny"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	chatReq, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	chatParts := chatReq.Messages[1].Parts
	if len(chatParts) != 1 || chatParts[0].Type != provider.PartText || chatParts[0].Text != "sunny" {
		t.Fatalf("chat context should hide research tool history: %+v", chatParts)
	}

	researchReq, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeResearch))
	if err != nil {
		t.Fatal(err)
	}
	researchParts := researchReq.Messages[1].Parts
	if len(researchParts) != 3 || researchParts[0].Type != provider.PartToolUse || researchParts[1].Type != provider.PartToolResult || researchParts[2].Type != provider.PartText {
		t.Fatalf("research context should keep research tool history: %+v", researchParts)
	}
}

func TestBuildUsesLatestCompactBoundary(t *testing.T) {
	ms := memstore.New()
	ctx := context.Background()
	if err := ms.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m_old",
		ClientMessageID: "c_old", UserText: "old user",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "t1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("old assistant"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t2", UserMessageID: "m_tail",
		ClientMessageID: "c_tail", UserText: "tail user",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "t2",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("tail assistant"),
	}); err != nil {
		t.Fatal(err)
	}
	beforeCompact, err := ms.ListMessages(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	oldAssistantID := testMessageIDByText(t, beforeCompact, "old assistant")
	tailAssistantID := testMessageIDByText(t, beforeCompact, "tail assistant")
	if _, err := ms.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{
		SessionID:       "s1",
		TurnID:          "t_compact",
		MessageID:       "m_compact",
		ClientMessageID: "compact:t_compact",
		Provider:        "mock",
		Model:           "mock",
		Text:            "summary of old history",
		Metadata:        store.CompactMessageMetadata([]string{"m_old", oldAssistantID}, []string{"m_tail", tailAssistantID}),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t3", UserMessageID: "m_after",
		ClientMessageID: "c_after", UserText: "after compact",
	}); err != nil {
		t.Fatal(err)
	}

	req, err := New(ms, nil).Build(ctx, "s1", "m", string(store.ModeChat))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(req.Messages))
	for _, msg := range req.Messages {
		got = append(got, msg.Text)
	}
	want := []string{"summary of old history", "tail user", "tail assistant", "after compact"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected compact context: got %v want %v", got, want)
	}
}

func TestSplitRecentInputTailTreatsSystemAsInputBoundary(t *testing.T) {
	msg := func(id, turn string, role store.Role) *store.Message {
		return &store.Message{ID: id, TurnID: turn, Role: role}
	}
	msgs := []*store.Message{
		msg("m_summary", "t_summary", store.RoleSummary),
		msg("m_user_old", "t_user_old", store.RoleUser),
		msg("m_assistant_old", "t_user_old", store.RoleAssistant),
		msg("m_system", "t_system", store.RoleSystem),
		msg("m_assistant_system", "t_system", store.RoleAssistant),
		msg("m_user_tail", "t_user_tail", store.RoleUser),
		msg("m_assistant_tail", "t_user_tail", store.RoleAssistant),
	}

	cold, tail := SplitRecentInputTail(msgs, 2)
	gotCold := testMessageIDs(cold)
	gotTail := testMessageIDs(tail)
	wantCold := []string{"m_summary", "m_user_old", "m_assistant_old"}
	wantTail := []string{"m_system", "m_assistant_system", "m_user_tail", "m_assistant_tail"}
	if strings.Join(gotCold, "|") != strings.Join(wantCold, "|") {
		t.Fatalf("unexpected cold messages: got %v want %v", gotCold, wantCold)
	}
	if strings.Join(gotTail, "|") != strings.Join(wantTail, "|") {
		t.Fatalf("unexpected tail messages: got %v want %v", gotTail, wantTail)
	}
}

func testMessageIDs(msgs []*store.Message) []string {
	ids := make([]string, 0, len(msgs))
	for _, msg := range msgs {
		ids = append(ids, msg.ID)
	}
	return ids
}

func testMessageIDByText(t *testing.T, msgs []*store.Message, text string) string {
	t.Helper()
	for _, msg := range msgs {
		if msg.Text == text {
			return msg.ID
		}
	}
	t.Fatalf("message text %q not found in %+v", text, msgs)
	return ""
}

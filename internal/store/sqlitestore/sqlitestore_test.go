package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/store"
)

func openTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st, path
}

func createTestSession(t *testing.T, st store.Store, id string) {
	t.Helper()
	if err := st.CreateSession(context.Background(), &store.Session{ID: id, Title: id, Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionWorkspaceDirsPersist(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	root := filepath.Join(t.TempDir(), "project")
	if err := st.CreateSession(ctx, &store.Session{
		ID:            "sess_workspace",
		Title:         "workspace",
		Provider:      "mock",
		Model:         "mock",
		WorkspaceDirs: []string{root, root + "/.", "relative"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetSession(ctx, "sess_workspace")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got.WorkspaceDirs, []string{root}) {
		t.Fatalf("workspace dirs not normalized: %+v", got.WorkspaceDirs)
	}

	other := filepath.Join(t.TempDir(), "other")
	dirs := []string{other, other}
	if _, err := st.UpdateSession(ctx, "sess_workspace", store.SessionUpdate{WorkspaceDirs: &dirs}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err = reopened.GetSession(ctx, "sess_workspace")
	if err != nil {
		t.Fatal(err)
	}
	if !sameStrings(got.WorkspaceDirs, []string{other}) {
		t.Fatalf("workspace dirs not persisted: %+v", got.WorkspaceDirs)
	}
}

func TestSessionReasoningEffortPersistsAndClearsOnModelChange(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_reasoning")

	effort := "high"
	updated, err := st.UpdateSession(ctx, "sess_reasoning", store.SessionUpdate{ReasoningEffort: &effort})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ReasoningEffort != "high" || updated.ReasoningModelKey != "mock:mock" {
		t.Fatalf("reasoning effort not bound to current model: %+v", updated)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, err := reopened.GetSession(ctx, "sess_reasoning")
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "high" || got.ReasoningModelKey != "mock:mock" {
		t.Fatalf("reasoning effort not persisted: %+v", got)
	}

	model := "next"
	got, err = reopened.UpdateSession(ctx, "sess_reasoning", store.SessionUpdate{Model: &model})
	if err != nil {
		t.Fatal(err)
	}
	if got.ReasoningEffort != "" || got.ReasoningModelKey != "" {
		t.Fatalf("reasoning effort must clear on model change: %+v", got)
	}
}

func TestCanvasItemsAreGlobalWithSessionActor(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_left")
	createTestSession(t, st, "sess_right")

	item, err := st.PutCanvasItem(ctx, store.CanvasItemInput{
		ID:             "canvas_1",
		ActorSessionID: "sess_left",
		Kind:           "markdown",
		Title:          "Note",
		Item:           []byte(`{"kind":"markdown","content":"hello"}`),
		Window:         []byte(`{"x":1,"y":2,"w":300,"h":200,"z":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceSessionID != "sess_left" || item.CreatedBySessionID != "sess_left" || item.UpdatedBySessionID != "sess_left" {
		t.Fatalf("unexpected actor fields: %+v", item)
	}

	visible, err := st.ListCanvasItems(ctx, "sess_right")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].ID != "canvas_1" {
		t.Fatalf("right session should see global canvas item: %+v", visible)
	}

	item, err = st.UpdateCanvasItemWindow(ctx, store.CanvasItemWindowPatch{
		ActorSessionID: "sess_right",
		ItemID:         "canvas_1",
		Window:         []byte(`{"x":9,"y":8,"w":320,"h":240,"z":2}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if item.SourceSessionID != "sess_left" || item.UpdatedBySessionID != "sess_right" {
		t.Fatalf("source should stay fixed and actor should update: %+v", item)
	}

	if err := st.DeleteSession(ctx, "sess_left"); err != nil {
		t.Fatal(err)
	}
	visible, err = st.ListCanvasItems(ctx, "sess_right")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].SourceSessionID != "sess_left" {
		t.Fatalf("global canvas item should survive source session delete: %+v", visible)
	}

	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	visible, err = reopened.ListCanvasItems(ctx, "sess_right")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 1 || visible[0].UpdatedBySessionID != "sess_right" {
		t.Fatalf("canvas item not persisted: %+v", visible)
	}
}

func TestCanvasItemListOrderIgnoresWindowUpdates(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_canvas_order")

	for _, id := range []string{"canvas_a", "canvas_b"} {
		if _, err := st.PutCanvasItem(ctx, store.CanvasItemInput{
			ID:             id,
			ActorSessionID: "sess_canvas_order",
			Kind:           "markdown",
			Title:          id,
			Item:           []byte(`{"kind":"markdown","content":"hello"}`),
			Window:         []byte(`{"x":1,"y":2,"w":300,"h":200,"z":1}`),
		}); err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}

	visible, err := st.ListCanvasItems(ctx, "sess_canvas_order")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 2 || visible[0].ID != "canvas_a" || visible[1].ID != "canvas_b" {
		t.Fatalf("canvas items should use stable created order: %+v", visible)
	}

	if _, err := st.UpdateCanvasItemWindow(ctx, store.CanvasItemWindowPatch{
		ActorSessionID: "sess_canvas_order",
		ItemID:         "canvas_b",
		Window:         []byte(`{"x":9,"y":8,"w":320,"h":240,"z":99}`),
	}); err != nil {
		t.Fatal(err)
	}
	visible, err = st.ListCanvasItems(ctx, "sess_canvas_order")
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 2 || visible[0].ID != "canvas_a" || visible[1].ID != "canvas_b" {
		t.Fatalf("window updates should not reorder canvas items: %+v", visible)
	}
}

func TestRenameDoesNotAffectRecentOrdering(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "older")
	time.Sleep(2 * time.Millisecond)
	createTestSession(t, st, "newer")
	time.Sleep(2 * time.Millisecond)

	title := "renamed older"
	if _, err := st.UpdateSession(context.Background(), "older", store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	pinned := true
	pinnedOrder := int64(7)
	updated, err := st.UpdateSession(context.Background(), "older", store.SessionUpdate{
		Pinned:      &pinned,
		PinnedOrder: &pinnedOrder,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Pinned {
		t.Fatal("pinned flag was not persisted")
	}
	if updated.PinnedOrder != pinnedOrder {
		t.Fatalf("pinned order was not persisted: %d", updated.PinnedOrder)
	}
	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 || sessions[0].ID != "newer" || sessions[1].ID != "older" {
		t.Fatalf("rename must not affect recent ordering: %+v", sessions)
	}
}

func beginTestTurn(t *testing.T, st store.Store, sessionID, turnID, msgID, clientID string) *store.BeginTurnResult {
	t.Helper()
	res, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          turnID,
		UserMessageID:   msgID,
		ClientMessageID: clientID,
		UserText:        "hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func appendCompletedTestTurn(t *testing.T, st store.Store, sessionID string, index int) {
	t.Helper()
	suffix := strconv.Itoa(index)
	turnID := "turn_" + suffix
	beginTestTurn(t, st, sessionID, turnID, "msg_"+suffix, "client_"+suffix)
	text := "assistant " + suffix
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         turnID,
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
}

func TestBeginTurnIdempotencyPrecedesRunningConflict(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")

	first := beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if first.Duplicate || first.StartedEvent == nil || first.StartedEvent.Seq != 1 {
		t.Fatalf("unexpected first begin: %+v", first)
	}

	duplicate, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_other",
		UserMessageID:   "msg_other",
		ClientMessageID: "client_1",
		UserText:        "retry",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !duplicate.Duplicate || duplicate.Turn.ID != "turn_1" || duplicate.UserMessage.ID != "msg_1" || duplicate.StartedEvent != nil {
		t.Fatalf("duplicate should return original turn/message without event: %+v", duplicate)
	}

	_, err = st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_2",
		UserMessageID:   "msg_2",
		ClientMessageID: "client_2",
		UserText:        "conflict",
	})
	if !errors.Is(err, store.ErrTurnRunning) {
		t.Fatalf("want ErrTurnRunning, got %v", err)
	}
}

func TestQueuedInputPersistsAttachments(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_attach")
	attachment := store.Attachment{
		ID:            "att_1",
		Name:          "note.txt",
		AttachmentKey: "sessions/sess_attach/blobs/note.txt",
		URL:           "/sessions/sess_attach/attachments/blobs/note.txt",
		MIME:          "text/plain",
		Size:          12,
		Origin:        "upload",
	}
	if _, err := st.QueueInput(ctx, store.QueueInputInput{
		SessionID:       "sess_attach",
		ClientMessageID: "client_attach",
		Attachments:     []store.Attachment{attachment},
		Provider:        "mock",
		Model:           "mock",
		Mode:            store.ModeChat,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	queued, err := reopened.ListQueuedInputs(ctx, "sess_attach")
	if err != nil {
		t.Fatal(err)
	}
	if len(queued) != 1 || len(queued[0].Attachments) != 1 || queued[0].Attachments[0].AttachmentKey != attachment.AttachmentKey {
		t.Fatalf("queued attachment was not persisted: %+v", queued)
	}
	promoted, err := reopened.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{
		SessionID:     "sess_attach",
		TurnID:        "turn_attach",
		UserMessageID: "msg_attach",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := store.AttachmentsFromParts(promoted.UserMessage.Parts)
	if len(got) != 1 || got[0].AttachmentKey != attachment.AttachmentKey {
		t.Fatalf("promoted message lost attachment: %+v", promoted.UserMessage.Parts)
	}
}

func TestListMessagesPageUsesStableOrder(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	for i := 1; i <= 4; i++ {
		appendCompletedTestTurn(t, st, "sess_1", i)
	}

	first, err := st.ListMessagesPage(context.Background(), "sess_1", "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore {
		t.Fatal("first recent page should report older messages")
	}
	got := messageLabels(first.Messages)
	want := []string{"user:hello", "assistant:assistant 3", "user:hello", "assistant:assistant 4"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}

	older, err := st.ListMessagesPage(context.Background(), "sess_1", first.Messages[0].ID, 4)
	if err != nil {
		t.Fatal(err)
	}
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	got = messageLabels(older.Messages)
	want = []string{"user:hello", "assistant:assistant 1", "user:hello", "assistant:assistant 2"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}

	_, err = st.ListMessagesPage(context.Background(), "sess_1", "missing", 4)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing before message should return ErrNotFound, got %v", err)
	}
}

func TestListTurnsPageUsesStableOrder(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	for i := 1; i <= 4; i++ {
		appendCompletedTestTurn(t, st, "sess_1", i)
	}

	first, err := st.ListTurnsPage(context.Background(), "sess_1", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasMore {
		t.Fatal("first recent page should report older turns")
	}
	got := turnIDs(first.Turns)
	want := []string{"turn_3", "turn_4"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected recent page: got %v want %v", got, want)
	}
	gotMessages := messageLabels(first.Turns[0].Messages)
	wantMessages := []string{"user:hello", "assistant:assistant 3"}
	if !sameStrings(gotMessages, wantMessages) {
		t.Fatalf("turn should include complete messages: got %v want %v", gotMessages, wantMessages)
	}

	older, err := st.ListTurnsPage(context.Background(), "sess_1", first.Turns[0].ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if older.HasMore {
		t.Fatal("older page should be exhausted")
	}
	got = turnIDs(older.Turns)
	want = []string{"turn_1", "turn_2"}
	if !sameStrings(got, want) {
		t.Fatalf("unexpected older page: got %v want %v", got, want)
	}

	_, err = st.ListTurnsPage(context.Background(), "sess_1", "missing", 2)
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing before turn should return ErrNotFound, got %v", err)
	}
}

func TestFinishTurnTerminalStatesAndEventsAfter(t *testing.T) {
	st, _ := openTestStore(t)

	cases := []struct {
		sessionID   string
		turnID      string
		msgID       string
		clientID    string
		status      store.TurnStatus
		kind        event.Kind
		text        *string
		interrupted bool
		errorText   string
	}{
		{"sess_completed", "turn_completed", "msg_completed", "client_completed", store.TurnCompleted, event.TurnCompleted, strptr("done"), false, ""},
		{"sess_failed", "turn_failed", "msg_failed", "client_failed", store.TurnFailed, event.TurnFailed, strptr("partial"), true, "boom"},
		{"sess_cancelled", "turn_cancelled", "msg_cancelled", "client_cancelled", store.TurnCancelled, event.TurnCancelled, strptr("partial"), true, ""},
	}

	for _, tc := range cases {
		createTestSession(t, st, tc.sessionID)
		beginTestTurn(t, st, tc.sessionID, tc.turnID, tc.msgID, tc.clientID)
		var parts []store.ContentPart
		if tc.text != nil {
			parts = store.TextPart(*tc.text)
		}
		res, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
			TurnID:         tc.turnID,
			Status:         tc.status,
			AssistantParts: parts,
			Interrupted:    tc.interrupted,
			Error:          tc.errorText,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.FinalEvent.Kind != tc.kind || res.FinalEvent.Seq != 2 {
			t.Fatalf("unexpected final event: %+v", res.FinalEvent)
		}
		if tc.text != nil {
			if res.AssistantMessage == nil || res.FinalEvent.AssistantMessageID != res.AssistantMessage.ID {
				t.Fatalf("final event must reference assistant message: %+v", res)
			}
			if res.AssistantMessage.Interrupted != tc.interrupted {
				t.Fatalf("unexpected interrupted flag: %+v", res.AssistantMessage)
			}
		}

		afterStart, err := st.EventsAfter(context.Background(), tc.sessionID, 1, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(afterStart) != 1 || afterStart[0].Kind != tc.kind || afterStart[0].Seq != 2 {
			t.Fatalf("EventsAfter should return final event only: %+v", afterStart)
		}
	}
}

func TestAppendCompactSummaryPersistsMetadataAndEvent(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_compact")

	res, err := st.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{
		SessionID:       "sess_compact",
		TurnID:          "turn_compact",
		MessageID:       "msg_compact",
		ClientMessageID: "compact:turn_compact",
		Provider:        "mock",
		Model:           "mock",
		Text:            "summary @message(msg_1)",
		Metadata:        store.CompactMessageMetadata([]string{"msg_1", "msg_2"}, []string{"msg_3"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.FinalEvent == nil || res.FinalEvent.Kind != event.TurnCompleted || res.FinalEvent.AssistantMessageID != "msg_compact" {
		t.Fatalf("unexpected compact event: %+v", res.FinalEvent)
	}
	if _, err := st.RunningTurn(ctx, "sess_compact"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("compact must not leave a running turn: %v", err)
	}
	msgs, err := st.ListMessages(ctx, "sess_compact", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Role != store.RoleSummary || msgs[0].Kind != store.MessageKindSummary {
		t.Fatalf("unexpected compact message: %+v", msgs)
	}
	meta, ok := store.CompactMetadataFromMessage(msgs[0])
	if !ok {
		t.Fatalf("compact metadata missing: %+v", msgs[0])
	}
	if !sameStrings(meta.SourceMessageIDs, []string{"msg_1", "msg_2"}) || !sameStrings(meta.TailMessageIDs, []string{"msg_3"}) {
		t.Fatalf("unexpected compact metadata: %+v", meta)
	}
	events, err := st.EventsAfter(ctx, "sess_compact", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != event.TurnCompleted || events[0].TurnID != "turn_compact" {
		t.Fatalf("unexpected compact events: %+v", events)
	}
}

func TestPersistenceAndSeqContinuation(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	text := "first"
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         "turn_1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	sessions, err := reopened.ListSessions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].ID != "sess_1" {
		t.Fatalf("sessions not persisted: %+v", sessions)
	}
	msgs, err := reopened.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[1].Text != "first" {
		t.Fatalf("messages not persisted: %+v", msgs)
	}
	evs, err := reopened.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Seq != 1 || evs[1].Seq != 2 {
		t.Fatalf("events not persisted: %+v", evs)
	}

	beginTestTurn(t, reopened, "sess_1", "turn_2", "msg_2", "client_2")
	evs, err = reopened.EventsAfter(context.Background(), "sess_1", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 1 || evs[0].Seq != 3 || evs[0].Kind != event.TurnStarted {
		t.Fatalf("seq did not continue after reopen: %+v", evs)
	}
}

func TestMessagePartsPersist(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_1",
		Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartThought, Text: "thinking"},
			{Type: store.ContentPartToolUse, CallID: "call_1", Name: "web_fetch", Args: []byte(`{"url":"https://example.com"}`)},
			{Type: store.ContentPartToolResult, CallID: "call_1", Name: "web_fetch", Ok: true, Content: `{"ok":true}`},
			{Type: store.ContentPartText, Text: "done"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	msgs, err := reopened.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	thought := msgs[1]
	if thought.Role != store.RoleAssistant || thought.Kind != store.MessageKindThought || thought.Text != "thinking" {
		t.Fatalf("thought message not persisted: %+v", thought)
	}
	toolUse := msgs[2]
	if toolUse.Role != store.RoleAssistant || toolUse.Kind != store.MessageKindToolUse || len(toolUse.Parts) != 1 || toolUse.Parts[0].Type != store.ContentPartToolUse {
		t.Fatalf("tool_use message not persisted: %+v", toolUse)
	}
	toolResult := msgs[3]
	if toolResult.Role != store.RoleTool || toolResult.Kind != store.MessageKindToolResult || len(toolResult.Parts) != 1 || toolResult.Parts[0].Type != store.ContentPartToolResult {
		t.Fatalf("tool_result message not persisted: %+v", toolResult)
	}
	assistant := msgs[4]
	if assistant.Role != store.RoleAssistant || assistant.Kind != store.MessageKindText || assistant.Text != "done" {
		t.Fatalf("text message not persisted: %+v", assistant)
	}
	turns, err := reopened.ListTurnsPage(context.Background(), "sess_1", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns.Turns) != 1 {
		t.Fatalf("unexpected turns: %+v", turns.Turns)
	}
	got := messageLabels(turns.Turns[0].Messages)
	want := []string{"user:hello", "assistant:thinking", "assistant:web_fetch", "tool:{\"ok\":true}", "assistant:done"}
	if !sameStrings(got, want) {
		t.Fatalf("turn should group all messages in turn_index order: got %v want %v", got, want)
	}
}

func TestAppendTurnOutputBeforeFinish(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")

	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts:  store.TextPart("first"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{
		TurnID: "turn_1",
		Parts: []store.ContentPart{{
			Type:   store.ContentPartToolUse,
			CallID: "call_1",
			Name:   "builtin_time_get_current",
			Args:   []byte(`{"timezone":"Asia/Singapore"}`),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "turn_1",
		Status: store.TurnCompleted,
	}); err != nil {
		t.Fatal(err)
	}

	msgs, err := st.ListMessages(context.Background(), "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := messageLabels(msgs), []string{"user:hello", "assistant:first", "assistant:builtin_time_get_current"}; !sameStrings(got, want) {
		t.Fatalf("finish must not duplicate appended output: got %v want %v", got, want)
	}
	if msgs[1].Text != "first" || msgs[2].Kind != store.MessageKindToolUse {
		t.Fatalf("unexpected appended messages: %+v", msgs)
	}
	evs, err := st.EventsAfter(context.Background(), "sess_1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[1].Kind != event.TurnCompleted || evs[1].AssistantMessageID != msgs[1].ID {
		t.Fatalf("final event should point at first appended output: %+v", evs)
	}
}

func TestTurnModelConfigPersists(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_1")
	cfg := []byte(`{"contextWindow":1000,"openai":{"temperature":0.6}}`)
	if _, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       "sess_1",
		TurnID:          "turn_1",
		UserMessageID:   "msg_1",
		ClientMessageID: "client_1",
		UserText:        "hello",
		Provider:        "default",
		Model:           "m1",
		ModelConfig:     cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	turn, err := reopened.RunningTurn(context.Background(), "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if string(turn.ModelConfig) != string(cfg) {
		t.Fatalf("model config not persisted: %s", turn.ModelConfig)
	}
	turns, err := reopened.ListTurnsPage(context.Background(), "sess_1", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns.Turns) != 1 {
		t.Fatalf("unexpected turns: %+v", turns.Turns)
	}
	if turns.Turns[0].Provider != "default" || turns.Turns[0].Model != "m1" {
		t.Fatalf("conversation turn model snapshot wrong: provider=%q model=%q", turns.Turns[0].Provider, turns.Turns[0].Model)
	}
}

func TestDeleteSessionCascades(t *testing.T) {
	st, _ := openTestStore(t)
	createTestSession(t, st, "sess_1")
	beginTestTurn(t, st, "sess_1", "turn_1", "msg_1", "client_1")
	text := "done"
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID:         "turn_1",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart(text),
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteSession(context.Background(), "sess_1"); err != nil {
		t.Fatal(err)
	}

	for _, table := range []string{"turns", "messages", "events"} {
		var count int
		if err := st.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s should cascade delete, count=%d", table, count)
		}
	}
}

func TestSchemaDoesNotStoreConfigTables(t *testing.T) {
	st, _ := openTestStore(t)
	rows, err := st.db.Query(`SELECT name FROM sqlite_master WHERE type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		if name == "settings" || name == "provider_profiles" {
			t.Fatalf("config table %q must not be in SQLite runtime store", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestUsageHourlyStatsRecordAndQuery(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	at := time.Date(2026, 6, 23, 10, 37, 0, 0, time.FixedZone("SGT", 8*60*60))

	first, err := st.RecordUsage(ctx, store.UsageRecordInput{
		OccurredAt:            at,
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantHour := at.UTC().Truncate(time.Hour)
	if !first.HourStartAt.Equal(wantHour) || first.RequestCount != 1 {
		t.Fatalf("first stat = %+v want hour %s count 1", first, wantHour)
	}
	if first.TotalTokens() != 150 {
		t.Fatalf("first total = %d want 150", first.TotalTokens())
	}

	second, err := st.RecordUsage(ctx, store.UsageRecordInput{
		OccurredAt:            at.Add(10 * time.Minute),
		RequestCount:          2,
		InputUncachedTokens:   -1,
		InputCachedTokens:     1,
		OutputContentTokens:   2,
		OutputReasoningTokens: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.RequestCount != 3 || second.InputUncachedTokens != 10 || second.InputCachedTokens != 21 || second.TotalTokens() != 156 {
		t.Fatalf("merged stat wrong: %+v", second)
	}

	third, err := st.RecordUsage(ctx, store.UsageRecordInput{
		OccurredAt:          at.Add(20 * time.Minute),
		Model:               "model-b",
		InputUncachedTokens: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	if third.Model != "model-b" || third.RequestCount != 1 || third.TotalTokens() != 7 {
		t.Fatalf("model stat wrong: %+v", third)
	}

	stats, err := st.UsageHourlyStats(ctx, wantHour, wantHour.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0].Model != "" || stats[0].RequestCount != 3 || stats[0].TotalTokens() != 156 || stats[1].Model != "model-b" || stats[1].TotalTokens() != 7 {
		t.Fatalf("queried stats wrong: %+v", stats)
	}
}

func TestSessionUsageRecordAndQuery(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_usage")

	empty, err := st.SessionUsage(ctx, "sess_usage")
	if err != nil {
		t.Fatal(err)
	}
	if empty.SessionID != "sess_usage" || empty.RequestCount != 0 || empty.CumulativeTotalTokens() != 0 {
		t.Fatalf("empty session usage wrong: %+v", empty)
	}

	first, err := st.RecordSessionUsage(ctx, "sess_usage", store.UsageRecordInput{
		InputUncachedTokens:   10,
		InputCachedTokens:     20,
		CacheCreationTokens:   30,
		OutputContentTokens:   40,
		OutputReasoningTokens: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.RequestCount != 1 || first.LastInputTokens() != 60 || first.LastOutputTokens() != 90 || first.CumulativeTotalTokens() != 150 {
		t.Fatalf("first session usage wrong: %+v", first)
	}

	second, err := st.RecordSessionUsage(ctx, "sess_usage", store.UsageRecordInput{
		RequestCount:          2,
		InputUncachedTokens:   -1,
		InputCachedTokens:     1,
		OutputContentTokens:   2,
		OutputReasoningTokens: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.RequestCount != 3 || second.LastInputUncachedTokens != 0 || second.LastInputTokens() != 1 || second.LastOutputTokens() != 5 || second.CumulativeTotalTokens() != 156 {
		t.Fatalf("merged session usage wrong: %+v", second)
	}

	if err := st.DeleteSession(ctx, "sess_usage"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SessionUsage(ctx, "sess_usage"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deleted session usage should be not found, got %v", err)
	}
}

func strptr(s string) *string { return &s }

func messageLabels(messages []*store.Message) []string {
	out := make([]string, 0, len(messages))
	for _, msg := range messages {
		out = append(out, string(msg.Role)+":"+msg.Text)
	}
	return out
}

func turnIDs(turns []*store.ConversationTurn) []string {
	out := make([]string, 0, len(turns))
	for _, turn := range turns {
		out = append(out, turn.ID)
	}
	return out
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

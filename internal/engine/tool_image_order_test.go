package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestCurrentTurnImagesStayBeforeLaterActions(t *testing.T) {
	home := t.TempDir()
	var parts []store.ContentPart
	for _, step := range []struct{ id, image string }{{"observe_before", "before"}, {"click", ""}, {"observe_after", "after"}, {"next_click", ""}} {
		parts = append(parts,
			store.ContentPart{Type: store.ContentPartToolUse, CallID: step.id, Name: step.id},
			store.ContentPart{Type: store.ContentPartToolResult, CallID: step.id, Name: step.id, Ok: true, Content: "ok"},
		)
		if step.image != "" {
			stored, err := attachment.NewService(home).StoreReader("s1", "window.png", "image/png", bytes.NewBufferString(step.image))
			if err != nil {
				t.Fatal(err)
			}
			stored.Origin = attachment.OriginTool
			parts = append(parts, store.AttachmentPart(stored))
		}
	}
	messages := requestMessagesWithTurnParts(nil, parts, nil, "s1", home, provider.ModelConfig{Capabilities: &provider.ModelCapabilities{Image: true}})
	var order []string
	for _, message := range messages {
		for _, part := range message.Parts {
			switch part.Type {
			case provider.PartToolResult:
				order = append(order, part.CallID)
			case provider.PartImage:
				if message.Role != provider.RoleUser {
					t.Fatalf("image must be user input: %+v", message)
				}
				order = append(order, string(part.Data))
			}
		}
	}
	if got, want := strings.Join(order, ","), "observe_before,before,click,observe_after,after,next_click"; got != want {
		t.Fatalf("tool/image timeline = %s; want %s", got, want)
	}
}

func TestToolImageTimelineSurvivesCanonicalReplay(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	dbPath := filepath.Join(home, "test.db")
	db, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "vision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{
		SessionID: "s1", TurnID: "t1", UserMessageID: "m1", ClientMessageID: "c1", UserText: "inspect", Mode: store.ModeWork,
	}); err != nil {
		t.Fatal(err)
	}
	image := func(value string) store.ContentPart {
		t.Helper()
		stored, err := attachment.NewService(home).StoreReader("s1", "window.png", "image/png", bytes.NewBufferString(value))
		if err != nil {
			t.Fatal(err)
		}
		stored.Origin = attachment.OriginTool
		return store.AttachmentPart(stored)
	}
	// Two observations in one model call, then an action, then a fresh image
	// of the same window. No action implicitly produces a new screenshot.
	batches := [][]store.ContentPart{
		{{Type: store.ContentPartToolUse, CallID: "a", Name: "observe", Args: json.RawMessage(`{"windowID":"1"}`)},
			{Type: store.ContentPartToolUse, CallID: "b", Name: "observe", Args: json.RawMessage(`{"windowID":"2"}`)},
			{Type: store.ContentPartToolResult, CallID: "a", Name: "observe", Ok: true, Content: "window 1"}, image("before"),
			{Type: store.ContentPartToolResult, CallID: "b", Name: "observe", Ok: true, Content: "window 2"}, image("other")},
		{{Type: store.ContentPartToolUse, CallID: "click", Name: "click"},
			{Type: store.ContentPartToolResult, CallID: "click", Name: "click", Ok: true, Content: "delivered"}},
		{{Type: store.ContentPartToolUse, CallID: "c", Name: "observe", Args: json.RawMessage(`{"windowID":"1"}`)},
			{Type: store.ContentPartToolResult, CallID: "c", Name: "observe", Ok: true, Content: "window 1"}, image("after")},
	}
	var all []store.ContentPart
	var continuations []provider.Continuation
	for i, batch := range batches {
		raw, _ := json.Marshal([]string{"native-state", string(rune('a' + i))})
		state := &store.ProviderState{Provider: "mock", Model: "vision", Kind: provider.ContinuationGoogle, Data: raw}
		// Match engine commits: model output/state precedes tool execution.
		resultIndex := 0
		for batch[resultIndex].Type == store.ContentPartToolUse {
			resultIndex++
		}
		if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "t1", Parts: batch[:resultIndex], ProviderState: state}); err != nil {
			t.Fatal(err)
		}
		if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "t1", Parts: batch[resultIndex:]}); err != nil {
			t.Fatal(err)
		}
		all = append(all, batch...)
		continuations = append(continuations, provider.Continuation{Kind: state.Kind, Data: state.Data})
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t1", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitestore.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	for _, vision := range []bool{true, false} {
		cfg := provider.ModelConfig{Capabilities: &provider.ModelCapabilities{Image: vision}}
		current := requestMessagesWithTurnParts(nil, all, continuations, "s1", home, cfg)
		req, err := contextbuilder.New(reopened, nil, contextbuilder.WithAttachmentHome(home)).BuildForProviderWithTools(
			ctx, "s1", "mock", "vision", string(store.ModeWork), []provider.ToolDef{{Name: "observe"}, {Name: "click"}}, cfg)
		if err != nil {
			t.Fatal(err)
		}
		timeline := func(messages []provider.Message) []string {
			var got []string
			for _, msg := range messages {
				for _, state := range msg.Continuations {
					got = append(got, "state:"+string(state.Data))
				}
				for _, part := range msg.Parts {
					switch part.Type {
					case provider.PartToolUse, provider.PartToolResult:
						got = append(got, string(part.Type)+":"+part.CallID)
					case provider.PartText:
						if part.CallID != "" {
							if msg.Role != provider.RoleUser || !strings.Contains(part.Text, "Source tool call: "+part.CallID) || !strings.Contains(part.Text, "Attachment created at:") {
								t.Fatalf("incorrect attachment attribution: %+v", msg)
							}
							got = append(got, "attachment:"+part.CallID)
						}
					case provider.PartImage:
						if !vision || msg.Role != provider.RoleUser {
							t.Fatalf("unexpected image input: %+v", msg)
						}
						got = append(got, "image:"+part.CallID+":"+string(part.Data))
					}
				}
			}
			return got
		}
		got, want := timeline(req.Messages), timeline(current)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("vision=%v: replay timeline differs\ngot  %v\nwant %v", vision, got, want)
		}
		sequence := strings.Join(got, "\n")
		if strings.Index(sequence, "tool_result:b") > strings.Index(sequence, "attachment:a") ||
			strings.Index(sequence, "attachment:b") > strings.Index(sequence, "tool_use:click") ||
			strings.Index(sequence, "tool_result:click") > strings.Index(sequence, "attachment:c") {
			t.Fatalf("screenshots moved outside their result batch: %v", got)
		}
		// Rebuilding cannot accumulate annotations or mutate canonical parts.
		if again := requestMessagesWithTurnParts(nil, all, continuations, "s1", home, cfg); !reflect.DeepEqual(current, again) {
			t.Fatal("request rebuild mutated the source timeline")
		}
	}
}

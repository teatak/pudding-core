package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestScreenshotBudgetAcrossHistoryCurrentTurnAndReadback(t *testing.T) {
	ctx := context.Background()
	home := t.TempDir()
	db, err := sqlitestore.Open(filepath.Join(home, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.CreateSession(ctx, &store.Session{ID: "s1", Provider: "mock", Model: "vision"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s1", TurnID: "t1", UserMessageID: "u1", ClientMessageID: "u1", UserText: "inspect", Mode: store.ModeCode}); err != nil {
		t.Fatal(err)
	}
	var parts []store.ContentPart
	var first store.Attachment
	service := attachment.NewService(home)
	// Both legacy/default reads and explicit references must survive later
	// screenshot batches, including after canonical history is reopened.
	for _, purpose := range []string{"", tool.MediaPurposeReference} {
		label := "reference-" + purpose
		stored, err := service.StoreReader("s1", "design.png", "image/png", bytes.NewBufferString(label))
		if err != nil {
			t.Fatal(err)
		}
		args := map[string]string{"source": "attachment", "attachmentKey": stored.AttachmentKey}
		if purpose != "" {
			args["purpose"] = purpose
		}
		raw, _ := json.Marshal(args)
		read := tool.NewBuiltinRunner(tool.WithHomeDir(home)).Call(ctx, tool.Call{SessionID: "s1", CallID: label, Name: tool.MediaRead, Mode: store.ModeCode, Args: raw})
		if !read.Ok || len(read.ContextAttachments) != 1 {
			t.Fatalf("reference read failed: %+v", read)
		}
		batch := []store.ContentPart{{Type: store.ContentPartToolUse, CallID: label, Name: tool.MediaRead, Args: raw}, {Type: store.ContentPartToolResult, CallID: label, Name: tool.MediaRead, Ok: true, Content: read.Content}, store.AttachmentPart(read.ContextAttachments[0])}
		parts = append(parts, batch...)
		if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "t1", Parts: batch}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 32; i++ {
		stored, err := service.StoreReader("s1", "browser-screenshot.png", "image/png", bytes.NewBufferString(fmt.Sprintf("image-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
		stored.Origin = attachment.OriginTool
		if i == 0 {
			first = stored
		}
		id := fmt.Sprintf("capture-%d", i)
		batch := []store.ContentPart{{Type: store.ContentPartToolUse, CallID: id, Name: tool.BrowserScreenshot, Args: json.RawMessage(`{}`)}, {Type: store.ContentPartToolResult, CallID: id, Name: tool.BrowserScreenshot, Ok: true, Content: `{"ok":true}`}, store.AttachmentPart(stored)}
		parts = append(parts, batch...)
		if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "t1", Parts: batch}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "t1", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(filepath.Join(home, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s1", TurnID: "t2", UserMessageID: "u2", ClientMessageID: "u2", UserText: "continue"}); err != nil {
		t.Fatal(err)
	}
	cfg := provider.ModelConfig{ContextWindow: 160000, Capabilities: &provider.ModelCapabilities{Image: true}}
	builder := contextbuilder.New(db, nil, contextbuilder.WithAttachmentHome(home))
	req, err := builder.BuildForProviderWithTools(ctx, "s1", "mock", "vision", string(store.ModeCode), []provider.ToolDef{{Name: tool.BrowserScreenshot}, {Name: tool.MediaRead}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	images := func(messages []provider.Message) []string {
		var out []string
		for _, m := range messages {
			for _, p := range m.Parts {
				if p.Type == provider.PartImage {
					out = append(out, string(p.Data))
				}
			}
		}
		return out
	}
	current := requestMessagesWithTurnParts(nil, parts, nil, "s1", "t1", home, cfg, true)
	if got, want := images(req.Messages), images(current); len(got) != 34 || !reflect.DeepEqual(got, want) {
		t.Fatal("builder pruned images without a budget decision")
	}
	req.Config = cfg
	settings := config.NewManager(home)
	if err := settings.Prepare(); err != nil {
		t.Fatal(err)
	}
	eng := &Engine{store: db, config: settings}
	resolved := &resolvedModel{providerName: "mock", model: "vision", protocol: "openai-compatible", config: cfg}
	if changed, err := eng.prepareContextRequest(ctx, "s1", "t2", resolved, store.ModeCode, &req, false); err != nil || !changed {
		t.Fatalf("image budget not committed: %v %v", changed, err)
	}
	state, err := db.ContextCooling(ctx, "s1")
	if err != nil || state.ImageThroughID == "" {
		t.Fatal("image watermark not saved")
	}
	current = contextbuilder.ApplyToolImageWatermark(current, state.ImageThroughID)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = sqlitestore.Open(filepath.Join(home, "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	builder = contextbuilder.New(db, nil, contextbuilder.WithAttachmentHome(home))
	req, err = builder.BuildForProviderWithTools(ctx, "s1", "mock", "vision", string(store.ModeCode), []provider.ToolDef{{Name: tool.BrowserScreenshot}, {Name: tool.MediaRead}}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := images(req.Messages), images(current); len(got) != 9 || !reflect.DeepEqual(got, want) || got[0] != "reference-" || got[1] != "reference-reference" || got[2] != "image-25" {
		t.Fatalf("history/current budgets disagree: %v / %v", got, want)
	}
	// The first screenshot is now absent, but its original can be loaded by key.
	args, _ := json.Marshal(map[string]string{"source": "attachment", "attachmentKey": first.AttachmentKey, "purpose": tool.MediaPurposeObservation})
	read := tool.NewBuiltinRunner(tool.WithHomeDir(home)).Call(ctx, tool.Call{SessionID: "s1", CallID: "read-old", Name: tool.MediaRead, Mode: store.ModeCode, Args: args})
	if !read.Ok || len(read.ContextAttachments) != 1 {
		t.Fatalf("old screenshot cannot be reloaded: %+v", read)
	}
	reread := []store.ContentPart{{Type: store.ContentPartToolUse, CallID: read.CallID, Name: read.Name, Args: args}, {Type: store.ContentPartToolResult, CallID: read.CallID, Name: read.Name, Ok: true, Content: read.Content}, store.AttachmentPart(read.ContextAttachments[0])}
	reloaded := requestMessagesWithTurnParts(req.Messages, reread, nil, "s1", "t2", home, cfg, true)
	got := images(reloaded)
	if len(got) != 10 || got[0] != "reference-" || got[1] != "reference-reference" || got[len(got)-1] != "image-0" {
		t.Fatalf("readback did not become recent/bounded: %v", got)
	}
	// Rebuilding cannot revive the evicted images or alter saved attachments.
	if !reflect.DeepEqual(reloaded, requestMessagesWithTurnParts(req.Messages, reread, nil, "s1", "t2", home, cfg, true)) {
		t.Fatal("replay changed image projection")
	}
	canonical, err := db.ListMessages(ctx, "s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	attachments := 0
	for _, m := range canonical {
		for _, p := range m.Parts {
			if p.Type == store.ContentPartAttachment {
				attachments++
			}
		}
	}
	if attachments != 34 {
		t.Fatalf("canonical attachments changed: %d", attachments)
	}
}

func TestScreenshotCleanupCoalescesWithCoolingAndCompact(t *testing.T) {
	for _, operation := range []string{"cooling", "compact"} {
		t.Run(operation, func(t *testing.T) {
			e, db, resolved, _ := coolingFixture(t)
			ctx, sid := context.Background(), "sess_1"
			home := t.TempDir()
			WithAttachmentHome(home)(e)
			resolved.config = provider.ModelConfig{ContextWindow: 1000000, Capabilities: &provider.ModelCapabilities{Image: true, Tools: true}}
			e.tools = &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.CommandRun}, {Name: tool.HistoryGetMessage}, {Name: tool.BrowserScreenshot}}}
			for i := 0; i < 9; i++ {
				img, err := attachment.NewService(home).StoreReader(sid, "browser-screenshot.png", "image/png", bytes.NewBufferString(fmt.Sprint(i)))
				if err != nil {
					t.Fatal(err)
				}
				img.Origin = attachment.OriginTool
				id := fmt.Sprint(i)
				if _, err := db.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "current", Parts: []store.ContentPart{{Type: store.ContentPartToolUse, Name: tool.BrowserScreenshot, CallID: id, Args: json.RawMessage(`{}`)}, {Type: store.ContentPartToolResult, Name: tool.BrowserScreenshot, CallID: id, Ok: true, Content: `{}`}, store.AttachmentPart(img)}}); err != nil {
					t.Fatal(err)
				}
			}
			build := func() provider.Request {
				r, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", e.tools.(*recordingToolRunner).defs, resolved.config)
				if err != nil {
					t.Fatal(err)
				}
				r.Config = resolved.config
				r.Tools = e.tools.(*recordingToolRunner).defs
				return r
			}
			req := build()
			if operation == "cooling" {
				before := requestInputTokens(req)
				if changed, err := e.tryCoolContext(ctx, sid, "current", resolved, store.ModeCode, &req, before, before-1, 1); err != nil || !changed {
					t.Fatalf("joint cooling failed: %v %v", changed, err)
				}
				state, err := db.ContextCooling(ctx, sid)
				if err != nil || state.ThroughTurnID == "" || state.ImageThroughID == "" {
					t.Fatalf("missing joint watermark: %+v %v", state, err)
				}
				if err := db.SetSettings(ctx, map[string]string{config.SettingToolResultCoolingEnabled: "false"}); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := e.compact(ctx, CompactInput{SessionID: sid}, resolved, store.ModeCode, "current", false); err != nil {
					t.Fatal(err)
				}
				state, err := db.ContextCooling(ctx, sid)
				if err != nil || state.ThroughTurnID != "" || state.SummaryMessageID == "" || state.ImageThroughID == "" {
					t.Fatalf("missing compact image state: %+v %v", state, err)
				}
			}
			after := build()
			count := 0
			for _, m := range after.Messages {
				for _, p := range m.Parts {
					if p.Type == provider.PartImage {
						count++
					}
				}
			}
			if count != 3 {
				t.Fatalf("%s retained %d observations, want 3", operation, count)
			}
			// A subsequent compact must not revive observations already omitted by cooling.
			if operation == "cooling" {
				if _, err := e.compact(ctx, CompactInput{SessionID: sid}, resolved, store.ModeCode, "current", false); err != nil {
					t.Fatal(err)
				}
				count = 0
				for _, m := range build().Messages {
					for _, p := range m.Parts {
						if p.Type == provider.PartImage {
							count++
						}
					}
				}
				if count != 3 {
					t.Fatal("compact revived omitted images")
				}
			}
		})
	}
}

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
	messages := requestMessagesWithTurnParts(nil, parts, nil, "s1", "t1", home, provider.ModelConfig{Capabilities: &provider.ModelCapabilities{Image: true}}, false)
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
		current := requestMessagesWithTurnParts(nil, all, continuations, "s1", "t1", home, cfg, false)
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
		if again := requestMessagesWithTurnParts(nil, all, continuations, "s1", "t1", home, cfg, false); !reflect.DeepEqual(current, again) {
			t.Fatal("request rebuild mutated the source timeline")
		}
	}
}

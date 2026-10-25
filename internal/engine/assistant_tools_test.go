package engine

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestAssistantNameToolsScopeAndValidation(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	runner := tool.NewBuiltinRunner()
	t.Cleanup(func() { _ = runner.Close() })
	eng := New(db, event.NewHub(), nil, db, WithTools(runner))
	t.Cleanup(eng.Stop)
	if err := db.CreateSession(ctx, &store.Session{ID: "ordinary", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	defs, err := eng.toolDefinitions(ctx, "ordinary", store.ModeChat)
	if err != nil || tool.HasDefinition(defs, tool.AssistantRename) {
		t.Fatalf("uninitialized assistant leaked a tool: %v", err)
	}
	profile, err := db.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil {
		t.Fatal(err)
	}
	sid := profile.Assistant.SessionID
	if err := db.CreateChildSession(ctx, sid, &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for _, plugins := range []PluginSource{nil, plugin.NewService(t.TempDir(), db.Manager)} {
		eng.plugins = plugins
		for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
			for _, sessionID := range []string{sid, "ordinary", "child"} {
				defs, err := eng.toolDefinitions(ctx, sessionID, mode)
				for _, def := range tool.AssistantDefinitions() {
					if err != nil || tool.HasDefinition(defs, def.Name) != (sessionID == sid) {
						t.Fatalf("scope %s/%s/%s: %v", sessionID, mode, def.Name, err)
					}
				}
			}
		}
	}
	for _, name := range []string{tool.AssistantRename, tool.AssistantUserName} {
		call := func(sessionID, args string) tool.Result {
			return eng.executeAllowedTool(ctx, sessionID, "turn", store.ModeChat, tool.Call{Name: name, Args: json.RawMessage(args)})
		}
		for _, args := range []string{`{}`, `null`, `{"name":null}`, `{"name":1}`, `{"name":"two\nlines"}`, `{"name":"new","userName":"wrong"}`, `{"name":"new"}{}`, `{"name":"` + strings.Repeat("名", 81) + `"}`} {
			if result := call(sid, args); result.Ok {
				t.Fatalf("invalid rename succeeded: %s", args)
			}
		}
		for _, sessionID := range []string{"ordinary", "child"} {
			for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
				// The engine's session argument is authoritative, not caller-supplied metadata.
				result := eng.executeAllowedTool(ctx, sessionID, "turn", mode, tool.Call{
					SessionID: sid, Name: name, Args: json.RawMessage(`{"name":"wrong"}`),
				})
				if result.Ok || !strings.Contains(result.Content, "only available in its own conversation") {
					t.Fatalf("direct call bypassed session scope: %s/%s: %+v", sessionID, mode, result)
				}
			}
		}
	}
	unchanged, err := db.GetAssistant(ctx)
	if err != nil || !reflect.DeepEqual(profile, unchanged) {
		t.Fatalf("rejected calls changed the profile: %v", err)
	}
}

func TestOrdinaryAndChildConversationsRejectForgedAssistantToolCalls(t *testing.T) {
	for _, target := range []string{"ordinary", "assistant-child"} {
		for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
			t.Run(target+"/"+string(mode), func(t *testing.T) {
				ctx := context.Background()
				db := storetest.New(t)
				profile, err := db.OpenAssistant(ctx, "小布", "capture", "test", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
				if err != nil {
					t.Fatal(err)
				}
				spoofedArgs, err := json.Marshal(map[string]string{"name": "越权改名", "sessionID": profile.Assistant.SessionID})
				if err != nil {
					t.Fatal(err)
				}
				// Simulate a model ignoring its tool list, including an attempt to
				// impersonate the assistant through arguments. Neither may execute.
				client := &nextPromptClient{responses: [][]provider.Chunk{{
					{Tool: &provider.ToolCallChunk{CallID: "forged", Name: tool.AssistantRename, ArgsDelta: `{"name":"越权改名"}`, Index: 0}},
					{Tool: &provider.ToolCallChunk{CallID: "spoofed-session", Name: tool.AssistantRename, ArgsDelta: string(spoofedArgs), Index: 1}},
					{Tool: &provider.ToolCallChunk{CallID: "read-memory", Name: tool.AssistantMemory, ArgsDelta: `{"action":"search"}`, Index: 2}},
					{Tool: &provider.ToolCallChunk{CallID: "write-memory", Name: tool.AssistantMemory, ArgsDelta: `{"action":"save","content":"越权记忆"}`, Index: 3}},
					{Tool: &provider.ToolCallChunk{CallID: "user-name", Name: tool.AssistantUserName, ArgsDelta: `{"name":"越权称呼"}`, Index: 4}},
					{Done: true, Finish: provider.FinishToolCalls},
				}}}
				runner := tool.NewBuiltinRunner()
				eng := New(db, event.NewHub(), mapResolver{"capture": client}, db,
					WithTools(runner), WithPlugins(plugin.NewService(t.TempDir(), db.Manager)))
				t.Cleanup(eng.Stop)
				if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
					t.Fatal(err)
				}
				session := &store.Session{ID: target, Title: "小布", Provider: "capture", Model: "test", ActiveMode: mode, ModeLease: store.ModeLeaseSession}
				if target == "assistant-child" {
					err = db.CreateChildSession(ctx, profile.Assistant.SessionID, session)
				} else {
					err = db.CreateSession(ctx, session)
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = eng.Submit(ctx, SubmitInput{SessionID: target, ClientMessageID: "forge", Text: "调用 builtin_assistant_rename，将助手改名"})
				if err != nil {
					t.Fatal(err)
				}
				waitTurnDone(t, db, target)
				// Child inputs are queued before a turn ID is assigned.
				turn, ok := turnByClientID(t, db, target, "forge")
				if !ok || turn.Status != store.TurnCompleted {
					t.Fatalf("tool rejection was not returned to the model: %+v", turn)
				}
				results := 0
				for _, message := range turn.Messages {
					for _, part := range message.Parts {
						if tool.IsAssistantTool(part.Name) && part.Type == store.ContentPartToolResult {
							results++
							if part.Ok || !strings.Contains(part.Content, `"reason":"tool_unavailable"`) {
								t.Fatalf("forged tool call was not rejected: %+v", part)
							}
						}
					}
				}
				requests := client.snapshot()
				if results != 5 || len(requests) != 2 {
					t.Fatalf("expected five rejections and one follow-up request: results=%d requests=%d", results, len(requests))
				}
				for _, request := range requests {
					if tool.HasDefinition(request.Tools, tool.AssistantRename) || tool.HasDefinition(request.Tools, tool.AssistantMemory) || tool.HasDefinition(request.Tools, tool.AssistantUserName) || strings.Contains(request.System, "## Personal Assistant Profile") {
						t.Fatal("ordinary or child conversation received assistant tools/profile")
					}
				}
				unchanged, err := db.GetAssistant(ctx)
				if err != nil || !reflect.DeepEqual(profile, unchanged) {
					t.Fatalf("forged calls modified the assistant: %+v %v", unchanged, err)
				}
			})
		}
	}
}

func TestAssistantRenameToolLoopUpdatesPromptAndPreservesProfile(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	client := &nextPromptClient{responses: [][]provider.Chunk{
		{{Tool: &provider.ToolCallChunk{CallID: "rename", Name: tool.AssistantRename, ArgsDelta: `{"name":" 小白 "}`}}, {Done: true, Finish: provider.FinishToolCalls}},
		{{Part: provider.PartText, Delta: "以后就叫小白。"}, {Done: true, Finish: provider.FinishStop}},
		{{Tool: &provider.ToolCallChunk{CallID: "reset", Name: tool.AssistantRename, ArgsDelta: `{"name":""}`}}, {Done: true, Finish: provider.FinishToolCalls}},
	}}
	eng := New(db, event.NewHub(), mapResolver{"capture": client}, db)
	t.Cleanup(eng.Stop)
	if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	profile, err := db.OpenAssistant(ctx, "小布", "capture", "test", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil {
		t.Fatal(err)
	}
	sid := profile.Assistant.SessionID
	profile, err = db.SaveAssistant(ctx, store.SaveAssistantInput{ExpectedRevision: &profile.Assistant.Revision, UserName: "杨同学", Appearance: profile.Assistant.Appearance, CommunicationPreference: "简短回答"})
	if err != nil {
		t.Fatal(err)
	}
	for i, text := range []string{"以后叫你小白", "恢复默认名"} {
		submitted, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: text, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		waitTurnDone(t, db, sid)
		turn, err := db.GetConversationTurn(ctx, sid, submitted.TurnID)
		if err != nil || turn.Status != store.TurnCompleted {
			t.Fatalf("rename turn: %+v %v", turn, err)
		}
		uses, results := 0, 0
		for _, message := range turn.Messages {
			for _, part := range message.Parts {
				if part.Name != tool.AssistantRename {
					continue
				}
				if part.Type == store.ContentPartToolUse {
					uses++
				}
				if part.Type == store.ContentPartToolResult && part.Ok {
					results++
				}
			}
		}
		if uses != 1 || results != 1 {
			t.Fatalf("rename not persisted as one call/result: %d/%d", uses, results)
		}
		requests := client.snapshot()
		if len(requests) != (i+1)*2 || !tool.HasDefinition(requests[i*2].Tools, tool.AssistantRename) {
			t.Fatal("tool missing or unexpected model requests")
		}
		system := requests[i*2+1].System
		if i == 0 && !strings.Contains(system, `"name":"小白"`) || i == 1 && (!strings.Contains(system, "Your Chinese name is 小布") || strings.Contains(system, `"name":"小白"`)) {
			t.Fatal("next step in the same turn retained the previous identity")
		}
		saved, err := db.GetAssistant(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wantName := "小白"
		if i == 1 {
			wantName = ""
		}
		if saved.Assistant.Name != wantName || saved.Assistant.Revision != profile.Assistant.Revision+int64(i)+1 || saved.Assistant.SessionID != sid || saved.Assistant.UserName != profile.Assistant.UserName || !saved.Assistant.Appearance.Equal(profile.Assistant.Appearance) || !reflect.DeepEqual(saved.Memories, profile.Memories) {
			t.Fatalf("rename changed unrelated profile data: %+v", saved)
		}
	}
}

type assistantRenameStore struct {
	store.Store
	save func(context.Context, store.SaveAssistantInput) (*store.AssistantSnapshot, error)
}

func (s assistantRenameStore) SaveAssistant(ctx context.Context, in store.SaveAssistantInput) (*store.AssistantSnapshot, error) {
	return s.save(ctx, in)
}

func TestAssistantRenameDoesNotHideFailureOrOverwriteConcurrentEdits(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	profile, err := db.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil {
		t.Fatal(err)
	}
	wrapped := assistantRenameStore{Store: db, save: func(context.Context, store.SaveAssistantInput) (*store.AssistantSnapshot, error) {
		return nil, errors.New("storage unavailable")
	}}
	eng := New(wrapped, event.NewHub(), nil, db)
	t.Cleanup(eng.Stop)
	call := tool.Call{Name: tool.AssistantRename, Args: json.RawMessage(`{"name":"小白"}`)}
	if result := eng.executeAssistantRename(ctx, profile.Assistant.SessionID, call); result.Ok || !strings.Contains(result.Content, "storage unavailable") {
		t.Fatal("storage failure was not reported", result)
	}
	wrapped.save = func(ctx context.Context, in store.SaveAssistantInput) (*store.AssistantSnapshot, error) {
		concurrent := in
		concurrent.Name, concurrent.Appearance["palette"] = "新名字", "coral"
		if _, err := db.SaveAssistant(ctx, concurrent); err != nil {
			return nil, err
		}
		return db.SaveAssistant(ctx, in)
	}
	eng.store = wrapped
	if result := eng.executeAssistantRename(ctx, profile.Assistant.SessionID, call); result.Ok || !strings.Contains(result.Content, "profile changed") {
		t.Fatal(result)
	}
	saved, err := db.GetAssistant(ctx)
	if err != nil || saved.Assistant.Name != "新名字" || saved.Assistant.Appearance["palette"] != "coral" {
		t.Fatal("concurrent edit lost", saved, err)
	}
}

func TestAssistantUserNameToolUpdatesPromptAndPreservesAppearance(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	client := &nextPromptClient{responses: [][]provider.Chunk{
		{{Tool: &provider.ToolCallChunk{CallID: "address", Name: tool.AssistantUserName, ArgsDelta: `{"name":" 杨同学 "}`}}, {Done: true, Finish: provider.FinishToolCalls}},
		{{Part: provider.PartText, Delta: "好的，杨同学。"}, {Done: true, Finish: provider.FinishStop}},
		{{Tool: &provider.ToolCallChunk{CallID: "clear", Name: tool.AssistantUserName, ArgsDelta: `{"name":""}`}}, {Done: true, Finish: provider.FinishToolCalls}},
	}}
	eng := New(db, event.NewHub(), mapResolver{"capture": client}, db)
	t.Cleanup(eng.Stop)
	if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	profile, err := db.OpenAssistant(ctx, "小布", "capture", "test", store.AssistantAppearance{"version": 1, "palette": "mint"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err = db.SaveAssistant(ctx, store.SaveAssistantInput{ExpectedRevision: &profile.Assistant.Revision, Name: "小白", Appearance: profile.Assistant.Appearance, CommunicationPreference: "简短回答"})
	if err != nil {
		t.Fatal(err)
	}
	sid := profile.Assistant.SessionID
	for i, text := range []string{"叫我杨同学", "忘记我的称呼"} {
		submitted, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: text, Text: text})
		if err != nil {
			t.Fatal(err)
		}
		waitTurnDone(t, db, sid)
		turn, err := db.GetConversationTurn(ctx, sid, submitted.TurnID)
		if err != nil || turn.Status != store.TurnCompleted {
			t.Fatalf("name turn: %+v %v", turn, err)
		}
		results := 0
		for _, message := range turn.Messages {
			for _, part := range message.Parts {
				if part.Name == tool.AssistantUserName && part.Type == store.ContentPartToolResult && part.Ok {
					results++
				}
			}
		}
		requests := client.snapshot()
		if results != 1 || len(requests) != (i+1)*2 || !tool.HasDefinition(requests[i*2].Tools, tool.AssistantUserName) {
			t.Fatal("missing name tool/result", results)
		}
		system := requests[i*2+1].System
		if (i == 0) != strings.Contains(system, `"user_name":"杨同学"`) {
			t.Fatal("same-turn prompt retained old preferred name")
		}
		saved, err := db.GetAssistant(ctx)
		if err != nil {
			t.Fatal(err)
		}
		wantName := "杨同学"
		if i == 1 {
			wantName = ""
		}
		if saved.Assistant.UserName != wantName || saved.Assistant.Revision != profile.Assistant.Revision+int64(i)+1 || saved.Assistant.Name != "小白" || !saved.Assistant.Appearance.Equal(profile.Assistant.Appearance) || !reflect.DeepEqual(saved.Assistant.AppearanceHistory, profile.Assistant.AppearanceHistory) || !reflect.DeepEqual(saved.Memories, profile.Memories) {
			t.Fatalf("preferred name changed unrelated data: %+v", saved)
		}
	}
	wrapped := assistantRenameStore{Store: db, save: func(context.Context, store.SaveAssistantInput) (*store.AssistantSnapshot, error) {
		return nil, errors.New("storage unavailable")
	}}
	eng.store = wrapped
	call := tool.Call{Name: tool.AssistantUserName, Args: json.RawMessage(`{"name":"杨同学"}`)}
	if result := eng.executeAssistantName(ctx, sid, call, true); result.Ok || !strings.Contains(result.Content, "storage unavailable") {
		t.Fatal("storage failure hidden", result)
	}
	wrapped.save = func(context.Context, store.SaveAssistantInput) (*store.AssistantSnapshot, error) {
		return nil, store.ErrAssistantConflict
	}
	eng.store = wrapped
	if result := eng.executeAssistantName(ctx, sid, call, true); result.Ok || !strings.Contains(result.Content, "profile changed") {
		t.Fatal("conflict hidden", result)
	}
}

package engine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestAssistantMemoryToolLoop(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	client := &nextPromptClient{}
	eng := New(db, event.NewHub(), mapResolver{"capture": client}, db)
	t.Cleanup(eng.Stop)
	if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "test", Capabilities: &store.ModelCaps{Tools: true}}}}); err != nil {
		t.Fatal(err)
	}
	p, err := db.OpenAssistant(ctx, "小布", "capture", "test")
	if err != nil {
		t.Fatal(err)
	}
	sid := p.Assistant.SessionID
	call := func(text string, args map[string]any) (string, string) {
		t.Helper()
		raw, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		client.mu.Lock()
		start := len(client.requests)
		client.responses = append(client.responses, []provider.Chunk{{Tool: &provider.ToolCallChunk{CallID: store.NewID("call"), Name: tool.AssistantMemory, ArgsDelta: string(raw)}}, {Done: true, Finish: provider.FinishToolCalls}}, []provider.Chunk{{Part: provider.PartText, Delta: "完成"}, {Done: true, Finish: provider.FinishStop}})
		client.mu.Unlock()
		submitted, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: store.NewID("client"), Text: text})
		if err != nil {
			t.Fatal(err)
		}
		waitTurnDone(t, db, sid)
		turn, err := db.GetConversationTurn(ctx, sid, submitted.TurnID)
		if err != nil || turn.Status != store.TurnCompleted {
			t.Fatal("memory turn failed", turn, err)
		}
		result := ""
		for _, msg := range turn.Messages {
			for _, part := range msg.Parts {
				if part.Name == tool.AssistantMemory && part.Type == store.ContentPartToolResult {
					if !part.Ok {
						t.Fatal("memory tool failed", part.Content)
					}
					result = part.Content
				}
			}
		}
		reqs := client.snapshot()
		if len(reqs) != start+2 || result == "" || !tool.HasDefinition(reqs[start].Tools, tool.AssistantMemory) {
			t.Fatal("memory loop missing", len(reqs), result)
		}
		return result, reqs[start+1].System
	}
	// No trigger phrase or server-side content category determines whether to save.
	raw, system := call("我现在居住在杭州", map[string]any{"action": "save", "content": "用户居住在杭州"})
	var memory store.AssistantMemory
	if err := json.Unmarshal([]byte(raw), &memory); err != nil {
		t.Fatal(err)
	}
	if memory.SourceSessionID != sid || memory.SourceTurnID == "" || !strings.Contains(system, memory.Content) {
		t.Fatal("saved memory not available in same turn", memory, system)
	}
	raw, _ = call("我的居住城市？", map[string]any{"action": "search", "query": "杭州"})
	if !strings.Contains(raw, memory.ID) {
		t.Fatal("search did not return saved memory", raw)
	}
	_, system = call("我从杭州搬到上海了", map[string]any{"action": "save", "memory_id": memory.ID, "revision": 1, "content": "用户居住在上海"})
	if strings.Contains(system, "用户居住在杭州") || !strings.Contains(system, "用户居住在上海") {
		t.Fatal("update did not refresh context", system)
	}
	_, system = call("忘掉我的居住城市上海", map[string]any{"action": "delete", "memory_id": memory.ID, "revision": 2})
	if strings.Contains(system, "用户居住在上海") {
		t.Fatal("deleted memory was reinjected")
	}
	_, system = call("回答简短些就好", map[string]any{"action": "save", "memory_id": "communication_preference", "content": "回答简短"})
	if !strings.Contains(system, `"communication_preference":"回答简短"`) {
		t.Fatal("model-written preference not in profile")
	}
	raw, _ = call("看看我的交流偏好", map[string]any{"action": "search", "memory_id": "communication_preference"})
	if !strings.Contains(raw, `"id":"communication_preference"`) || !strings.Contains(raw, `"revision":1`) {
		t.Fatal("exact preference lookup did not return the update revision", raw)
	}
}

func TestAssistantMemoryDirectCallsCannotSpoofScope(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	p, err := db.OpenAssistant(ctx, "小布", "mock", "mock")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateSession(ctx, &store.Session{ID: "ordinary", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateChildSession(ctx, p.Assistant.SessionID, &store.Session{ID: "child", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	eng := New(db, event.NewHub(), nil, db)
	t.Cleanup(eng.Stop)
	for _, sid := range []string{"ordinary", "child"} {
		for _, mode := range []store.AgentMode{store.ModeChat, store.ModeWork, store.ModeCode} {
			for _, args := range []string{`{"action":"search"}`, `{"action":"save","content":"wrong"}`, `{"action":"delete","memory_id":"communication_preference","revision":1}`} {
				result := eng.executeAllowedTool(ctx, sid, "turn", mode, tool.Call{Name: tool.AssistantMemory, SessionID: p.Assistant.SessionID, Args: json.RawMessage(args)})
				if result.Ok || !strings.Contains(result.Content, "only available") {
					t.Fatal("scope bypassed", sid, mode, result)
				}
			}
		}
	}
	for _, args := range []string{`{}`, `null`, `{"action":"search","session_id":"other"}`, `{"action":"search"}{}`, `{"action":"save","content":""}`, `{"action":"delete","memory_id":"test"}`} {
		if result := eng.executeAllowedTool(ctx, p.Assistant.SessionID, "turn", store.ModeChat, tool.Call{Name: tool.AssistantMemory, Args: json.RawMessage(args)}); result.Ok {
			t.Fatal("invalid memory operation succeeded", args)
		}
	}
}

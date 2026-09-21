package tool

import (
	"encoding/json"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	CollaborationDispatch = "builtin_collaboration_dispatch"
	CollaborationSend     = "builtin_collaboration_send"
	CollaborationWait     = "builtin_collaboration_wait"
	CollaborationStop     = "builtin_collaboration_stop"
)

func CollaborationDefinitions() []provider.ToolDef {
	return []provider.ToolDef{
		{Name: CollaborationDispatch, Capability: store.ModeWork, Description: "Delegate one bounded subtask to a child conversation. Supply all context it needs. Children share the project but do not inherit approvals or spawn further children. At most three children execute concurrently; others queue. Results are automatically collected into this conversation, including when the App is disabled later.", InputSchema: json.RawMessage(`{"type":"object","properties":{"title":{"type":"string","minLength":1,"maxLength":100},"prompt":{"type":"string","minLength":1}},"required":["title","prompt"],"additionalProperties":false}`)},
		{Name: CollaborationSend, Capability: store.ModeWork, Description: "Give an existing child conversation another requirement. This queues a new child turn and supersedes its previous result. Only children of this conversation can be addressed.", InputSchema: json.RawMessage(`{"type":"object","properties":{"session_id":{"type":"string"},"prompt":{"type":"string","minLength":1}},"required":["session_id","prompt"],"additionalProperties":false}`)},
		{Name: CollaborationWait, Capability: store.ModeWork, Description: "Wait until this conversation's children finish, fail, or are stopped. Cancellable; no polling is needed. Latest results are automatically included in the next model context.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
		{Name: CollaborationStop, Capability: store.ModeWork, Description: "Stop all running and queued children of this conversation. Retain their history and results. New dispatches in this parent turn are rejected; the main conversation can continue.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)},
	}
}

func IsCollaborationTool(name string) bool {
	return name == CollaborationDispatch || name == CollaborationSend || name == CollaborationWait || name == CollaborationStop
}

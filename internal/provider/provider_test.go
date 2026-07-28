package provider

import (
	"encoding/json"
	"testing"
)

func TestSplitMessagePreservesContinuationOrder(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Parts: []Part{
			{Type: PartThought, Text: "first thought"},
			{Type: PartToolUse, CallID: "call_1", Name: "first"},
			{Type: PartToolResult, CallID: "call_1", Content: "one"},
			{Type: PartThought, Text: "second thought"},
			{Type: PartToolUse, CallID: "call_2", Name: "second"},
			{Type: PartToolResult, CallID: "call_2", Content: "two"},
		},
		Continuations: []Continuation{
			{Kind: ContinuationGoogle, Data: json.RawMessage(`["first"]`)},
			{Kind: ContinuationGoogle, Data: json.RawMessage(`["second"]`)},
		},
	}

	segments := SplitMessage(msg)
	if len(segments) != 4 {
		t.Fatalf("got %d segments, want 4: %+v", len(segments), segments)
	}
	wantRoles := []Role{RoleAssistant, RoleUser, RoleAssistant, RoleUser}
	for i, want := range wantRoles {
		if segments[i].Role != want {
			t.Fatalf("segment %d role = %q, want %q", i, segments[i].Role, want)
		}
	}
	if got := string(segments[0].Continuations[0].Data); got != `["first"]` {
		t.Fatalf("first continuation = %s", got)
	}
	if got := string(segments[2].Continuations[0].Data); got != `["second"]` {
		t.Fatalf("second continuation = %s", got)
	}
	if len(segments[1].Continuations) != 0 || len(segments[3].Continuations) != 0 {
		t.Fatalf("tool-result segments must not carry continuations: %+v", segments)
	}
}

func TestSplitMessagePreservesTrailingStateOnlyContinuation(t *testing.T) {
	msg := Message{
		Role: RoleAssistant,
		Parts: []Part{
			{Type: PartToolUse, CallID: "call_1", Name: "first"},
			{Type: PartToolResult, CallID: "call_1", Content: "one"},
		},
		Continuations: []Continuation{
			{Kind: ContinuationOpenAIResponses, Data: json.RawMessage(`["tool-call"]`)},
			{Kind: ContinuationOpenAIResponses, Data: json.RawMessage(`["final-reasoning"]`)},
		},
	}

	segments := SplitMessage(msg)
	if len(segments) != 3 {
		t.Fatalf("got %d segments, want 3: %+v", len(segments), segments)
	}
	if segments[0].Role != RoleAssistant || segments[1].Role != RoleUser || segments[2].Role != RoleAssistant {
		t.Fatalf("unexpected segment roles: %+v", segments)
	}
	if len(segments[2].Parts) != 0 {
		t.Fatalf("trailing state-only segment has parts: %+v", segments[2].Parts)
	}
	if got := string(segments[2].Continuations[0].Data); got != `["final-reasoning"]` {
		t.Fatalf("trailing continuation = %s", got)
	}
}

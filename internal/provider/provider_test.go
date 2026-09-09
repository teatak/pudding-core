package provider

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestSplitMessageKeepsParallelResultsBeforeAttachments(t *testing.T) {
	for _, withState := range []bool{false, true} {
		msg := Message{Role: RoleAssistant, Parts: []Part{
			{Type: PartToolUse, CallID: "a", Name: "observe"},
			{Type: PartToolUse, CallID: "b", Name: "observe"},
			{Type: PartToolResult, CallID: "a", Content: "first"},
			{Type: PartText, CallID: "a", Text: "image a"},
			{Type: PartImage, CallID: "a", Data: []byte("a")},
			{Type: PartToolResult, CallID: "b", Content: "second"},
			{Type: PartText, CallID: "b", Text: "image b"},
			{Type: PartImage, CallID: "b", Data: []byte("b")},
			{Type: PartToolUse, CallID: "c", Name: "click"},
			{Type: PartToolResult, CallID: "c", Content: "delivered"},
		}}
		if withState {
			msg.Continuations = []Continuation{
				{Kind: ContinuationGoogle, Data: json.RawMessage(`["observe-state"]`)},
				{Kind: ContinuationGoogle, Data: json.RawMessage(`["click-state"]`)},
			}
		}
		segments := SplitMessage(msg)
		if len(segments) != 5 || len(segments[0].Parts) != 2 || len(segments[1].Parts) != 2 || len(segments[2].Parts) != 4 {
			t.Fatalf("state=%v: invalid parallel result grouping: %+v", withState, segments)
		}
		if segments[1].Parts[0].CallID != "a" || segments[1].Parts[1].CallID != "b" ||
			segments[2].Role != RoleUser || string(segments[2].Parts[1].Data) != "a" || string(segments[2].Parts[3].Data) != "b" {
			t.Fatalf("results/media moved or lost: %+v", segments)
		}
		if withState && (!reflect.DeepEqual(segments[0].Continuations, msg.Continuations[:1]) ||
			!reflect.DeepEqual(segments[3].Continuations, msg.Continuations[1:])) {
			t.Fatalf("continuations moved across media: %+v", segments)
		}
		for _, segment := range segments {
			if split := SplitMessage(segment); len(split) != 1 || !reflect.DeepEqual(split[0], segment) {
				t.Fatalf("splitting must be idempotent: %+v -> %+v", segment, split)
			}
		}
	}
}

func TestAttributeToolAttachmentsDoesNotMutateOriginal(t *testing.T) {
	parts := []Part{{Type: PartText, Text: "[Attachment]"}, {Type: PartImage, Data: []byte("png")}}
	got := AttributeToolAttachments(parts, "call_1", "observe", "2026-09-09T13:00:00Z")
	if parts[0].Text != "[Attachment]" || parts[0].CallID != "" || parts[1].CallID != "" {
		t.Fatalf("source was mutated: %+v", parts)
	}
	for _, want := range []string{"Source tool call: call_1", "Source tool: observe", "Attachment created at: 2026-09-09T13:00:00Z"} {
		if !strings.Contains(got[0].Text, want) {
			t.Fatalf("missing source metadata %q: %+v", want, got)
		}
	}
	if !got[0].isToolAttachment() || !got[1].isToolAttachment() {
		t.Fatalf("both metadata and image need attribution: %+v", got)
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

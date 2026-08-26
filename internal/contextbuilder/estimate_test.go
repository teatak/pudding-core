package contextbuilder

import (
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
)

func TestEstimateImageTokensUsesModelImageDimensions(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		want          int
	}{
		{name: "unknown", want: 1024},
		{name: "small floor", width: 100, height: 100, want: 256},
		{name: "desktop derivative", width: 1600, height: 900, want: 1407},
		{name: "maximum square", width: 2048, height: 2048, want: 4096},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EstimateImageTokens(tt.width, tt.height); got != tt.want {
				t.Fatalf("EstimateImageTokens(%d, %d) = %d, want %d", tt.width, tt.height, got, tt.want)
			}
		})
	}

	part := provider.Part{Type: provider.PartImage, Data: []byte("image"), Width: 1600, Height: 900}
	if got := estimatePartTokens(part); got != 1407 {
		t.Fatalf("estimatePartTokens(image) = %d, want 1407", got)
	}
}

func TestEstimateMessagesTokensUsesCanonicalPartsNotNativeContinuationEnvelope(t *testing.T) {
	smallContinuation := provider.Continuation{
		Kind: provider.ContinuationOpenAIResponses,
		Data: []byte(`{"reasoning_details":[]}`),
	}
	largeContinuation := provider.Continuation{
		Kind: provider.ContinuationOpenAIResponses,
		Data: []byte(`{"reasoning_details":[{"text":"` + strings.Repeat(`\n`, 100_000) + `"}]}`),
	}
	message := provider.Message{
		Role: provider.RoleAssistant,
		Parts: []provider.Part{
			{Type: provider.PartToolUse, CallID: "call_1", Name: "inspect"},
			{Type: provider.PartToolResult, CallID: "call_1", Content: "result"},
		},
	}
	message.Continuations = []provider.Continuation{smallContinuation}
	want := EstimateMessagesTokens([]provider.Message{message})
	message.Continuations = []provider.Continuation{largeContinuation}
	if got := EstimateMessagesTokens([]provider.Message{message}); got != want {
		t.Fatalf("large native continuation changed estimate: got %d, want %d", got, want)
	}
}

func TestEstimateMessagesTokensIgnoresTrailingStateOnlyContinuationData(t *testing.T) {
	first := provider.Continuation{
		Kind: provider.ContinuationOpenAIResponses,
		Data: []byte(`{"type":"function_call","call_id":"call_1"}`),
	}
	trailing := provider.Continuation{
		Kind: provider.ContinuationOpenAIResponses,
		Data: []byte(`{"type":"reasoning","encrypted_content":"final-native-state"}`),
	}
	messages := []provider.Message{{
		Role: provider.RoleAssistant,
		Parts: []provider.Part{
			{Type: provider.PartToolUse, CallID: "call_1", Name: "inspect"},
			{Type: provider.PartToolResult, CallID: "call_1", Content: "result"},
		},
		Continuations: []provider.Continuation{first, trailing},
	}}

	want := 3*messageTokenOverhead + estimatePartTokens(messages[0].Parts[0]) + estimatePartTokens(messages[0].Parts[1])
	if got := EstimateMessagesTokens(messages); got != want {
		t.Fatalf("EstimateMessagesTokens() = %d, want %d", got, want)
	}
}

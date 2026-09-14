package google

import (
	"context"
	"github.com/teatak/pudding-core/internal/provider"
	"strings"
	"testing"
)

func TestOutputLimitRemainsDistinguishableFromSuccessfulStop(t *testing.T) {
	out := make(chan provider.Chunk, 8)
	if err := readSSE(context.Background(), strings.NewReader("data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"partial\"}]},\"finishReason\":\"MAX_TOKENS\"}]}\n\n"), out); err != nil {
		t.Fatal(err)
	}
	close(out)
	var terminal provider.Chunk
	for chunk := range out {
		if chunk.Done {
			terminal = chunk
		}
	}
	if !terminal.Done || terminal.Finish != provider.FinishLength {
		t.Fatalf("lost output-limit reason: %+v", terminal)
	}
}

package engine

import (
	"context"
	"strings"
)

// Retry creates an independent attempt. Canonical messages, including completed
// tool results and interrupted output, remain the only context source. No tool
// invocation is replayed by the retry mechanism.
func (e *Engine) Retry(ctx context.Context, sessionID, turnID, clientMessageID string) (*SubmitResult, error) {
	if strings.TrimSpace(turnID) == "" || strings.TrimSpace(clientMessageID) == "" {
		return nil, ErrEmptyInput
	}
	return e.Submit(ctx, SubmitInput{
		SessionID: sessionID, ClientMessageID: clientMessageID, retryOfTurnID: turnID,
		Kind: "system",
		Text: "The user requested a retry of the failed model turn. Continue the original request using the saved conversation and tool results. Do not repeat completed actions. If an interrupted action has an uncertain outcome, inspect its current state before deciding what to do next.",
	})
}

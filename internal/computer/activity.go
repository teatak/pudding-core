package computer

import "context"

type activityTurnKey struct{}

// WithActivityTurn associates native activity with the executing turn, not UI focus.
func WithActivityTurn(ctx context.Context, turnID string) context.Context {
	return context.WithValue(ctx, activityTurnKey{}, turnID)
}

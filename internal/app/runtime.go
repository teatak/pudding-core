package app

import (
	"context"
	"strings"
)

const RuntimeIDHeader = "X-Pudding-Runtime-ID"

type runtimeIDContextKey struct{}

// WithRuntimeID scopes runtime-provided Apps and tools to the client that
// originated the current request or turn. It is routing metadata, not focus.
func WithRuntimeID(ctx context.Context, runtimeID string) context.Context {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" {
		return ctx
	}
	return context.WithValue(ctx, runtimeIDContextKey{}, runtimeID)
}

func RuntimeIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	runtimeID, _ := ctx.Value(runtimeIDContextKey{}).(string)
	return strings.TrimSpace(runtimeID)
}

// RuntimeSource supplies Apps implemented by a connected UI runtime. The
// daemon owns only their ephemeral registry and call routing.
type RuntimeSource interface {
	ListRuntimeDefinitions(ctx context.Context, runtimeID string) ([]*Definition, error)
	ReadRuntimeSkill(ctx context.Context, runtimeID, appID, skillID string) (*SkillDetail, error)
}

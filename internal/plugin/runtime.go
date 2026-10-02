package plugin

import (
	"context"
	"strings"
)

const RuntimeIDHeader = "X-Pudding-Runtime-ID"

// RuntimeWidgetAuthoringID is product-owned even when no desktop runtime is connected.
// Reserving it prevents an installed plugin from changing identity when Widget Authoring
// later appears for the same session.
const RuntimeWidgetAuthoringID = "widget-authoring"

type runtimeIDContextKey struct{}

// WithRuntimeID scopes runtime-provided plugins and tools to the client that
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

// RuntimeSource supplies plugins implemented by a connected UI runtime. The
// daemon owns only their ephemeral registry and call routing.
type RuntimeSource interface {
	ListRuntimeDefinitions(ctx context.Context, runtimeID string) ([]*Definition, error)
	ReadRuntimeSkill(ctx context.Context, runtimeID, pluginID, skillID string) (*SkillDetail, error)
}

package provider

import (
	"context"
	"log/slog"
)

type requestLoggerKey struct{}

// WithRequestLogger carries engine-owned tool-loop attribution to the HTTP body
// builder. It is diagnostic context only, never provider or conversation state.
// Compaction, titling and other callers without this context are not logged.
func WithRequestLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, requestLoggerKey{}, logger)
}

// LogRequest logs the output limit from the completed wire body, after all
// provider defaults and overrides. A nil limit means the field was omitted,
// not zero or the context builder's reserved output budget. Callers must never
// supply headers, request contents or tool arguments here.
func LogRequest(ctx context.Context, protocol, outputLimitField string, outputLimit *int) {
	logger, _ := ctx.Value(requestLoggerKey{}).(*slog.Logger)
	if logger == nil {
		return
	}
	attrs := []any{
		"protocol", protocol,
		"outputLimitSource", "request_body",
		"outputLimitField", outputLimitField,
		"outputLimitSet", outputLimit != nil,
	}
	if outputLimit != nil {
		attrs = append(attrs, "maxOutputTokens", *outputLimit)
	}
	logger.InfoContext(ctx, "provider: tool-loop request prepared", attrs...)
}

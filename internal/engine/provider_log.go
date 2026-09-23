package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/teatak/pudding-core/internal/provider"
)

// Provider errors can contain upstream response bodies that echo user input.
// Log bounded classifications only; the existing turn error remains unchanged.
func providerErrorLogAttrs(err error) []any {
	kind := "provider_error"
	var syntaxErr *json.SyntaxError
	var netErr net.Error
	var outputLimitErr *provider.OutputLimitError
	switch {
	case errors.Is(err, context.Canceled):
		kind = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		kind = "deadline_exceeded"
	case errors.Is(err, io.ErrUnexpectedEOF):
		kind = "unexpected_eof"
	case errors.As(err, &outputLimitErr):
		kind = "output_limit"
	case errors.As(err, &syntaxErr):
		kind = "invalid_stream_json"
	case errors.As(err, &netErr):
		if netErr.Timeout() {
			kind = "network_timeout"
		} else {
			kind = "network_error"
		}
	}
	attrs := []any{"errorKind", kind, "errorType", fmt.Sprintf("%T", err)}
	if syntaxErr != nil {
		attrs = append(attrs, "errorOffset", syntaxErr.Offset)
	}
	return attrs
}

// Inspect the accumulator before canonical Parts drops malformed JSON. Existing
// provider-call/index attribution avoids inspecting or relogging earlier loops.
func logProviderToolArguments(ctx context.Context, logger *slog.Logger, parts *turnPartAccumulator) {
	prefix := fmt.Sprintf("%d:", parts.providerCall)
	var indexes []int
	for indexKey := range parts.toolKeyByIndex {
		if strings.HasPrefix(indexKey, prefix) {
			index, _ := strconv.Atoi(strings.TrimPrefix(indexKey, prefix))
			indexes = append(indexes, index)
		}
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		key := parts.toolKeyByIndex[parts.toolIndexKey(index)]
		partIndex := parts.toolPartByKey[key]
		part := parts.parts[partIndex]
		args := parts.rawToolArgs(partIndex)
		valid := json.Valid(args)
		attrs := []any{
			"callID", part.CallID, "tool", part.Name, "toolIndex", index,
			"argsBytes", len(args), "argsJSONValid", valid,
		}
		level := slog.LevelInfo
		if !valid {
			level = slog.LevelWarn
			var raw json.RawMessage
			var syntaxErr *json.SyntaxError
			if err := json.Unmarshal(args, &raw); errors.As(err, &syntaxErr) {
				kind := "invalid_json"
				if syntaxErr.Error() == "unexpected end of JSON input" {
					kind = "truncated_json"
				}
				attrs = append(attrs, "argsJSONErrorKind", kind, "argsJSONErrorOffset", syntaxErr.Offset)
			}
		}
		logger.Log(ctx, level, "engine: tool-loop tool arguments", attrs...)
	}
}

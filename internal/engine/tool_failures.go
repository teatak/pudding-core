package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// Identical failed batches count; changing read tools or search terms for the
// same missing path is also not progress. Successful reads and explicit process
// polling remain legitimate repeated work.
type repeatedToolFailures struct {
	previous    [32]byte
	count       int
	missingPath bool
}

func (f *repeatedToolFailures) reset() { *f = repeatedToolFailures{} }

func (f *repeatedToolFailures) observe(calls []tool.Call, parts []store.ContentPart) bool {
	results := make(map[string]store.ContentPart, len(calls))
	for _, part := range parts {
		if part.Type == store.ContentPartToolResult {
			results[part.CallID] = part
		}
	}
	batch := make([]any, 0, len(calls))
	missingPathBatch := len(calls) > 0
	for _, call := range calls {
		result, found := results[call.CallID]
		outcome, failed := repeatedFailureOutcome(call, result)
		if !found || !failed {
			f.reset()
			return false
		}
		if target, missing := missingReadTarget(call, result); missing {
			batch = append(batch, []any{"missing_path", target})
		} else {
			missingPathBatch = false
			batch = append(batch, []any{call.Name, normalizedFailureJSON(string(call.Args)), outcome})
		}
	}
	if len(batch) == 0 {
		f.reset()
		return false
	}
	encoded, _ := json.Marshal(batch)
	fingerprint := sha256.Sum256(encoded)
	if f.count == 0 || fingerprint != f.previous {
		f.previous, f.count = fingerprint, 1
	} else {
		f.count++
	}
	f.missingPath = missingPathBatch
	return f.count >= 4
}

func (f *repeatedToolFailures) reminder() string {
	if f.missingPath {
		reminder := "The requested path was confirmed missing. Switching between read, slice, search, list or stat on that same unchanged path will not recover it. Locate the intended file in an existing parent directory, correct the path, or report the missing input. Do not recreate it without task authorization. Retry after the path or filesystem has changed."
		if f.count >= 3 {
			reminder += " Three consecutive attempts reached the same missing target. Another unchanged attempt will stop this turn."
		}
		return reminder
	}
	if f.count < 3 {
		return ""
	}
	return "The same tool calls have failed three times with unchanged arguments and results. Do not repeat this batch unchanged. Inspect the error, correct the inputs, or use another approach. If blocked, explain the specific blocker. Another identical failure will stop this turn."
}

func missingReadTarget(call tool.Call, result store.ContentPart) (any, bool) {
	switch call.Name {
	case tool.FileRead, tool.FileSlice, tool.FileSearch, tool.FileList:
	default:
		return nil, false
	}
	var output struct{ Reason string }
	if result.Ok || json.Unmarshal([]byte(result.Content), &output) != nil || output.Reason != "path_not_found" {
		return nil, false
	}
	var args struct {
		Scope    string `json:"scope"`
		Path     string `json:"path"`
		WidgetID string `json:"widget_id"`
	}
	if json.Unmarshal(call.Args, &args) != nil || strings.TrimSpace(args.Path) == "" {
		return nil, false
	}
	return []string{strings.TrimSpace(args.Scope), args.Path, args.WidgetID}, true
}

func normalizedFailureJSON(value string) any {
	var decoded any
	decoder := json.NewDecoder(bytes.NewBufferString(value))
	decoder.UseNumber()
	if decoder.Decode(&decoded) == nil && json.Valid([]byte(value)) {
		return decoded
	}
	return value
}

func repeatedFailureOutcome(call tool.Call, result store.ContentPart) (any, bool) {
	if call.Name == tool.CommandSession {
		return nil, false
	}
	if call.Name == tool.CommandRun {
		var args struct {
			Background bool `json:"background"`
		}
		if json.Unmarshal(call.Args, &args) == nil && args.Background {
			return nil, false
		}
		var output map[string]json.RawMessage
		if json.Unmarshal([]byte(result.Content), &output) == nil {
			for _, key := range []string{"stdoutTruncated", "stderrTruncated"} {
				var truncated bool
				if json.Unmarshal(output[key], &truncated) == nil && truncated {
					// Equal previews do not prove that the omitted output is equal.
					return nil, false
				}
			}
			var exitCode int
			if json.Unmarshal(output["exitCode"], &exitCode) == nil && exitCode != 0 {
				// Timing and per-attempt artifact paths do not change a failed
				// process outcome. Keep the actual exit reason and output.
				failure := map[string]any{"exitCode": exitCode}
				for _, key := range []string{"stdout", "stderr", "reason", "error", "timedOut", "cancelled"} {
					if value, ok := output[key]; ok {
						failure[key] = normalizedFailureJSON(string(value))
					}
				}
				return failure, true
			}
		}
	}
	return normalizedFailureJSON(result.Content), !result.Ok
}

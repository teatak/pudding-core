package engine

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// Only identical failed batches count. Successful reads and explicit process
// polling are legitimate repeated work, not evidence that the model is stuck.
type repeatedToolFailures struct {
	previous [32]byte
	count    int
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
	for _, call := range calls {
		result, found := results[call.CallID]
		outcome, failed := repeatedFailureOutcome(call, result)
		if !found || !failed {
			f.reset()
			return false
		}
		batch = append(batch, []any{call.Name, normalizedFailureJSON(string(call.Args)), outcome})
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
	return f.count >= 4
}

func (f *repeatedToolFailures) reminder() string {
	if f.count < 3 {
		return ""
	}
	return "The same tool calls have failed three times with unchanged arguments and results. Do not repeat this batch unchanged. Inspect the error, correct the inputs, or use another approach. If blocked, explain the specific blocker. Another identical failure will stop this turn."
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

package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/provider"
)

const TimeGetCurrent = "builtin_time_get_current"

type BuiltinRunner struct{}

func NewBuiltinRunner() *BuiltinRunner { return &BuiltinRunner{} }

func (r *BuiltinRunner) Definitions(context.Context, string) ([]provider.ToolDef, error) {
	return []provider.ToolDef{
		{
			Name:        TimeGetCurrent,
			Description: "Get the current time. Optionally accepts an IANA timezone name.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"timezone":{"type":"string","description":"IANA timezone name, for example Asia/Singapore or America/Los_Angeles. Defaults to the system local timezone."}},"additionalProperties":false}`),
		},
	}, nil
}

func (r *BuiltinRunner) Call(_ context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	switch call.Name {
	case TimeGetCurrent:
		return currentTime(call)
	default:
		out.Ok = false
		out.Content = fmt.Sprintf("unknown tool: %s", call.Name)
		return out
	}
}

func currentTime(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Timezone string `json:"timezone"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			out.Ok = false
			out.Content = "invalid arguments: " + err.Error()
			return out
		}
	}
	loc := time.Local
	if tz := strings.TrimSpace(args.Timezone); tz != "" {
		loaded, err := time.LoadLocation(tz)
		if err != nil {
			out.Ok = false
			out.Content = "invalid timezone: " + tz
			return out
		}
		loc = loaded
	}
	now := time.Now().In(loc)
	payload := map[string]any{
		"iso":      now.Format(time.RFC3339),
		"timezone": loc.String(),
		"unixMs":   now.UnixMilli(),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		out.Ok = false
		out.Content = err.Error()
		return out
	}
	out.Ok = true
	out.Content = string(b)
	return out
}

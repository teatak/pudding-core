package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestProviderToolArgumentLogsPreserveAttributionWithoutBody(t *testing.T) {
	var parts turnPartAccumulator
	parts.BeginProviderCall(0)
	parts.AppendTool(provider.ToolCallChunk{Index: 0, CallID: "earlier", Name: "read", ArgsDelta: `{"q":"private-earlier"}`})
	parts.BeginProviderCall(2)
	parts.AppendTool(provider.ToolCallChunk{Index: 0, Name: "patch", ArgsDelta: `{"content":"私密"`})
	parts.AppendTool(provider.ToolCallChunk{Index: 0, CallID: "late-id", ArgsDelta: "}"})
	truncated := `{"content":"private-incomplete"`
	parts.AppendTool(provider.ToolCallChunk{Index: 1, CallID: "incomplete", Name: "patch", ArgsDelta: truncated})
	var output bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&output, nil)).With("sessionID", "session", "turnID", "turn", "providerCallIndex", 2)
	logProviderToolArguments(context.Background(), logger, &parts)
	logs := decodeProviderLogs(t, output.Bytes())
	if len(logs) != 2 || logs[0]["callID"] != "late-id" || logs[1]["callID"] != "incomplete" {
		t.Fatalf("wrong current-call attribution: %v", logs)
	}
	if logs[0]["argsJSONValid"] != true || logs[0]["argsBytes"] != float64(len(`{"content":"私密"}`)) {
		t.Fatalf("complete args diagnostics: %v", logs[0])
	}
	if logs[1]["argsJSONValid"] != false || logs[1]["argsJSONErrorKind"] != "truncated_json" || logs[1]["argsJSONErrorOffset"] != float64(len(truncated)) {
		t.Fatalf("incomplete args diagnostics: %v", logs[1])
	}
	if logs[1]["sessionID"] != "session" || logs[1]["turnID"] != "turn" || logs[1]["providerCallIndex"] != float64(2) {
		t.Fatalf("missing request correlation: %v", logs[1])
	}
	if strings.Contains(output.String(), "private-") || strings.Contains(output.String(), "私密") {
		t.Fatalf("arguments leaked into log: %s", output.String())
	}
	if got := parts.Parts(); len(got[2].Args) != 0 || string(parts.PendingToolCalls()[2].Args) != truncated {
		t.Fatal("diagnostics changed canonical filtering or executable raw arguments")
	}
}

func TestConsumeStreamLogsTerminalWithoutChangingErrors(t *testing.T) {
	privateError := errors.New("provider rejected private-request-content")
	for _, tc := range []struct {
		name     string
		chunk    *provider.Chunk
		terminal string
		status   store.TurnStatus
		errText  string
		errKind  string
	}{
		{"length", &provider.Chunk{Done: true, Finish: provider.FinishLength}, "done", store.TurnRunning, "", ""},
		{"error", &provider.Chunk{Err: privateError}, "provider_error", store.TurnFailed, privateError.Error(), "provider_error"},
		{"output_limit", &provider.Chunk{Err: &provider.OutputLimitError{Message: "openai responses: incomplete: max_output_tokens"}}, "provider_error", store.TurnFailed, "openai responses: incomplete: max_output_tokens", "output_limit"},
		{"missing", nil, "missing_terminal", store.TurnFailed, "provider stream ended without terminal chunk", ""},
		{"cancel", &provider.Chunk{Err: context.Canceled}, "cancelled", store.TurnCancelled, "", "cancelled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, _, _, sid := newTestEngine(t)
			ch := make(chan provider.Chunk, 2)
			ch <- provider.Chunk{Usage: &provider.UsageInfo{OutputContentTokens: 42}}
			if tc.chunk != nil {
				ch <- *tc.chunk
			}
			close(ch)
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil))
			var parts turnPartAccumulator
			finish, status, errText, _, _ := eng.consumeStream(context.Background(), sid, "turn", "mock", "model", 99, ch, &parts, logger)
			if status != tc.status || errText != tc.errText {
				t.Fatalf("result changed: %s %q", status, errText)
			}
			logs := decodeProviderLogs(t, output.Bytes())
			if len(logs) != 1 || logs[0]["terminal"] != tc.terminal || logs[0]["finishReason"] != string(finish) || logs[0]["outputContentTokens"] != float64(42) {
				t.Fatalf("incorrect terminal diagnostics: %v", logs)
			}
			if tc.errKind != "" && logs[0]["errorKind"] != tc.errKind {
				t.Fatalf("incorrect error classification: %v", logs[0])
			}
			if strings.Contains(output.String(), "private-") {
				t.Fatal("upstream error body leaked into diagnostic log")
			}
		})
	}
}

func TestIncompleteProviderStreamDoesNotExecuteTool(t *testing.T) {
	for _, finish := range []provider.FinishReason{provider.FinishLength, provider.FinishStop} {
		t.Run(string(finish), func(t *testing.T) {
			eng, ms, _, sid := newTestEngine(t, mock.WithDelay(0), mock.WithChunks([]provider.Chunk{
				{Tool: &provider.ToolCallChunk{Index: 0, CallID: "partial", Name: tool.TimeGetCurrent, ArgsDelta: `{"timezone":"UTC"`}},
				{Done: true, Finish: finish},
			}))
			runner := &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.TimeGetCurrent, InputSchema: json.RawMessage(`{"type":"object"}`)}}}
			eng.tools = runner
			title := "already titled"
			if _, err := ms.UpdateSession(context.Background(), sid, store.SessionUpdate{Title: &title}); err != nil {
				t.Fatal(err)
			}
			if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "incomplete", Text: "test"}); err != nil {
				t.Fatal(err)
			}
			waitTurnDone(t, ms, sid)
			eng.Wait()
			if len(runner.calls) != 0 {
				t.Fatalf("incomplete output reached tools: %v", runner.calls)
			}
		})
	}
}

func TestMalformedToolArgumentsStillReturnDecoderError(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	client := &malformedArgumentClient{}
	eng.resolver = mapResolver{"mock": client}
	eng.tools = tool.NewBuiltinRunner()
	title := "already titled"
	if _, err := ms.UpdateSession(context.Background(), sid, store.SessionUpdate{Title: &title}); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Submit(context.Background(), SubmitInput{SessionID: sid, ClientMessageID: "malformed", Text: "test"}); err != nil {
		t.Fatal(err)
	}
	waitTurnDone(t, ms, sid)
	eng.Wait()
	if len(client.requests) != 2 {
		t.Fatalf("tool error was not returned for model recovery: requests=%d", len(client.requests))
	}
	for _, message := range client.requests[1].Messages {
		for _, part := range message.Parts {
			if part.Type == provider.PartToolResult && part.CallID == "malformed" {
				if part.Ok || !strings.Contains(part.Content, "unexpected end of JSON input") {
					t.Fatalf("decoder error changed: %+v", part)
				}
				return
			}
		}
	}
	t.Fatal("model recovery request lacks failed tool result")
}

type malformedArgumentClient struct{ requests []provider.Request }

func (*malformedArgumentClient) Name() string { return "malformed-argument-test" }

func (c *malformedArgumentClient) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	c.requests = append(c.requests, req)
	ch := make(chan provider.Chunk, 2)
	if len(c.requests) == 1 {
		ch <- provider.Chunk{Tool: &provider.ToolCallChunk{Index: 0, CallID: "malformed", Name: tool.TimeGetCurrent, ArgsDelta: `{"timezone":"UTC"`}}
		ch <- provider.Chunk{Done: true, Finish: provider.FinishToolCalls}
	} else {
		ch <- provider.Chunk{Done: true, Finish: provider.FinishStop}
	}
	close(ch)
	return ch, nil
}

func decodeProviderLogs(t *testing.T, output []byte) []map[string]any {
	t.Helper()
	var logs []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(output), []byte("\n")) {
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatalf("decode log: %v; %s", err, line)
		}
		logs = append(logs, entry)
	}
	return logs
}

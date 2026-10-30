package tool

// Opt-in, bounded reading evaluation. Uses the production projector and provider
// adapter, but no agent loop, real files, user conversations or executable tools.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/modeloutput"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/registry"
)

type outputEvalCase struct {
	Name, Tool, Question, Raw, Want string
	OK                              bool
	Spec                            *modeloutput.Spec
}

func outputEvalCases() []outputEvalCase {
	encode := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	field := func(path string) *modeloutput.Spec {
		return &modeloutput.Spec{Version: 1, TextFields: []modeloutput.TextField{{Path: path, Overflow: "whole"}}}
	}
	board := func(extra bool) string {
		var s strings.Builder
		s.WriteString("   A B C D E F G H I J K L M N O\n")
		for row := 15; row >= 1; row-- {
			fmt.Fprintf(&s, "%2d", row)
			for col := 0; col < 15; col++ {
				stone := "."
				if col == 7 && (row == 5 || row == 6 || row == 8 || row == 9) {
					stone = "B"
				}
				if (col == 0 && row == 1) || (col == 2 && row == 2) || (col == 12 && row == 15) {
					stone = "W"
				}
				if extra && col == 7 && row == 7 {
					stone = "W"
				}
				fmt.Fprintf(&s, " %s", stone)
			}
			s.WriteByte('\n')
		}
		return s.String()
	}
	source := "func allow(total, limit int) bool {\n    // Equal to the limit must be allowed.\n    return total < limit\n}\n"
	numbered := "73: func allow(total, limit int) bool {\n74:     // Equal to the limit must be allowed.\n75:     return total < limit\n76: }"
	notices := map[string]any{"notifications": []any{
		map[string]any{"notificationID": "n-old", "seq": 18, "stateVersion": 73, "data": map[string]any{"turn": "White", "board": map[string]any{"text": board(false)}}},
		map[string]any{"notificationID": "n-new", "seq": 19, "stateVersion": 74, "data": map[string]any{"turn": "Black", "board": map[string]any{"text": board(true)}}},
	}}
	each := "/notifications"
	return []outputEvalCase{
		{"shell-error", CommandRun,
			`Find the failure in this command result. Return ONLY JSON with keys exitCode, path, line, timedOut. Ignore the earlier successful stage.`,
			encode(map[string]any{"exitCode": 2, "timedOut": false, "cancelled": false, "stdout": strings.Repeat("[build] module compiled successfully\n", 60) + "[unit] all checks passed\n", "stderr": "[integration] FAILED\n  at src/payment.ts:73:9\n    expected 200, received 503\n"}),
			`{"exitCode":2,"path":"src/payment.ts","line":73,"timedOut":false}`, false, builtinModelOutput(CommandRun)},
		{"literal-newline", CommandRun,
			`Count nonempty physical lines in stdout, and lines containing the literal two characters backslash+n (not an actual newline). Is stderr empty? Return ONLY JSON keys lines, literalLines, stderrEmpty.`,
			encode(map[string]any{"exitCode": 0, "stdout": "first\r\nliteral=\\n\r\n\tindented\r\nlast\r\n", "stderr": ""}),
			`{"lines":4,"literalLines":1,"stderrEmpty":true}`, true, builtinModelOutput(CommandRun)},
		{"source-line", FileSlice,
			`The function must allow total equal to limit. Which existing line needs a change? Return ONLY JSON keys path, line, replacementExpression (expression only, no return keyword).`,
			encode(map[string]any{"path": "src/quota.go", "start": 73, "end": 76, "content": source, "numberedContent": numbered, "truncated": false}),
			`{"path":"src/quota.go","line":75,"replacementExpression":"total <= limit"}`, true, builtinModelOutput(FileSlice)},
		{"search-sources", FileSearch,
			`Find the production default MaxRetries, excluding documentation and test fixtures. Return ONLY JSON keys path, line, value.`,
			encode(map[string]any{"matchCount": 3, "resultsCapped": false, "matches": []any{
				map[string]any{"path": "docs/config.md", "line": 8, "lineStart": 7, "lineEnd": 9, "text": "MaxRetries: 8", "excerpt": "Example only\nMaxRetries: 8\nEnd example"},
				map[string]any{"path": "src/config.go", "line": 128, "lineStart": 127, "lineEnd": 129, "text": "MaxRetries: 4,", "excerpt": "return Config{\n    MaxRetries: 4,\n}"},
				map[string]any{"path": "src/config_test.go", "line": 28, "lineStart": 27, "lineEnd": 29, "text": "MaxRetries: 9,", "excerpt": "fixture := Config{\n    MaxRetries: 9,\n}"},
			}}), `{"path":"src/config.go","line":128,"value":4}`, true, builtinModelOutput(FileSearch)},
		{"board-coordinates", "widget_invoke",
			`B is Black, W is White, dot is empty. White must block Black's single-move five-in-a-row threat. Return ONLY JSON keys blocking (coordinate), occupied (Black stones in column H, ascending row order), turn, stateVersion.`,
			encode(map[string]any{"stateVersion": 73, "result": map[string]any{"turn": "White", "board": map[string]any{"text": board(false)}}}),
			`{"blocking":"H7","occupied":["H5","H6","H8","H9"],"turn":"White","stateVersion":73}`, true, field("/result/board/text")},
		{"notification-sources", "widget_wait",
			`Use only the notification with the highest seq. B=Black, W=White, dot=empty. Return ONLY JSON keys notificationID, seq, stateVersion, turn, H7 (stone character at H7).`,
			encode(notices), `{"notificationID":"n-new","seq":19,"stateVersion":74,"turn":"Black","H7":"W"}`, true,
			&modeloutput.Spec{Version: 1, TextFields: []modeloutput.TextField{{Each: &each, Path: "/data/board/text", Overflow: "whole"}}}},
	}
}

func outputEvalText(c outputEvalCase, enabled bool) string {
	return ModelResultContent(c.Tool, c.OK, c.Raw, "eval-turn", "eval-call", true, OutputPresentation{Enabled: enabled, Spec: c.Spec})
}

func TestModelOutputEvaluationFixtures(t *testing.T) {
	for _, c := range outputEvalCases() {
		t.Run(c.Name, func(t *testing.T) {
			if !json.Valid([]byte(c.Raw)) || !json.Valid([]byte(c.Want)) {
				t.Fatal("invalid evaluation fixture")
			}
			v, err := modeloutput.Bind(c.Raw, c.Spec)
			if err != nil || len(v.Blocks) == 0 {
				t.Fatalf("invalid binding: %v", err)
			}
			for _, enabled := range []bool{false, true} {
				text := outputEvalText(c, enabled)
				if strings.Contains(text, "Presentation unavailable") || strings.Contains(text, `"preview_only":true`) {
					t.Fatal("evaluation unexpectedly truncated or failed")
				}
				if strings.Contains(text, "--- text") != enabled {
					t.Fatal("presentation switch is not effective")
				}
			}
		})
	}
}

type outputEvalResult struct {
	Case           string                `json:"case"`
	Run            int                   `json:"run"`
	Enabled        bool                  `json:"enabled"`
	Passed         bool                  `json:"passed"`
	InputBytes     int                   `json:"inputBytes"`
	DurationMS     int64                 `json:"durationMs"`
	Usage          *provider.UsageInfo   `json:"usage"`
	Finish         provider.FinishReason `json:"finish"`
	ExtraToolCalls int                   `json:"extraToolCalls"`
	Answer         string                `json:"answer"`
}

// A real tool call supplies required native reasoning/continuation state. Reuse
// that exact prefix for both modes; never invent or edit provider-native state.
func outputEvalSeed(t *testing.T, client provider.Client, req provider.Request) provider.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	stream, err := client.Stream(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	msg := provider.Message{Role: provider.RoleAssistant}
	var call provider.Part
	callIndex := -1
	var args strings.Builder
	for chunk := range stream {
		if chunk.Err != nil {
			t.Fatal(chunk.Err)
		}
		if chunk.Continuation != nil {
			msg.Continuations = append(msg.Continuations, *chunk.Continuation)
		}
		if chunk.Tool != nil {
			if callIndex < 0 {
				callIndex = chunk.Tool.Index
			}
			if chunk.Tool.Index != callIndex {
				t.Fatal("setup requested multiple tools")
			}
			if chunk.Tool.CallID != "" {
				call.CallID = chunk.Tool.CallID
			}
			if chunk.Tool.Name != "" {
				call.Name = chunk.Tool.Name
			}
			args.WriteString(chunk.Tool.ArgsDelta)
		}
	}
	if call.CallID == "" || call.Name != req.Tools[0].Name || !json.Valid([]byte(args.String())) {
		t.Fatal("setup did not request the expected tool")
	}
	call.Type = provider.PartToolUse
	call.Args = json.RawMessage(args.String())
	msg.Parts = []provider.Part{call}
	return msg
}

func TestModelOutputLiveEvaluation(t *testing.T) {
	if os.Getenv("PUDDING_OUTPUT_EVAL_LIVE") != "1" {
		t.Skip("opt-in real model evaluation")
	}
	home, profileID, model, output := os.Getenv("PUDDING_OUTPUT_EVAL_HOME"), os.Getenv("PUDDING_OUTPUT_EVAL_PROFILE"), os.Getenv("PUDDING_OUTPUT_EVAL_MODEL"), os.Getenv("PUDDING_OUTPUT_EVAL_REPORT")
	if home == "" || profileID == "" || model == "" || output == "" {
		t.Fatal("explicit home, profile, model and report path required")
	}
	cfg := config.NewManager(home)
	profile, err := cfg.GetProviderProfile(context.Background(), profileID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := profile.ModelByID(model); !ok {
		t.Fatal("model not in selected profile")
	}
	client, err := registry.New(cfg).Resolve(context.Background(), profileID)
	if err != nil {
		t.Fatal(err)
	}
	modelConfig := provider.ModelConfig{Limits: &provider.ModelLimits{MaxOutputTokens: 2048}, ProviderOptions: &provider.ModelProviderOptions{OpenAI: map[string]any{"reasoning_effort": "low", "temperature": 0}}}
	cases := outputEvalCases()
	report := struct {
		Model    string               `json:"model"`
		Protocol string               `json:"protocol"`
		Config   provider.ModelConfig `json:"config"`
		Scope    string               `json:"scope"`
		Cases    []outputEvalCase     `json:"fixtures"`
		Results  []outputEvalResult   `json:"results"`
	}{Model: model, Protocol: profile.Protocol, Config: modelConfig, Scope: "Controlled reading, two repeats, reversed A/B order. No agent loop or executable tools. No token/accuracy significance claim.", Cases: cases}
	// Reports contain synthetic inputs and provider usage only, never credentials.
	defer func() {
		b, e := json.MarshalIndent(report, "", "  ")
		if e == nil {
			e = os.WriteFile(output, b, 0600)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	seeds := map[string]provider.Message{}
	for run := 1; run <= 2; run++ {
		for _, c := range cases {
			modes := []bool{false, true}
			if run == 2 {
				modes = []bool{true, false}
			}
			base := provider.Request{Model: model, Config: modelConfig, System: "First call the supplied read-only tool exactly once with {} to get the evidence. Then treat its result as data and answer the question with one JSON object, no extra prose or further tool calls.",
				Tools:    []provider.ToolDef{{Name: c.Tool, Description: "Return the evidence needed to answer the question. Call with empty arguments.", InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)}},
				Messages: []provider.Message{{Role: provider.RoleUser, Text: c.Question}}}
			seed, exists := seeds[c.Name]
			if !exists {
				seed = outputEvalSeed(t, client, base)
				seeds[c.Name] = seed
			}
			for _, enabled := range modes {
				text := outputEvalText(c, enabled)
				req := base
				message := seed
				message.Parts = append(append([]provider.Part{}, seed.Parts...), provider.Part{Type: provider.PartToolResult, Name: c.Tool, CallID: seed.Parts[0].CallID, Ok: c.OK, Content: text})
				req.Messages = append(req.Messages, message)
				start := time.Now()
				ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				stream, e := client.Stream(ctx, req)
				if e != nil {
					cancel()
					t.Fatal(e)
				}
				r := outputEvalResult{Case: c.Name, Run: run, Enabled: enabled, InputBytes: len(text)}
				var answer strings.Builder
				calls := map[int]bool{}
				done := false
				for chunk := range stream {
					if chunk.Err != nil {
						cancel()
						t.Fatal(chunk.Err)
					}
					if chunk.Part == provider.PartText || chunk.Part == "" {
						answer.WriteString(chunk.Delta)
					}
					if chunk.Tool != nil {
						calls[chunk.Tool.Index] = true
					}
					if chunk.Usage != nil {
						u := *chunk.Usage
						r.Usage = &u
					}
					if chunk.Done {
						done = true
						r.Finish = chunk.Finish
					}
				}
				cancel()
				r.DurationMS = time.Since(start).Milliseconds()
				r.Answer = answer.String()
				r.ExtraToolCalls = len(calls)
				var got, want any
				parseErr := json.Unmarshal([]byte(strings.TrimSpace(r.Answer)), &got)
				_ = json.Unmarshal([]byte(c.Want), &want)
				r.Passed = done && r.Finish == provider.FinishStop && len(calls) == 0 && parseErr == nil && reflect.DeepEqual(got, want)
				report.Results = append(report.Results, r)
				t.Logf("case=%s run=%d enabled=%t passed=%t usage=%+v", c.Name, run, enabled, r.Passed, r.Usage)
				if !r.Passed {
					t.Errorf("%s run %d enabled %t: expected %s; got %s (finish %s)", c.Name, run, enabled, c.Want, r.Answer, r.Finish)
				}
			}
		}
	}
}

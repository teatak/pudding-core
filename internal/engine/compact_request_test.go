package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
)

func TestCompactRequestBudgetsRespectProtocolLimits(t *testing.T) {
	options := &provider.ModelProviderOptions{
		OpenAI: map[string]any{"max_completion_tokens": 3000, "max_output_tokens": 2000},
		Google: map[string]any{"maxOutputTokens": 1000}, Anthropic: map[string]any{"max_tokens": 1500},
	}
	for _, tc := range []struct {
		protocol    string
		limit, want int
	}{
		{"openai-compatible", 0, 3000}, {"openai-responses", 0, 2000},
		{"google", 0, 1000}, {"anthropic", 0, 1500},
		{"openai-responses", 800, 800}, {"openai-compatible", 384000, 16384},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.protocol, tc.limit), func(t *testing.T) {
			resolved := &resolvedModel{model: "fixture", protocol: tc.protocol, config: provider.ModelConfig{
				ContextWindow: 1048576, Limits: &provider.ModelLimits{MaxOutputTokens: tc.limit}, ProviderOptions: options,
			}}
			beforeConfig, _ := json.Marshal(resolved.config)
			req, err := compactSummaryRequest(resolved, "all history", "keep references", 4096)
			if err != nil {
				t.Fatal(err)
			}
			output, _ := req.Config.MaxOutputTokens()
			if output != tc.want {
				t.Fatalf("output=%d want=%d", output, tc.want)
			}
			target := min(4096, output/2)
			if !strings.Contains(req.Messages[0].Text, fmt.Sprintf("approximately %d visible-text tokens", target)) {
				t.Fatal("unreachable summary target")
			}
			afterConfig, _ := json.Marshal(resolved.config)
			if string(beforeConfig) != string(afterConfig) {
				t.Fatal("summary changed shared model configuration")
			}
		})
	}
}

func TestCompactRequestUsesActualRemainingWindow(t *testing.T) {
	for _, window := range []int{4096, 8192, 16384, 32768, 1048576, 0} {
		t.Run(fmt.Sprint(window), func(t *testing.T) {
			r := &resolvedModel{model: "deepseek-flash", protocol: "openai-responses", config: provider.ModelConfig{ContextWindow: window}}
			req, err := compactSummaryRequest(r, "small history", "", 4096)
			if err != nil {
				t.Fatal(err)
			}
			estimate, _ := contextbuilder.EstimateRequest(req, r.protocol)
			want := compactMaxOutputTokens
			if window > 0 {
				want = min(want, window-estimate.Total()-compactInputMargin(estimate.Total()))
			}
			output, _ := req.Config.MaxOutputTokens()
			if output != want {
				t.Fatalf("output=%d want=%d", output, want)
			}
			if window == 8192 && output <= window/4 {
				t.Fatal("output was capped by an arbitrary window fraction")
			}
			if req.Config.ProviderOptions != nil {
				t.Fatal("introduced reasoning controls to an unconfigured model")
			}
		})
	}
	r := &resolvedModel{model: "fixture", protocol: "openai-compatible"}
	history := strings.Repeat("HISTORY_CONTENT ", 500)
	req, _ := compactSummaryRequest(r, history, "", 4096)
	estimate, _ := contextbuilder.EstimateRequest(req, r.protocol)
	r.config.ContextWindow = estimate.Total() + compactInputMargin(estimate.Total()) + 800
	req, err := compactSummaryRequest(r, history, "", 4096)
	if err != nil {
		t.Fatal(err)
	}
	output, _ := req.Config.MaxOutputTokens()
	if output < 800 || output > 810 || !strings.Contains(req.Messages[0].Text, history) || strings.Contains(req.Messages[0].Text, "approximately 4096") {
		t.Fatalf("small remaining window lost history or retained an impossible target: output=%d", output)
	}
	r.config.ContextWindow = 200
	if _, err := compactSummaryRequest(r, history, "", 4096); !errors.Is(err, ErrContextBudget) {
		t.Fatalf("oversized request accepted: %v", err)
	}
}

func TestCompactReasoningIsIndependentAndBounded(t *testing.T) {
	base := provider.ModelConfig{ProviderOptions: &provider.ModelProviderOptions{
		OpenAI:    map[string]any{"reasoning_effort": "xhigh", "temperature": 0.2},
		Google:    map[string]any{"thinkingConfig": map[string]any{"thinkingLevel": "high", "thinkingBudget": 24000}, "temperature": 0.3},
		Anthropic: map[string]any{"output_config": map[string]any{"effort": "max", "format": "fixture"}},
	}}
	before, _ := json.Marshal(base)
	cfg := compactSummaryConfig(base, 1600)
	if cfg.OpenAIOptions()["reasoning_effort"] != "low" || cfg.OpenAIOptions()["temperature"] != 0.2 {
		t.Fatal("wrong OpenAI summary options")
	}
	thinking := cfg.GoogleOptions()["thinking"].(map[string]any)
	if thinking["level"] != "low" || thinking["budget"] != 800 || cfg.GoogleOptions()["thinkingConfig"] != nil {
		t.Fatalf("wrong Google summary thinking: %+v", cfg.GoogleOptions())
	}
	if cfg.AnthropicOptions()["output_config"].(map[string]any)["effort"] != "low" {
		t.Fatal("Anthropic effort inherited")
	}
	after, _ := json.Marshal(base)
	if string(before) != string(after) {
		t.Fatal("mutated shared nested options")
	}
	for _, level := range []string{"none", "minimal", "low"} {
		cfg := compactSummaryConfig(provider.ModelConfig{ProviderOptions: &provider.ModelProviderOptions{OpenAI: map[string]any{"reasoning_effort": level}}}, 1000)
		if cfg.OpenAIOptions()["reasoning_effort"] != level {
			t.Fatal("raised an already lower effort")
		}
	}
	for _, budget := range []int{0, 128, 12000, -1} {
		cfg := compactSummaryConfig(provider.ModelConfig{ProviderOptions: &provider.ModelProviderOptions{Google: map[string]any{"thinkingBudget": budget}}}, 1000)
		want := 500
		if budget >= 0 {
			want = min(want, budget)
		}
		if cfg.GoogleOptions()["thinking"].(map[string]any)["budget"] != want {
			t.Fatal("explicit thinking budget bypassed summary limit")
		}
	}
}

func TestCompactIncludesRetainedMessagesInOriginalOrder(t *testing.T) {
	eng, _, _, sid := newTestEngine(t)
	defer eng.Stop()
	messages := []*store.Message{
		{ID: "input", Role: store.RoleUser, Text: "retained task"},
		{ID: "old", Role: store.RoleAssistant, Text: strings.Repeat("source ", 2000) + "UNTRUNCATED_END"},
		{ID: "correction", Role: store.RoleUser, Text: "retained correction"},
	}
	calls := 0
	client := compactClientFunc(func(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
		calls++
		text := req.Messages[0].Text
		if !strings.Contains(text, `id="input" ref="@message(input)" role="user" action="retain"`) || !strings.Contains(text, `id="old" ref="@message(old)" role="assistant" action="summarize"`) || !strings.Contains(text, "UNTRUNCATED_END") {
			t.Fatal("missing message, source annotation or long message ending")
		}
		if strings.Index(text, "retained task") >= strings.Index(text, "UNTRUNCATED_END") || strings.Index(text, "UNTRUNCATED_END") >= strings.Index(text, "retained correction") {
			t.Fatal("changed chronological order")
		}
		return compactChunks(provider.Chunk{Delta: "summary", Done: true, Finish: provider.FinishStop}), nil
	})
	r := &resolvedModel{model: "fixture", protocol: "openai-compatible", config: provider.ModelConfig{ContextWindow: 64000}}
	if _, err := eng.summarizeCompactHistory(context.Background(), sid, r, client, messages, messages[1:2], "", 100); err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

// Use the actual Responses adapter with an in-process transport. No external
// model calls: exercise request serialization, truncated SSE, usage and commit.
func TestCompactResponsesSingleRequestAndFailureKeepsHistory(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			eng, ms, _, sid := newTestEngine(t)
			defer eng.Stop()
			ctx := context.Background()
			if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-responses", Models: []store.ProviderModel{{ID: "mock-model", ContextWindow: 1048576}}}); err != nil {
				t.Fatal(err)
			}
			effort := "xhigh"
			if _, err := ms.UpdateSession(ctx, sid, store.SessionUpdate{ReasoningEffort: &effort}); err != nil {
				t.Fatal(err)
			}
			appendEngineTestTurn(t, ms, sid, "old", strings.Repeat("old fact ", 32000)+"FINAL_OLD_FACT", "answer")
			appendEngineTestTurn(t, ms, sid, "recent1", "recent correction", "retained answer")
			appendEngineTestTurn(t, ms, sid, "recent2", "current task", "current answer")
			before, _ := ms.ListMessages(ctx, sid, 0)
			calls := 0
			client := openai.NewResponses(openai.Config{BaseURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: contextBudgetTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				var body struct {
					MaxOutput int `json:"max_output_tokens"`
					Reasoning struct {
						Effort string `json:"effort"`
					} `json:"reasoning"`
					Input json.RawMessage `json:"input"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				if body.MaxOutput != 16384 || body.Reasoning.Effort != "low" || !strings.Contains(string(body.Input), "FINAL_OLD_FACT") || !strings.Contains(string(body.Input), "recent correction") {
					t.Errorf("wrong summary request: output=%d reasoning=%s", body.MaxOutput, body.Reasoning.Effort)
				}
				terminal := `{"type":"response.completed","response":{"usage":{"input_tokens":65000,"output_tokens":100,"output_tokens_details":{"reasoning_tokens":80}}}}`
				if failed {
					terminal = `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":65000,"output_tokens":16384,"output_tokens_details":{"reasoning_tokens":16000}}}}`
				}
				stream := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"Earlier facts summarized.\"}\n\n" + "data: " + terminal + "\n\n"
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(stream))}, nil
			})}})
			eng.resolver = registry.Static(client)
			_, err := eng.Compact(ctx, CompactInput{SessionID: sid})
			if calls != 1 {
				t.Fatalf("unexpected split or retry: %d calls", calls)
			}
			sess, _ := ms.GetSession(ctx, sid)
			if sess.ReasoningEffort != "xhigh" {
				t.Fatal("compaction changed the session's reasoning preference")
			}
			after, _ := ms.ListMessages(ctx, sid, 0)
			if failed {
				if !errors.Is(err, ErrCompactIncomplete) || !strings.Contains(err.Error(), "max_output_tokens") || !reflect.DeepEqual(before, after) {
					t.Fatalf("truncation committed or lost its reason: %v", err)
				}
			} else if err != nil || len(after) != len(before)+1 {
				t.Fatalf("successful summary not committed: %v", err)
			}
			usage, err := ms.SessionUsage(ctx, sid)
			if err != nil {
				t.Fatal(err)
			}
			if usage.RequestCount != 1 || usage.CumulativeInputUncachedTokens != 65000 {
				t.Fatalf("missing usage: %+v", usage)
			}
			if failed && usage.CumulativeOutputReasoningTokens != 16000 {
				t.Fatalf("lost billed reasoning on incomplete response: %+v", usage)
			}
		})
	}
}

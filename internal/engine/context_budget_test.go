package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/openai"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
)

func TestCompactThresholdDoesNotReserveMaximumOutput(t *testing.T) {
	for _, output := range []int{0, 384000, 1000000, 2000000} {
		cfg := provider.ModelConfig{ContextWindow: 1050000, Limits: &provider.ModelLimits{MaxOutputTokens: output}}
		for _, percent := range []int{0, 40, 80, 95, 100} {
			if got := compactTrigger(cfg, percent); got != 1050000*percent/100 {
				t.Fatalf("output=%d percent=%d trigger=%d", output, percent, got)
			}
		}
	}
	if compactTrigger(provider.ModelConfig{}, 80) != 0 {
		t.Fatal("unknown window must not infer a trigger")
	}
}

func TestRequestOutputFitsRemainingWindow(t *testing.T) {
	options := &provider.ModelProviderOptions{
		OpenAI:    map[string]any{"max_completion_tokens": 32000, "max_output_tokens": 3000},
		Anthropic: map[string]any{"max_tokens": 16000},
		Google:    map[string]any{"maxOutputTokens": 12000},
	}
	for _, tc := range []struct {
		name, protocol                  string
		window, input, configured, want int
		options                         *provider.ModelProviderOptions
	}{
		{name: "million output capability", window: 1050000, input: 700000, configured: 1000000, want: 350000},
		{name: "within configured limit", window: 1050000, input: 563028, configured: 384000, want: 384000},
		{name: "shrinking remaining window", window: 1050000, input: 800000, configured: 384000, want: 250000},
		{name: "small configured output", window: 1050000, input: 800000, configured: 4096, want: 4096},
		{name: "unconfigured OpenAI", protocol: "openai-compatible", window: 128000, input: 100000},
		{name: "unconfigured Responses", protocol: "openai-responses", window: 128000, input: 100000},
		{name: "unconfigured Google", protocol: "google", window: 128000, input: 100000},
		{name: "Anthropic default", protocol: "anthropic", window: 128000, input: 10000, want: 64000},
		{name: "Anthropic default capped", protocol: "anthropic", window: 128000, input: 100000, want: 28000},
		{name: "chat option", protocol: "openai-compatible", window: 128000, input: 100000, options: options, want: 28000},
		{name: "responses option precedence", protocol: "openai-responses", window: 128000, input: 100000, options: options, want: 3000},
		{name: "Anthropic option", protocol: "anthropic", window: 128000, input: 100000, options: options, want: 16000},
		{name: "Google option", protocol: "google", window: 128000, input: 100000, options: options, want: 12000},
		{name: "neutral limit precedence", protocol: "openai-responses", window: 128000, input: 100000, configured: 8000, options: options, want: 8000},
		{name: "last token", window: 128000, input: 127999, configured: 64000, want: 1},
		{name: "unknown window", input: 100000, configured: 64000, want: 64000},
		{name: "unknown window and output", input: 100000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := provider.ModelConfig{ContextWindow: tc.window, ProviderOptions: tc.options, Limits: &provider.ModelLimits{MaxOutputTokens: tc.configured, MaxToolLoops: 64}}
			before, _ := json.Marshal(cfg)
			req := provider.Request{Config: cfg}
			if err := limitRequestOutput(&req, tc.protocol, tc.input); err != nil {
				t.Fatal(err)
			}
			got, _ := req.Config.MaxOutputTokens()
			if got != tc.want || req.Config.Limits.MaxToolLoops != 64 {
				t.Fatalf("limits=%+v want output=%d", req.Config.Limits, tc.want)
			}
			after, _ := json.Marshal(cfg)
			if string(before) != string(after) {
				t.Fatal("request mutated shared model config")
			}
		})
	}
	for _, input := range []int{128000, 128001} {
		req := provider.Request{Config: provider.ModelConfig{ContextWindow: 128000}}
		if err := limitRequestOutput(&req, "", input); !errors.Is(err, ErrContextBudget) {
			t.Fatalf("exhausted window: %v", err)
		}
	}
}

func TestContextPreflightUsesCalibratedInputWithoutEarlyCompaction(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	if _, err := ms.RecordSessionUsage(ctx, sid, store.UsageRecordInput{Provider: "mock", Model: "mock-model", EstimatedInputTokens: 100, InputUncachedTokens: 130}); err != nil {
		t.Fatal(err)
	}
	req := provider.Request{Config: provider.ModelConfig{ContextWindow: 100000, Limits: &provider.ModelLimits{MaxOutputTokens: 100000}}, Messages: []provider.Message{{Role: provider.RoleUser, Text: strings.Repeat("facts ", 32000)}}}
	tokens := calibratedTokenEstimate(requestInputTokens(req), 1.3)
	if tokens < 50000 || tokens >= 80000 {
		t.Fatalf("fixture must be below 80%%: %d", tokens)
	}
	resolved := &resolvedModel{providerName: "mock", model: "mock-model", protocol: "openai-compatible", config: req.Config}
	eng.resolver = registry.Static(compactClientFunc(func(context.Context, provider.Request) (<-chan provider.Chunk, error) {
		t.Error("compaction attempted below 80%")
		return nil, errors.New("unexpected compaction")
	}))
	for _, percent := range []string{"80", "0"} {
		if err := ms.SetSettings(ctx, map[string]string{config.SettingCompactAutoThresholdPercent: percent}); err != nil {
			t.Fatal(err)
		}
		request := req
		compacted, err := eng.prepareContextRequest(ctx, sid, "", resolved, store.ModeChat, &request, false)
		if err != nil || compacted {
			t.Fatalf("preflight: compacted=%v err=%v", compacted, err)
		}
		output, _ := request.Config.MaxOutputTokens()
		if output != 100000-tokens {
			t.Fatalf("output=%d expected=%d", output, 100000-tokens)
		}
	}
}

func TestQueuedRequestUsesProtocolOutputLimit(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-responses", Models: []store.ProviderModel{{ID: "mock-model"}}}); err != nil {
		t.Fatal(err)
	}
	cfg := provider.ModelConfig{ContextWindow: 100000, ProviderOptions: &provider.ModelProviderOptions{OpenAI: map[string]any{"max_output_tokens": 700, "max_completion_tokens": 900}}}
	snapshot, _ := json.Marshal(cfg)
	if _, err := ms.QueueInput(ctx, store.QueueInputInput{SessionID: sid, ClientMessageID: "budget-queue", Text: "continue", Provider: "mock", Model: "mock-model", Mode: store.ModeChat, ModelConfig: snapshot}); err != nil {
		t.Fatal(err)
	}
	capture := &captureClient{reqCh: make(chan provider.Request, 1)}
	eng.resolver = registry.Static(capture)
	eng.TryDrainQueued(sid)
	eng.Wait()
	select {
	case req := <-capture.reqCh:
		output, _ := req.Config.MaxOutputTokens()
		if output != 700 {
			t.Fatalf("queued Responses request used wrong protocol output limit: %d", output)
		}
	default:
		t.Fatal("queued request never reached provider")
	}
}

func requestInputTokens(req provider.Request) int {
	e, err := contextbuilder.EstimateRequest(req, "openai-compatible")
	if err != nil {
		panic(err)
	}
	return e.Total()
}

func TestContextPreflightCountsNativeReasoningAcrossTurnBoundary(t *testing.T) {
	eng, ms, _, sid := newTestEngine(t)
	defer eng.Stop()
	ctx := context.Background()
	if err := ms.SetSettings(ctx, map[string]string{config.SettingCompactAutoThresholdPercent: "0"}); err != nil {
		t.Fatal(err)
	}
	thought := strings.Repeat("reasoning ", 4000)
	native, _ := json.Marshal([]any{map[string]any{"type": "reasoning", "content": []any{map[string]any{"type": "reasoning_text", "text": thought}}}, map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "answer"}}}})
	req := provider.Request{Config: provider.ModelConfig{ContextWindow: 100000, Limits: &provider.ModelLimits{MaxOutputTokens: 100000}}, Messages: []provider.Message{{Role: provider.RoleAssistant, Parts: []provider.Part{{Type: provider.PartThought, Text: thought}, {Type: provider.PartText, Text: "answer"}}, Continuations: []provider.Continuation{{Kind: provider.ContinuationOpenAIResponses, Data: native}}}}}
	resolved := &resolvedModel{providerName: "mock", model: "mock-model", protocol: "openai-responses", config: req.Config}
	before, err := contextbuilder.EstimateRequest(req, resolved.protocol)
	if err != nil {
		t.Fatal(err)
	}
	if before.Total() < 10000 {
		t.Fatalf("native reasoning not budgeted: %+v", before)
	}
	for _, historical := range []bool{false, true} {
		if historical {
			req.Messages[0].Parts = req.Messages[0].Parts[1:]
		}
		compacted, err := eng.prepareContextRequest(ctx, sid, "", resolved, store.ModeChat, &req, false)
		if err != nil || compacted {
			t.Fatalf("compacted=%v err=%v", compacted, err)
		}
		output, _ := req.Config.MaxOutputTokens()
		if output != 100000-before.Total() {
			t.Fatalf("historical=%v output=%d input=%d", historical, output, before.Total())
		}
	}
}

type contextBudgetTransport func(*http.Request) (*http.Response, error)

func (f contextBudgetTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestContextPreflightResponsesWireOutputLimit(t *testing.T) {
	for _, configured := range []int{0, 4096, 384000} {
		t.Run(fmt.Sprint(configured), func(t *testing.T) {
			eng, ms, _, sid := newTestEngine(t)
			defer eng.Stop()
			ctx := context.Background()
			if err := ms.SetSettings(ctx, map[string]string{config.SettingCompactAutoThresholdPercent: "0"}); err != nil {
				t.Fatal(err)
			}
			req := provider.Request{Model: "fixture", Config: provider.ModelConfig{ContextWindow: 100000}, Messages: []provider.Message{{Role: provider.RoleUser, Text: "question"}}}
			if configured > 0 {
				req.Config.Limits = &provider.ModelLimits{MaxOutputTokens: configured}
			}
			estimate, err := contextbuilder.EstimateRequest(req, "openai-responses")
			if err != nil {
				t.Fatal(err)
			}
			resolved := &resolvedModel{providerName: "mock", model: "mock-model", protocol: "openai-responses", config: req.Config}
			if compacted, err := eng.prepareContextRequest(ctx, sid, "", resolved, store.ModeChat, &req, false); err != nil || compacted {
				t.Fatalf("preflight: compacted=%v err=%v", compacted, err)
			}
			var body map[string]json.RawMessage
			client := openai.NewResponses(openai.Config{BaseURL: "https://fixture.invalid", HTTPClient: &http.Client{Transport: contextBudgetTransport(func(r *http.Request) (*http.Response, error) {
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\"}\n\n"))}, nil
			})}})
			ch, err := client.Stream(ctx, req)
			if err != nil {
				t.Fatal(err)
			}
			for chunk := range ch {
				if chunk.Err != nil {
					t.Fatal(chunk.Err)
				}
			}
			if configured == 0 {
				if body["max_output_tokens"] != nil {
					t.Fatalf("blank setting inserted max_output_tokens: %s", body["max_output_tokens"])
				}
			} else {
				var got int
				if err := json.Unmarshal(body["max_output_tokens"], &got); err != nil {
					t.Fatal(err)
				}
				if want := min(configured, req.Config.ContextWindow-estimate.Total()); got != want {
					t.Fatalf("wire output limit = %d, want %d", got, want)
				}
			}
		})
	}
}

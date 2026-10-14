package api

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

func TestCleanModelsGroupsStableFamilies(t *testing.T) {
	for _, tc := range []struct {
		name   string
		models []store.ProviderModel
		want   []store.ProviderModel
	}{
		{"empty", nil, []store.ProviderModel{}},
		{"unknown models stay independent", modelsWithIDs("custom-a gpt-new custom-b gpt-old free"), modelsWithIDs("custom-a gpt-new gpt-old custom-b free")},
		{"first occurrence and internal order", modelsWithIDs("qwen3-max claude-opus deepseek-pro qwen2-mini claude-haiku deepseek-flash"), modelsWithIDs("qwen3-max qwen2-mini claude-opus claude-haiku deepseek-pro deepseek-flash")},
		{"relay prefixes and aliases", []store.ProviderModel{
			{ID: "proxy/DeepSeek/pro:free"}, {ID: "opaque", DisplayName: "Qwen 3"}, {ID: "gpt-custom"},
			{ID: "deepseek-flash", DisplayName: "Claude alias"}, {ID: "qwen3-flash"}, {ID: "openai/o3-mini"},
		}, []store.ProviderModel{
			{ID: "proxy/DeepSeek/pro:free"}, {ID: "deepseek-flash", DisplayName: "Claude alias"},
			{ID: "opaque", DisplayName: "Qwen 3"}, {ID: "qwen3-flash"}, {ID: "gpt-custom"}, {ID: "openai/o3-mini"},
		}},
		{"clean and deduplicate without losing metadata", []store.ProviderModel{
			{ID: " glm-pro ", DisplayName: " Pro ", ContextWindow: 64000, Unavailable: true, CostMultiplier: float64Ptr(0.2),
				Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 5},
				ProviderOptions: &store.ProviderOptions{OpenAI: map[string]any{"temperature": 0.7}}},
			{ID: "gpt-custom"}, {ID: "glm-flash"}, {ID: "glm-pro", DisplayName: "Duplicate"}, {ID: " "},
		}, []store.ProviderModel{
			{ID: "glm-pro", DisplayName: "Pro", ContextWindow: 64000, Unavailable: true, CostMultiplier: float64Ptr(0.2),
				Capabilities: &store.ModelCaps{Tools: true}, Limits: &store.ModelLimits{MaxToolLoops: 5},
				ProviderOptions: &store.ProviderOptions{OpenAI: map[string]any{"temperature": 0.7}}},
			{ID: "glm-flash"}, {ID: "gpt-custom"},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanModels(tc.models)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v; want %+v", got, tc.want)
			}
			if again := cleanModels(got); !reflect.DeepEqual(again, got) {
				t.Fatalf("grouping is not idempotent: %+v", again)
			}
		})
	}
}

func TestModelFamily(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{" OpenRouter/DeepSeek/DeepSeek-V4.1-Flash:free ", "deepseek"},
		{"anthropic/claude-sonnet", "anthropic"}, {"OpenAI/GPT-6.1", "gpt"}, {"o3-mini", "gpt"},
		{"models/gemini-flash", "gemini"}, {"gemma-3", "gemini"}, {"qwen3.8", "qwen"},
		{"mimo-v2", "mimo"}, {"moonshotai/kimi-k3", "moonshot"}, {"z-ai/glm-5.3", "zhipu"},
		{"grok-4", "grok"}, {"free", ""}, {"custom/mystery", ""}, {"", ""},
	} {
		if got := modelFamily(tc.value); got != tc.want {
			t.Errorf("modelFamily(%q) = %q; want %q", tc.value, got, tc.want)
		}
	}
}

func TestProviderModelGroupingAcrossWritesAndSync(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Deliberately different from the user's family and model order.
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen3"},{"id":"gpt-new"},{"id":"deepseek-new"},{"id":"claude-new"},{"id":"deepseek-pro"},{"id":"gpt-base"},{"id":"qwen2"},{"id":"gemini-flash"},{"id":"gpt-latest"}]}`))
	}))
	defer upstream.Close()
	srv, _, _ := newConfigTestServer(t)
	url := srv.URL + "/providers/grouped"
	created := decodeJSON[providerProfileView](t, req(t, http.MethodPost, srv.URL+"/providers", map[string]any{
		"id": "grouped", "brand": "buzzhive", "protocol": "openai-compatible", "baseURL": upstream.URL,
		"models": modelsWithIDs("claude-opus deepseek-pro claude-haiku gpt-base"),
	}))
	assertModelOrder(t, created.Models, "claude-opus claude-haiku deepseek-pro gpt-base")
	current := created.Models
	for _, tc := range []struct{ name, added, want string }{
		{"manual add", "deepseek-flash", "claude-opus claude-haiku deepseek-pro deepseek-flash gpt-base"},
		{"batch import", "gpt-new qwen3 deepseek-new qwen2", "claude-opus claude-haiku deepseek-pro deepseek-flash deepseek-new gpt-base gpt-new qwen3 qwen2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patched := decodeJSON[providerProfileView](t, req(t, http.MethodPatch, url, map[string]any{
				"models": append(current, modelsWithIDs(tc.added)...),
			}))
			assertModelOrder(t, patched.Models, tc.want)
			stored := decodeJSON[providerProfileView](t, req(t, http.MethodGet, url, nil))
			assertModelOrder(t, stored.Models, tc.want)
			current = stored.Models
		})
	}
	for range 2 {
		synced := decodeJSON[providerProfileView](t, req(t, http.MethodPost, url+"/sync", nil))
		want := "claude-opus claude-haiku claude-new deepseek-pro deepseek-flash deepseek-new gpt-base gpt-new gpt-latest qwen3 qwen2 gemini-flash"
		assertModelOrder(t, synced.Models, want)
		stored := decodeJSON[providerProfileView](t, req(t, http.MethodGet, url, nil))
		assertModelOrder(t, stored.Models, want)
		if !stored.Models[0].Unavailable || stored.Models[2].Unavailable {
			t.Fatalf("sync lost availability metadata: %+v", stored.Models)
		}
	}
}

func TestOpenRouterSyncGroupsOnlyConfiguredModels(t *testing.T) {
	got := syncOpenRouterModels(modelsWithIDs("claude-opus gpt-base claude-haiku"), []provider.ModelCandidate{
		{ID: "claude-new"}, {ID: "claude-haiku"}, {ID: "gpt-base"}, {ID: "claude-opus"},
	})
	assertModelOrder(t, got, "claude-opus claude-haiku gpt-base")
}

func modelsWithIDs(ids string) []store.ProviderModel {
	models := make([]store.ProviderModel, 0)
	for _, id := range strings.Fields(ids) {
		models = append(models, store.ProviderModel{ID: id})
	}
	return models
}

func assertModelOrder(t *testing.T, models []store.ProviderModel, want string) {
	t.Helper()
	ids := make([]string, 0, len(models))
	for _, model := range models {
		ids = append(ids, model.ID)
	}
	if got := strings.Join(ids, " "); got != want {
		t.Fatalf("model order = %q; want %q", got, want)
	}
}

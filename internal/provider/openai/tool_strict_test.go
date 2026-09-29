package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/provider"
)

func TestFunctionToolsPreserveOptionalFieldsWithoutStrictNormalization(t *testing.T) {
	// edit omits content; making every property required changes the tool contract.
	schema := json.RawMessage(`{"type":"object","properties":{"files":{"type":"array","items":{"type":"object","properties":{"action":{"type":"string"},"content":{"type":"string"},"hunks":{"type":"array","items":{"type":"string"}}},"required":["action"],"additionalProperties":false}}},"required":["files"],"additionalProperties":false}`)
	req := provider.Request{Model: "model", Tools: []provider.ToolDef{{Name: "patch", InputSchema: schema}}}
	config := Config{BaseURL: "https://example.invalid/v1"}
	for _, tc := range []struct {
		name   string
		nested bool
		build  func() (*http.Request, error)
	}{
		{"chat", true, func() (*http.Request, error) { return New(config).newRequest(context.Background(), req, false) }},
		{"responses", false, func() (*http.Request, error) { return NewResponses(config).newRequest(context.Background(), req) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			defer request.Body.Close()
			var body struct {
				Tools []map[string]json.RawMessage `json:"tools"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body.Tools) != 1 {
				t.Fatalf("tools = %v", body.Tools)
			}
			function := body.Tools[0]
			if tc.nested {
				var nested map[string]json.RawMessage
				if err := json.Unmarshal(function["function"], &nested); err != nil {
					t.Fatal(err)
				}
				function = nested
			}
			if string(function["strict"]) != "false" {
				t.Errorf("wire strict = %s; must explicitly be false, not omitted", function["strict"])
			}
			var want, got any
			if err := json.Unmarshal(schema, &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(function["parameters"], &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("tool schema changed: got %s, want %s", function["parameters"], schema)
			}
		})
	}
}

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestCompactFailureResponsePreservesDetails(t *testing.T) {
	for _, tc := range []struct {
		name, code, detail string
		window, status     int
	}{
		{"capacity", "context_budget_exceeded", "estimated summary input", 1024, http.StatusRequestEntityTooLarge},
		{"truncation", "compact_incomplete", "max_output_tokens", 64000, http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			ms := storetest.New(t)
			if err := ms.PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "model", ContextWindow: tc.window}}}); err != nil {
				t.Fatal(err)
			}
			if err := ms.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "model"}); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				id := fmt.Sprint(i)
				if _, err := ms.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn" + id, UserMessageID: "message" + id, ClientMessageID: "client" + id, UserText: strings.Repeat("old fact ", 500)}); err != nil {
					t.Fatal(err)
				}
				if _, err := ms.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn" + id, Status: store.TurnCompleted, AssistantParts: store.TextPart("answer")}); err != nil {
					t.Fatal(err)
				}
			}
			hub := event.NewHub()
			eng := engine.New(ms, hub, registry.Static(mock.New(mock.WithChunks([]provider.Chunk{{Err: &provider.OutputLimitError{Message: "openai responses: incomplete: max_output_tokens"}}}))), ms)
			defer eng.Stop()
			handler := New(eng, ms, ms, hub).Handler(testToken, nil)
			req := httptest.NewRequest(http.MethodPost, "/sessions/session/compact", strings.NewReader(`{"clientMessageID":"compact:fixture"}`))
			req.Header.Set("Authorization", "Bearer "+testToken)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			var body map[string]string
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != tc.status || body["error"] != tc.code || !strings.Contains(body["detail"], tc.detail) {
				t.Fatalf("status=%d payload=%v", recorder.Code, body)
			}
			messages, err := ms.ListMessages(ctx, "session", 0)
			if err != nil || len(messages) != 6 {
				t.Fatalf("failed compact changed history: %v", err)
			}
		})
	}
}

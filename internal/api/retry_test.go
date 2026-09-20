package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestRetryEndpointScopesAndDeduplicates(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "retry", Title: "test", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	if err := st.(*memstore.Memstore).PutProviderProfile(ctx, &store.ProviderProfile{ID: "mock", Protocol: "openai-compatible", BaseURL: "http://example.invalid", Models: []store.ProviderModel{{ID: "model"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "retry", TurnID: "failed", ClientMessageID: "user", UserMessageID: "user-message", UserText: "original"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "failed", Status: store.TurnFailed, Error: "503"}); err != nil {
		t.Fatal(err)
	}
	endpoint := srv.URL + "/sessions/retry/turns/failed/retry"
	for _, sample := range []struct {
		url  string
		body map[string]string
		want int
	}{
		{endpoint, map[string]string{}, 400},
		{srv.URL + "/sessions/retry/turns/missing/retry", map[string]string{"clientMessageID": "missing"}, 404},
		{endpoint, map[string]string{"clientMessageID": "retry-request"}, 202},
		{endpoint, map[string]string{"clientMessageID": "retry-request"}, 200},
	} {
		resp := req(t, http.MethodPost, sample.url, sample.body)
		if resp.StatusCode != sample.want {
			t.Fatalf("status=%d want=%d", resp.StatusCode, sample.want)
		}
		resp.Body.Close()
	}
	resp := req(t, http.MethodGet, srv.URL+"/sessions/retry/turns", nil)
	defer resp.Body.Close()
	var result struct {
		Turns []store.ConversationTurn `json:"turns"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Turns) != 2 || result.Turns[1].RetryOfTurnID != "failed" {
		t.Fatalf("retry metadata missing: %+v", result.Turns)
	}
}

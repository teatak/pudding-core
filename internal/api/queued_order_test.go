package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestReorderQueuedInputsHTTP(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "queue", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "queue", TurnID: "running", UserMessageID: "initial", ClientMessageID: "initial", UserText: "initial"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b"} {
		if _, err := st.QueueInput(ctx, store.QueueInputInput{SessionID: "queue", ClientMessageID: id, Text: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"clientMessageIDs":["b","a"]}`, http.StatusOK},
		{`{"clientMessageIDs":["a","a"]}`, http.StatusConflict},
		{`{"clientMessageIDs":["b"]}`, http.StatusConflict},
		{`{}`, http.StatusBadRequest},
	} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/sessions/queue/queued-inputs/reorder", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != tc.status {
			t.Fatalf("status=%d expected=%d", res.StatusCode, tc.status)
		}
		if tc.status == http.StatusOK {
			var payload struct {
				QueuedInputs []*store.QueuedInput `json:"queuedInputs"`
			}
			if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			if len(payload.QueuedInputs) != 2 || payload.QueuedInputs[0].ClientMessageID != "b" {
				t.Fatal("wrong response order")
			}
		}
		res.Body.Close()
	}
	inputs, err := st.ListQueuedInputs(ctx, "queue")
	if err != nil || len(inputs) != 2 || inputs[0].ClientMessageID != "b" {
		t.Fatal("failed request changed order")
	}
}

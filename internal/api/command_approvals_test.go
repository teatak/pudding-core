package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestCommandApprovalsAPIRequiresSessionAndAuthentication(t *testing.T) {
	ms := memstore.New()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms)
	t.Cleanup(eng.Stop)
	if err := ms.CreateSession(context.Background(), &store.Session{ID: "session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	handler := New(eng, ms, ms, hub).Handler(testToken, nil)
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		for _, tc := range []struct {
			id     string
			auth   bool
			status int
		}{
			{"session", true, http.StatusOK}, {"missing", true, http.StatusNotFound}, {"session", false, http.StatusUnauthorized},
		} {
			req := httptest.NewRequest(method, "/sessions/"+tc.id+"/command-approvals", nil)
			if tc.auth {
				req.Header.Set("Authorization", "Bearer "+testToken)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("%s %s got %d: %s", method, tc.id, rec.Code, rec.Body.String())
			}
			if rec.Code == http.StatusOK {
				var status engine.CommandApprovalStatus
				if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
					t.Fatal(err)
				}
				if status.GrantCount != 0 || status.ReusedCount != 0 || status.ApprovalReasons == nil {
					t.Fatalf("status=%+v", status)
				}
			}
		}
	}
}

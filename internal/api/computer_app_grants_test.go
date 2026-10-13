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
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestComputerAppGrantAPIIsAuthenticatedAndSessionScoped(t *testing.T) {
	ctx := context.Background()
	st := storetest.New(t)
	hub := event.NewHub()
	eng := engine.New(st, hub, registry.Static(mock.New()), st)
	t.Cleanup(eng.Stop)
	for _, id := range []string{"session", "other"} {
		if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
		if err := st.GrantComputerApp(ctx, id, "com.example.App"); err != nil {
			t.Fatal(err)
		}
	}
	handler := New(eng, st, st, hub).Handler(testToken, nil)
	request := func(method, path string, authenticated bool, status int) []byte {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if authenticated {
			req.Header.Set("Authorization", "Bearer "+testToken)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		suffix := "/computer-app-grants"
		if method == http.MethodDelete {
			suffix += "/com.example.App"
		}
		request(method, "/sessions/session"+suffix, false, http.StatusUnauthorized)
		request(method, "/sessions/missing"+suffix, true, http.StatusNotFound)
	}
	var result struct {
		Grants []store.ComputerAppGrant `json:"grants"`
	}
	if err := json.Unmarshal(request("GET", "/sessions/session/computer-app-grants", true, http.StatusOK), &result); err != nil || len(result.Grants) != 1 || result.Grants[0].AppID != "com.example.App" || result.Grants[0].CreatedAt.IsZero() {
		t.Fatalf("list response: %+v %v", result, err)
	}
	for range 2 {
		request("DELETE", "/sessions/session/computer-app-grants/com.example.App", true, http.StatusNoContent)
	}
	if err := json.Unmarshal(request("GET", "/sessions/session/computer-app-grants", true, http.StatusOK), &result); err != nil || result.Grants == nil || len(result.Grants) != 0 {
		t.Fatalf("revoked list: %+v %v", result, err)
	}
	if granted, err := st.HasComputerAppGrant(ctx, "other", "com.example.App"); err != nil || !granted {
		t.Fatalf("cross-session revoke: %v %v", granted, err)
	}
	if _, err := st.GetSession(ctx, "session"); err != nil {
		t.Fatalf("revoke deleted the conversation: %v", err)
	}
}

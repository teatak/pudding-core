package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teatak/pudding-core/internal/plugin"
)

func TestAuthScopesRequestToExplicitRuntime(t *testing.T) {
	var got string
	handler := withAuth("token", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		got = plugin.RuntimeIDFromContext(r.Context())
	}))
	req := httptest.NewRequest(http.MethodGet, "/plugins", nil)
	req.Header.Set("Authorization", "Bearer token")
	req.Header.Set(plugin.RuntimeIDHeader, "runtime_a")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if got != "runtime_a" {
		t.Fatalf("runtime id = %q, want runtime_a", got)
	}
}

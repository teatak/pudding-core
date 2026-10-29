package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestCommandApprovalsAPIRequiresSessionAndAuthentication(t *testing.T) {
	ms := storetest.New(t)
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
				if status.GrantCount != 0 || status.ReusedCount != 0 || status.ApprovalReasons == nil || status.ReuseMissReasons == nil {
					t.Fatalf("status=%+v", status)
				}
			}
		}
	}
}

func TestApprovalDiagnosticsSettingsApplyImmediatelyAndReset(t *testing.T) {
	ctx := context.Background()
	cfg := config.NewManager(t.TempDir())
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	ms := storetest.New(t)
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), cfg)
	t.Cleanup(eng.Stop)
	handler := New(eng, ms, cfg, hub).Handler(testToken, nil)
	if eng.CommandApprovals("session").DiagnosticsEnabled {
		t.Fatal("diagnostics enabled by default")
	}
	for _, tc := range []struct {
		method, body string
		code         int
		enabled      bool
	}{
		{http.MethodPut, `{"developer_approval_diagnostics":"true"}`, http.StatusNoContent, true},
		{http.MethodPut, `{"developer_approval_diagnostics":"invalid"}`, http.StatusBadRequest, true},
		{http.MethodDelete, "", http.StatusOK, false},
		{http.MethodPut, `{"developer_approval_diagnostics":"true"}`, http.StatusNoContent, true},
		{http.MethodPut, `{"developer_approval_diagnostics":"false"}`, http.StatusNoContent, false},
	} {
		req := httptest.NewRequest(tc.method, "/settings", strings.NewReader(tc.body))
		req.Header.Set("Authorization", "Bearer "+testToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tc.code || eng.CommandApprovals("session").DiagnosticsEnabled != tc.enabled {
			t.Fatalf("%s: status=%d body=%s diagnostics=%v", tc.body, rec.Code, rec.Body.String(), eng.CommandApprovals("session"))
		}
		saved, err := cfg.Settings(ctx)
		if err != nil || (saved[config.SettingDeveloperApprovalDiagnostics] == "true") != tc.enabled {
			t.Fatalf("saved setting disagrees: %v, %v", saved, err)
		}
		restarted := engine.New(ms, event.NewHub(), registry.Static(mock.New()), cfg)
		if restarted.CommandApprovals("session").DiagnosticsEnabled != tc.enabled {
			t.Fatal("restart lost explicit developer setting")
		}
		restarted.Stop()
	}
}

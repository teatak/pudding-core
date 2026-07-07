package browser

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestElectronBridgeServiceOpenListAndRelease(t *testing.T) {
	const token = "bridge-token"
	tabsBySession := map[string][]electronBridgeSnapshot{}
	var closedTabID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		var req electronBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
			return
		}
		switch r.URL.Path {
		case "/browser/tabs/list":
			writeElectronBridgeTestJSON(w, electronBridgeTabsResponse{Tabs: tabsBySession[req.SessionID], ProcessMode: "headless"})
		case "/browser/tabs/open":
			if req.SessionID == "" || req.TabID == "" || req.URL != "https://example.com" {
				t.Fatalf("unexpected open request: %+v", req)
			}
			tab := electronBridgeSnapshot{
				SessionID: req.SessionID,
				TabID:     req.TabID,
				Status:    "detached",
				URL:       req.URL,
				Title:     "Example",
				RuntimeID: "webContents:1",
			}
			tabsBySession[req.SessionID] = []electronBridgeSnapshot{tab}
			writeElectronBridgeTestJSON(w, tab)
		case "/browser/tabs/close":
			closedTabID = req.TabID
			writeElectronBridgeTestJSON(w, map[string]bool{"ok": true})
		default:
			http.Error(w, `{"error":"browser bridge route not found"}`, http.StatusNotFound)
		}
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	tab, err := service.Open(context.Background(), "sess_a", "", "https://example.com")
	if err != nil {
		t.Fatal(err)
	}
	if tab.SessionID != "sess_a" || tab.ID == "" || tab.URL != "https://example.com" || tab.Mode != "headless" {
		t.Fatalf("unexpected tab snapshot: %+v", tab)
	}
	tabs, err := service.ListTabs(context.Background(), "sess_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(tabs) != 1 || tabs[0].ID != tab.ID {
		t.Fatalf("unexpected tabs: %+v", tabs)
	}
	if err := service.ReleaseTab(context.Background(), "sess_a", tab.ID); err != nil {
		t.Fatal(err)
	}
	if closedTabID != tab.ID {
		t.Fatalf("expected closed tab %q, got %q", tab.ID, closedTabID)
	}
}

func TestElectronBridgeServiceMapsMissingTab(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"browser tab not found"}`))
	}))
	defer server.Close()

	service, err := NewElectronBridgeService(ElectronBridgeConfig{URL: server.URL, Token: "token"})
	if err != nil {
		t.Fatal(err)
	}
	err = service.ReleaseTab(context.Background(), "sess_a", "tab_missing")
	if !errors.Is(err, ErrTabNotFound) {
		t.Fatalf("expected ErrTabNotFound, got %v", err)
	}
}

func writeElectronBridgeTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil && !strings.Contains(err.Error(), "broken pipe") {
		panic(err)
	}
}

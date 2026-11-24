package acp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHostStdioForwardAndCancel(t *testing.T) {
	cancelled := make(chan struct{})
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/local-agent-mcp" || r.Header.Get("Authorization") != "Bearer scoped" {
			t.Error("incorrect endpoint or auth")
		}
		var message struct {
			ID     int
			Method string
		}
		if err := json.NewDecoder(r.Body).Decode(&message); err != nil {
			t.Error(err)
			return
		}
		if message.Method == "tools/call" {
			close(started)
			<-r.Context().Done()
			close(cancelled)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": map[string]bool{"ok": true}})
	}))
	defer srv.Close()
	input, in := io.Pipe()
	out, output := io.Pipe()
	done := make(chan error, 1)
	go func() { done <- ForwardHostMCP(srv.URL+"/local-agent-mcp", "scoped", input, output); output.Close() }()
	enc, dec := json.NewEncoder(in), json.NewDecoder(out)
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	var result map[string]any
	if err := dec.Decode(&result); err != nil || result["id"] != float64(1) {
		t.Fatalf("reply: %+v %v", result, err)
	}
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "tools/call"})
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	_ = enc.Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/cancelled", "params": map[string]any{"requestId": 2}})
	if err := dec.Decode(&result); err != nil || result["error"] == nil {
		t.Fatalf("cancel reply: %+v %v", result, err)
	}
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("HTTP work not cancelled")
	}
	in.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

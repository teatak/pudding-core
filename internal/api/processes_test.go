package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/provider/registry"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestBackgroundProcessAPIListsAndStopsSessionProcess(t *testing.T) {
	ctx := context.Background()
	ms := memstore.New()
	for _, id := range []string{"sess_process", "sess_other"} {
		if err := ms.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	runner := tool.NewBuiltinRunner()
	hub := event.NewHub()
	eng := engine.New(ms, hub, registry.Static(mock.New()), ms, engine.WithTools(tool.NewMultiRunner(runner)))
	t.Cleanup(eng.Stop)
	server := httptest.NewServer(New(eng, ms, ms, hub).Handler(testToken, nil))
	t.Cleanup(server.Close)

	root := t.TempDir()
	args, _ := json.Marshal(map[string]any{
		"scope":      "project",
		"command":    fmt.Sprintf("%q -test.run=^TestAPIBackgroundProcessHelper$", os.Args[0]),
		"env":        map[string]string{"PUDDING_API_BACKGROUND_HELPER": "1"},
		"background": true,
	})
	started := runner.Call(ctx, tool.Call{
		SessionID:   "sess_process",
		CallID:      "call_start",
		Name:        tool.CommandRun,
		Args:        args,
		ProjectDirs: []string{root},
	})
	if !started.Ok {
		t.Fatalf("start process: %+v", started)
	}
	var startPayload struct {
		ProcessID string `json:"processID"`
	}
	if err := json.Unmarshal([]byte(started.Content), &startPayload); err != nil || startPayload.ProcessID == "" {
		t.Fatalf("decode start payload: content=%s err=%v", started.Content, err)
	}

	type processList struct {
		Processes []tool.BackgroundProcessSnapshot `json:"processes"`
	}
	listed := decodeJSON[processList](t, req(t, http.MethodGet, server.URL+"/sessions/sess_process/processes", nil))
	if len(listed.Processes) != 1 || listed.Processes[0].ProcessID != startPayload.ProcessID || !listed.Processes[0].Running {
		t.Fatalf("unexpected process list: %+v", listed.Processes)
	}
	other := decodeJSON[processList](t, req(t, http.MethodGet, server.URL+"/sessions/sess_other/processes", nil))
	if len(other.Processes) != 0 {
		t.Fatalf("process leaked across sessions: %+v", other.Processes)
	}
	type sessionList struct {
		Sessions []*store.Session `json:"sessions"`
	}
	sessions := decodeJSON[sessionList](t, req(t, http.MethodGet, server.URL+"/sessions", nil))
	for _, session := range sessions.Sessions {
		if session.ID == "sess_process" && session.BackgroundProcessCount != 1 {
			t.Fatalf("running background process count = %d, want 1", session.BackgroundProcessCount)
		}
	}

	var logSnapshot tool.BackgroundProcessLogSnapshot
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		logSnapshot = decodeJSON[tool.BackgroundProcessLogSnapshot](t, req(t, http.MethodGet, server.URL+"/sessions/sess_process/processes/"+startPayload.ProcessID+"?tail_bytes=65536", nil))
		if backgroundProcessLogText(logSnapshot.Output) != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if logSnapshot.Process.ProcessID != startPayload.ProcessID || !strings.Contains(backgroundProcessLogText(logSnapshot.Output), "ready") {
		t.Fatalf("unexpected process log snapshot: %+v", logSnapshot)
	}
	wrongLogSession := req(t, http.MethodGet, server.URL+"/sessions/sess_other/processes/"+startPayload.ProcessID, nil)
	if wrongLogSession.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-session log status = %d, want 404", wrongLogSession.StatusCode)
	}
	wrongLogSession.Body.Close()

	wrongSession := req(t, http.MethodDelete, server.URL+"/sessions/sess_other/processes/"+startPayload.ProcessID, nil)
	if wrongSession.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-session stop status = %d, want 404", wrongSession.StatusCode)
	}
	wrongSession.Body.Close()
	stopped := req(t, http.MethodDelete, server.URL+"/sessions/sess_process/processes/"+startPayload.ProcessID, nil)
	if stopped.StatusCode != http.StatusNoContent {
		t.Fatalf("stop status = %d, want 204", stopped.StatusCode)
	}
	stopped.Body.Close()
	listed = decodeJSON[processList](t, req(t, http.MethodGet, server.URL+"/sessions/sess_process/processes", nil))
	if len(listed.Processes) != 1 || listed.Processes[0].Running || listed.Processes[0].Status != "stopped" {
		t.Fatalf("stopped process snapshot missing: %+v", listed.Processes)
	}
	sessions = decodeJSON[sessionList](t, req(t, http.MethodGet, server.URL+"/sessions", nil))
	for _, session := range sessions.Sessions {
		if session.ID == "sess_process" && session.BackgroundProcessCount != 0 {
			t.Fatalf("finished background process count = %d, want 0", session.BackgroundProcessCount)
		}
	}
}

func backgroundProcessLogText(chunks []tool.BackgroundProcessOutputChunk) string {
	var text strings.Builder
	for _, chunk := range chunks {
		text.WriteString(chunk.Content)
	}
	return text.String()
}

func TestAPIBackgroundProcessHelper(t *testing.T) {
	if os.Getenv("PUDDING_API_BACKGROUND_HELPER") != "1" {
		return
	}
	fmt.Println("ready")
	time.Sleep(30 * time.Second)
}

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestElectronParentDoneTracksManagedStdinEOF(t *testing.T) {
	t.Setenv("PUDDING_ELECTRON_MANAGED", "1")
	done := electronParentDone(strings.NewReader(""))
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("managed parent EOF was not observed")
	}
}

func TestElectronParentDoneDisabledOutsideElectron(t *testing.T) {
	t.Setenv("PUDDING_ELECTRON_MANAGED", "")
	if done := electronParentDone(strings.NewReader("")); done != nil {
		t.Fatal("unmanaged daemon should not watch stdin")
	}
}

func TestRunToolsReportJSON(t *testing.T) {
	dir := t.TempDir()
	if err := home.Prepare(dir); err != nil {
		t.Fatal(err)
	}
	db, err := sqlitestore.Open(filepath.Join(dir, "data", "pudding.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	err = runTools([]string{"report", "--home", dir, "--days", "7", "--json"}, &stdout, &stderr, time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("run tools report: %v, stderr=%s", err, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("decode report: %v, output=%s", err, stdout.String())
	}
	if payload["totalTurns"] != float64(0) || payload["calls"] != float64(0) {
		t.Fatalf("unexpected empty report: %+v", payload)
	}
}

func TestRunToolsReportValidatesArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runTools([]string{"report", "--days", "0"}, &stdout, &stderr, time.Now())
	if err == nil || !strings.Contains(err.Error(), "between 1 and 3650") {
		t.Fatalf("unexpected days validation: %v", err)
	}
	err = runTools([]string{"unknown"}, &stdout, &stderr, time.Now())
	if err == nil || !strings.Contains(err.Error(), "unknown tools command") {
		t.Fatalf("unexpected command validation: %v", err)
	}
}

func TestRunToolsReportHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runTools([]string{"report", "--help"}, &stdout, &stderr, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "include current built-in tools") {
		t.Fatalf("unexpected report help: %s", stderr.String())
	}
}

func TestRunAgentEvalHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := runAgent([]string{"eval", "--help"}, &stdout, &stderr, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "provider profile ID") || !strings.Contains(stderr.String(), "runs per case") {
		t.Fatalf("unexpected agent eval help: %s", stderr.String())
	}
}

func TestNativeMCPForwardsStdioToRunningDesktop(t *testing.T) {
	const frame = "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ping\"}\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/stdio" || r.Header.Get("Authorization") != "Bearer fixture" || r.Header.Get("Upgrade") != "pudding-stdio" {
			http.Error(w, "invalid handshake", http.StatusForbidden)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		if _, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: pudding-stdio\r\n\r\n"); err != nil {
			t.Error(err)
			return
		}
		line, err := rw.ReadString('\n')
		if err != nil || line != frame {
			t.Errorf("unexpected stdio frame %q, %v", line, err)
			return
		}
		if _, err := io.WriteString(conn, "reply\n"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "mcp"), 0700); err != nil {
		t.Fatal(err)
	}
	descriptor, _ := json.Marshal(map[string]string{"url": server.URL + "/mcp", "token": "fixture"})
	if err := os.WriteFile(filepath.Join(dir, "mcp", "widget-mcp.json"), descriptor, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if err := runMCP([]string{"--home", dir}, strings.NewReader(frame), &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.String() != "reply\n" {
		t.Fatalf("unexpected protocol output %q", stdout.String())
	}
}

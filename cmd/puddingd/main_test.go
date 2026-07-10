package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

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

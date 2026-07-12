package toolreport_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
	"github.com/teatak/pudding-core/internal/tool"
	"github.com/teatak/pudding-core/internal/toolreport"
)

func TestGenerateAggregatesCanonicalToolUsage(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "pudding.db")
	seedToolReportDB(t, path, now)

	report, err := toolreport.Generate(context.Background(), path, toolreport.Options{
		Since: now.Add(-30 * 24 * time.Hour),
		Until: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalTurns != 4 || report.CodeTurns != 2 || report.WorkTurns != 1 || report.ChatTurns != 1 || report.Calls != 6 {
		t.Fatalf("unexpected report totals: %+v", report)
	}
	search := findToolStat(t, report, tool.FileSearch)
	if search.Group != "file" || search.Calls != 3 || search.Turns != 2 || search.Sessions != 1 {
		t.Fatalf("unexpected file search counts: %+v", search)
	}
	if search.RequiredMode != store.ModeCode || search.EligibleTurns != 2 || !near(search.TurnCoverageRate, 1) {
		t.Fatalf("unexpected file search coverage: %+v", search)
	}
	if search.CompletedCalls != 3 || search.SuccessfulCalls != 2 || search.FailedCalls != 1 || !near(search.SuccessRate, 2.0/3.0) {
		t.Fatalf("unexpected file search success: %+v", search)
	}
	if search.RepeatedCalls != 1 || math.Abs(search.RepeatRate-1.0/3.0) > 0.0001 {
		t.Fatalf("unexpected file search repeats: %+v", search)
	}
	if search.CLIFallbacks != 1 || !near(search.CLIFallbackRate, 1) || search.P95ResultBytes != 2000 {
		t.Fatalf("unexpected file search fallback/result size: %+v", search)
	}

	web := findToolStat(t, report, tool.WebSearch)
	if web.RequiredMode != store.ModeChat || web.EligibleTurns != 4 || !near(web.TurnCoverageRate, 0.25) {
		t.Fatalf("unexpected web search coverage: %+v", web)
	}
	rest := findToolStat(t, report, tool.RESTRequest)
	if rest.RequiredMode != store.ModeWork || rest.EligibleTurns != 3 || !near(rest.TurnCoverageRate, 1.0/3.0) {
		t.Fatalf("unexpected REST request coverage: %+v", rest)
	}

	var output bytes.Buffer
	if err := toolreport.WriteText(&output, report); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"Tool usage report", "TURN%", tool.FileSearch, "100.0%", "2.0 KiB"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("formatted report missing %q:\n%s", expected, text)
		}
	}
}

func TestGenerateRejectsMissingDatabase(t *testing.T) {
	now := time.Now()
	_, err := toolreport.Generate(context.Background(), filepath.Join(t.TempDir(), "missing.db"), toolreport.Options{
		Since: now.Add(-time.Hour),
		Until: now,
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unexpected missing database error: %v", err)
	}
}

func TestGenerateCanIncludeUnusedBuiltinTools(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "pudding.db")
	seedToolReportDB(t, path, now)
	report, err := toolreport.Generate(context.Background(), path, toolreport.Options{
		Since:         now.Add(-30 * 24 * time.Hour),
		Until:         now,
		IncludeUnused: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rename := findToolStat(t, report, tool.CodeRename)
	if rename.Calls != 0 || rename.RequiredMode != store.ModeCode || rename.EligibleTurns != 2 || !near(rename.TurnCoverageRate, 0) {
		t.Fatalf("unexpected unused rename stat: %+v", rename)
	}
	process := findToolStat(t, report, tool.CommandStart)
	if process.Group != "command" || process.RequiredMode != store.ModeCode {
		t.Fatalf("unexpected background command stat: %+v", process)
	}
}

func seedToolReportDB(t *testing.T, path string, now time.Time) {
	t.Helper()
	storeDB, err := sqlitestore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storeDB.Close() })

	db, err := sql.Open("sqlite3", path+"?_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	createdAt := now.Add(-time.Hour).UnixMilli()
	if _, err := db.Exec(`INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('session_1','test','test',?,?,?)`, createdAt, createdAt, createdAt); err != nil {
		t.Fatal(err)
	}
	insertTurn(t, db, "turn_project_1", "session_1", store.ModeCode, now.Add(-3*time.Hour))
	insertTurn(t, db, "turn_project_2", "session_1", store.ModeCode, now.Add(-2*time.Hour))
	insertTurn(t, db, "turn_work", "session_1", store.ModeWork, now.Add(-90*time.Minute))
	insertTurn(t, db, "turn_chat", "session_1", store.ModeChat, now.Add(-time.Hour))
	insertTurn(t, db, "turn_old", "session_1", store.ModeCode, now.Add(-40*24*time.Hour))

	index := 0
	insertToolCall(t, db, "session_1", "turn_project_1", &index, "search_1", tool.FileSearch, map[string]any{"scope": "project", "query": "needle"}, false, "failed")
	insertToolCall(t, db, "session_1", "turn_project_1", &index, "command_1", tool.CommandRun, map[string]any{"scope": "project", "argv": []string{"rg", "needle"}}, true, "match")
	index = 0
	insertToolCall(t, db, "session_1", "turn_project_2", &index, "search_2", tool.FileSearch, map[string]any{"scope": "project", "query": "first"}, true, strings.Repeat("x", 100))
	insertToolCall(t, db, "session_1", "turn_project_2", &index, "search_3", tool.FileSearch, map[string]any{"scope": "project", "query": "second"}, true, strings.Repeat("y", 2000))
	index = 0
	insertToolCall(t, db, "session_1", "turn_work", &index, "rest_1", tool.RESTRequest, map[string]any{"endpoint": "demo", "path": "/items"}, true, "result")
	index = 0
	insertToolCall(t, db, "session_1", "turn_chat", &index, "web_1", tool.WebSearch, map[string]any{"query": "news"}, true, "result")
	index = 0
	insertToolCall(t, db, "session_1", "turn_old", &index, "old_1", tool.FileSearch, map[string]any{"scope": "project", "query": "old"}, true, "old")
}

func insertTurn(t *testing.T, db *sql.DB, id, sessionID string, mode store.AgentMode, createdAt time.Time) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO turns(id,session_id,client_message_id,status,provider,model,mode,model_config,created_at,updated_at)
		VALUES(?,?,?,'completed','test','test',?,'{}',?,?)
	`, id, sessionID, "client_"+id, mode, createdAt.UnixMilli(), createdAt.UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
}

func insertToolCall(t *testing.T, db *sql.DB, sessionID, turnID string, index *int, callID, name string, args any, ok bool, content string) {
	t.Helper()
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	useParts, err := json.Marshal([]store.ContentPart{{Type: store.ContentPartToolUse, CallID: callID, Name: name, Args: rawArgs}})
	if err != nil {
		t.Fatal(err)
	}
	resultParts, err := json.Marshal([]store.ContentPart{{Type: store.ContentPartToolResult, CallID: callID, Ok: ok, Content: content}})
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().UnixMilli()
	*index++
	insertMessage(t, db, callID+"_use", sessionID, turnID, "assistant", "tool_use", string(useParts), *index, createdAt)
	*index++
	insertMessage(t, db, callID+"_result", sessionID, turnID, "tool", "tool_result", string(resultParts), *index, createdAt)
}

func insertMessage(t *testing.T, db *sql.DB, id, sessionID, turnID, role, kind, parts string, turnIndex int, createdAt int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT INTO messages(id,session_id,turn_id,role,kind,text,parts,turn_index,created_at)
		VALUES(?,?,?,?,?,'',?,?,?)
	`, id, sessionID, turnID, role, kind, parts, turnIndex, createdAt)
	if err != nil {
		t.Fatal(err)
	}
}

func findToolStat(t *testing.T, report toolreport.Report, name string) toolreport.ToolStat {
	t.Helper()
	for _, stat := range report.Tools {
		if stat.Name == name {
			return stat
		}
	}
	t.Fatalf("tool stat %q not found: %+v", name, report.Tools)
	return toolreport.ToolStat{}
}

func near(value *float64, expected float64) bool {
	return value != nil && math.Abs(*value-expected) < 0.0001
}

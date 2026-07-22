// Package toolreport derives local tool-usage statistics from canonical messages.
package toolreport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type Options struct {
	Since         time.Time
	Until         time.Time
	IncludeUnused bool
}

type Report struct {
	GeneratedAt      time.Time  `json:"generatedAt"`
	Since            time.Time  `json:"since"`
	Until            time.Time  `json:"until"`
	TotalTurns       int        `json:"totalTurns"`
	ChatTurns        int        `json:"chatTurns"`
	WorkTurns        int        `json:"workTurns"`
	CodeTurns        int        `json:"codeTurns"`
	UnknownModeTurns int        `json:"unknownModeTurns,omitempty"`
	Calls            int        `json:"calls"`
	SkippedMessages  int        `json:"skippedMessages,omitempty"`
	Tools            []ToolStat `json:"tools"`
}

type ToolStat struct {
	Name             string          `json:"name"`
	Group            string          `json:"group"`
	RequiredMode     store.AgentMode `json:"requiredMode,omitempty"`
	Calls            int             `json:"calls"`
	Turns            int             `json:"turns"`
	Sessions         int             `json:"sessions"`
	EligibleTurns    int             `json:"eligibleTurns,omitempty"`
	TurnCoverageRate *float64        `json:"turnCoverageRate,omitempty"`
	CompletedCalls   int             `json:"completedCalls"`
	SuccessfulCalls  int             `json:"successfulCalls"`
	FailedCalls      int             `json:"failedCalls"`
	SuccessRate      *float64        `json:"successRate,omitempty"`
	RepeatedCalls    int             `json:"repeatedCalls"`
	RepeatRate       float64         `json:"repeatRate"`
	CLIFallbacks     int             `json:"cliFallbacks"`
	CLIFallbackRate  *float64        `json:"cliFallbackRate,omitempty"`
	P95ResultBytes   int             `json:"p95ResultBytes"`
}

type turnKey struct {
	sessionID string
	turnID    string
}

type callKey struct {
	turnKey
	callID string
}

type callRecord struct {
	turn        turnKey
	name        string
	args        json.RawMessage
	resultSeen  bool
	ok          bool
	resultBytes int
}

type statAccumulator struct {
	stat        ToolStat
	turns       map[turnKey]struct{}
	sessions    map[string]struct{}
	resultSizes []int
}

func Generate(ctx context.Context, dbPath string, options Options) (Report, error) {
	if options.Since.IsZero() || options.Until.IsZero() || !options.Since.Before(options.Until) {
		return Report{}, errors.New("tool report requires a valid since/until window")
	}
	db, err := openReadOnly(dbPath)
	if err != nil {
		return Report{}, err
	}
	defer db.Close()

	report := Report{
		GeneratedAt: options.Until.UTC(),
		Since:       options.Since.UTC(),
		Until:       options.Until.UTC(),
	}
	turns, err := loadTurns(ctx, db, options, &report)
	if err != nil {
		return Report{}, err
	}
	calls, turnCalls, skipped, err := loadCalls(ctx, db, options, turns)
	if err != nil {
		return Report{}, err
	}
	report.SkippedMessages = skipped
	report.Tools, report.Calls = aggregate(calls, turnCalls, report, options.IncludeUnused)
	return report, nil
}

func openReadOnly(path string) (*sql.DB, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("tool report database path is required")
	}
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("tool report database does not exist: %s", path)
		}
		return nil, fmt.Errorf("tool report inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("tool report database is not a regular file: %s", path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("tool report resolve database path: %w", err)
	}
	uri := &url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	query := uri.Query()
	query.Set("mode", "ro")
	query.Set("_busy_timeout", "5000")
	uri.RawQuery = query.Encode()
	db, err := sql.Open("sqlite3", uri.String())
	if err != nil {
		return nil, fmt.Errorf("tool report open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("tool report read database: %w", err)
	}
	return db, nil
}

func loadTurns(ctx context.Context, db *sql.DB, options Options, report *Report) (map[turnKey]store.AgentMode, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT session_id, id, mode
		FROM turns
		WHERE created_at >= ? AND created_at < ?
	`, options.Since.UnixMilli(), options.Until.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("tool report query turns: %w", err)
	}
	defer rows.Close()

	turns := make(map[turnKey]store.AgentMode)
	for rows.Next() {
		var sessionID, turnID string
		var rawMode store.AgentMode
		if err := rows.Scan(&sessionID, &turnID, &rawMode); err != nil {
			return nil, fmt.Errorf("tool report scan turn: %w", err)
		}
		mode := store.NormalizeAgentMode(rawMode)
		turns[turnKey{sessionID: sessionID, turnID: turnID}] = mode
		report.TotalTurns++
		switch mode {
		case store.ModeChat:
			report.ChatTurns++
		case store.ModeWork:
			report.WorkTurns++
		case store.ModeCode:
			report.CodeTurns++
		default:
			report.UnknownModeTurns++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool report read turns: %w", err)
	}
	return turns, nil
}

func loadCalls(ctx context.Context, db *sql.DB, options Options, turns map[turnKey]store.AgentMode) ([]*callRecord, map[turnKey][]*callRecord, int, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT m.session_id, m.turn_id, m.parts
		FROM messages AS m
		JOIN turns AS t ON t.id = m.turn_id AND t.session_id = m.session_id
		WHERE t.created_at >= ? AND t.created_at < ?
		  AND m.kind IN ('tool_use', 'tool_result')
		ORDER BY m.session_id, m.turn_id, m.turn_index, m.created_at, m.id
	`, options.Since.UnixMilli(), options.Until.UnixMilli())
	if err != nil {
		return nil, nil, 0, fmt.Errorf("tool report query messages: %w", err)
	}
	defer rows.Close()

	var calls []*callRecord
	turnCalls := make(map[turnKey][]*callRecord)
	byID := make(map[callKey]*callRecord)
	skipped := 0
	for rows.Next() {
		var sessionID, turnID, rawParts string
		if err := rows.Scan(&sessionID, &turnID, &rawParts); err != nil {
			return nil, nil, skipped, fmt.Errorf("tool report scan message: %w", err)
		}
		tk := turnKey{sessionID: sessionID, turnID: turnID}
		if _, ok := turns[tk]; !ok {
			continue
		}
		var parts []store.ContentPart
		if err := json.Unmarshal([]byte(rawParts), &parts); err != nil {
			skipped++
			continue
		}
		for _, part := range parts {
			switch part.Type {
			case store.ContentPartToolUse:
				name := strings.TrimSpace(part.Name)
				if name == "" {
					continue
				}
				record := &callRecord{turn: tk, name: name, args: append(json.RawMessage(nil), part.Args...)}
				calls = append(calls, record)
				turnCalls[tk] = append(turnCalls[tk], record)
				if callID := strings.TrimSpace(part.CallID); callID != "" {
					key := callKey{turnKey: tk, callID: callID}
					if _, exists := byID[key]; !exists {
						byID[key] = record
					}
				}
			case store.ContentPartToolResult:
				callID := strings.TrimSpace(part.CallID)
				if callID == "" {
					continue
				}
				record := byID[callKey{turnKey: tk, callID: callID}]
				if record == nil || record.resultSeen {
					continue
				}
				record.resultSeen = true
				record.ok = part.Ok
				record.resultBytes = len([]byte(part.Content))
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, skipped, fmt.Errorf("tool report read messages: %w", err)
	}
	return calls, turnCalls, skipped, nil
}

func aggregate(calls []*callRecord, turnCalls map[turnKey][]*callRecord, report Report, includeUnused bool) ([]ToolStat, int) {
	accumulators := make(map[string]*statAccumulator)
	for _, call := range calls {
		acc := accumulators[call.name]
		if acc == nil {
			acc = &statAccumulator{
				stat:     ToolStat{Name: call.name, Group: toolGroup(call.name)},
				turns:    make(map[turnKey]struct{}),
				sessions: make(map[string]struct{}),
			}
			accumulators[call.name] = acc
		}
		acc.stat.Calls++
		acc.turns[call.turn] = struct{}{}
		acc.sessions[call.turn.sessionID] = struct{}{}
		if call.resultSeen {
			acc.stat.CompletedCalls++
			acc.resultSizes = append(acc.resultSizes, call.resultBytes)
			if call.ok {
				acc.stat.SuccessfulCalls++
			} else {
				acc.stat.FailedCalls++
			}
		}
	}

	for _, items := range turnCalls {
		counts := make(map[string]int)
		for _, call := range items {
			counts[call.name]++
		}
		for name, count := range counts {
			if count > 1 {
				accumulators[name].stat.RepeatedCalls += count - 1
			}
		}
		for index, call := range items {
			if !call.resultSeen || call.ok || call.name == tool.CommandRun {
				continue
			}
			domain := toolDomain(call.name)
			if domain == "" {
				continue
			}
			for _, later := range items[index+1:] {
				if later.name == tool.CommandRun && commandDomain(later.args) == domain {
					accumulators[call.name].stat.CLIFallbacks++
					break
				}
			}
		}
	}

	requiredModes := currentRequiredModes()
	if includeUnused {
		for name := range requiredModes {
			if accumulators[name] == nil {
				accumulators[name] = &statAccumulator{
					stat:     ToolStat{Name: name, Group: toolGroup(name)},
					turns:    make(map[turnKey]struct{}),
					sessions: make(map[string]struct{}),
				}
			}
		}
	}
	stats := make([]ToolStat, 0, len(accumulators))
	for name, acc := range accumulators {
		acc.stat.Turns = len(acc.turns)
		acc.stat.Sessions = len(acc.sessions)
		if required, known := requiredModes[name]; known {
			acc.stat.RequiredMode = required
			switch required {
			case store.ModeChat:
				acc.stat.EligibleTurns = report.TotalTurns
			case store.ModeWork:
				acc.stat.EligibleTurns = report.WorkTurns + report.CodeTurns
			case store.ModeCode:
				acc.stat.EligibleTurns = report.CodeTurns
			}
			acc.stat.TurnCoverageRate = ratioPointer(acc.stat.Turns, acc.stat.EligibleTurns)
		}
		acc.stat.SuccessRate = ratioPointer(acc.stat.SuccessfulCalls, acc.stat.CompletedCalls)
		if acc.stat.Calls > 0 {
			acc.stat.RepeatRate = float64(acc.stat.RepeatedCalls) / float64(acc.stat.Calls)
		}
		if toolDomain(name) != "" {
			acc.stat.CLIFallbackRate = ratioPointer(acc.stat.CLIFallbacks, acc.stat.FailedCalls)
		}
		acc.stat.P95ResultBytes = percentile95(acc.resultSizes)
		stats = append(stats, acc.stat)
	}
	sort.Slice(stats, func(i, j int) bool {
		if stats[i].Calls != stats[j].Calls {
			return stats[i].Calls > stats[j].Calls
		}
		return stats[i].Name < stats[j].Name
	})
	return stats, len(calls)
}

func currentRequiredModes() map[string]store.AgentMode {
	modes := map[string]store.AgentMode{tool.RequestCapability: store.ModeChat, tool.AppLoad: store.ModeChat, tool.AppUnload: store.ModeChat}
	for _, definition := range tool.BuiltinDefinitions() {
		mode := store.NormalizeAgentMode(definition.Capability)
		if mode == "" {
			mode = store.ModeCode
		}
		modes[definition.Name] = mode
	}
	return modes
}

func ratioPointer(numerator, denominator int) *float64 {
	if denominator <= 0 {
		return nil
	}
	value := float64(numerator) / float64(denominator)
	return &value
}

func percentile95(values []int) int {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]int(nil), values...)
	sort.Ints(ordered)
	index := int(math.Ceil(float64(len(ordered))*0.95)) - 1
	if index < 0 {
		index = 0
	}
	return ordered[index]
}

func toolGroup(name string) string {
	switch {
	case strings.HasPrefix(name, "builtin_command_"):
		return "command"
	case name == tool.AppLoad, name == tool.AppUnload:
		return "app"
	case name == tool.RequestCapability, strings.HasPrefix(name, "builtin_project_"):
		return "project"
	case strings.HasPrefix(name, "builtin_code_"):
		return "code"
	case strings.HasPrefix(name, "builtin_file_"):
		return "file"
	case strings.HasPrefix(name, "builtin_git_"):
		return "git"
	case strings.HasPrefix(name, "builtin_history_"):
		return "history"
	case strings.HasPrefix(name, "builtin_browser_"):
		return "browser"
	case strings.HasPrefix(name, "builtin_web_"):
		return "web"
	case strings.HasPrefix(name, "builtin_graphql_"), strings.HasPrefix(name, "builtin_rest_"):
		return "api"
	case strings.HasPrefix(name, "builtin_skill_"):
		return "skill"
	case strings.HasPrefix(name, "builtin_attachment_"):
		return "attachment"
	case name == tool.RequestUserInput:
		return "interaction"
	default:
		return "other"
	}
}

func toolDomain(name string) string {
	switch {
	case strings.HasPrefix(name, "builtin_git_"):
		return "git"
	case strings.HasPrefix(name, "builtin_file_"):
		return "file"
	case strings.HasPrefix(name, "builtin_code_"):
		return "code"
	default:
		return ""
	}
}

func commandDomain(raw json.RawMessage) string {
	var args struct {
		Command string `json:"command"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return ""
	}
	fields := strings.Fields(args.Command)
	if len(fields) == 0 {
		return ""
	}
	executable := strings.ToLower(filepath.Base(strings.Trim(fields[0], "'\"")))
	executable = strings.TrimSuffix(executable, ".exe")
	switch executable {
	case "git":
		return "git"
	case "cat", "fd", "find", "grep", "head", "ls", "realpath", "rg", "ripgrep", "sed", "stat", "tail", "tree", "wc":
		return "file"
	case "gopls", "tsserver", "typescript-language-server":
		return "code"
	default:
		return ""
	}
}

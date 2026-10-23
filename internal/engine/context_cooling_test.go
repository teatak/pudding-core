package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/mock"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
	"github.com/teatak/pudding-core/internal/tool"
)

type coolingCommitStore struct {
	store.Store
	commits int
}

func (s *coolingCommitStore) AdvanceContextCooling(ctx context.Context, in store.AdvanceContextCoolingInput) error {
	s.commits++
	return s.Store.AdvanceContextCooling(ctx, in)
}

func coolingFixture(t *testing.T) (*Engine, *storetest.Store, *resolvedModel, provider.Request) {
	t.Helper()
	e, db, _, sid := newTestEngine(t, mock.WithScript([]string{"summary of old evidence"}), mock.WithDelay(0))
	t.Cleanup(e.Stop)
	ctx := context.Background()
	for i := 0; i < 6; i++ {
		id := fmt.Sprint(i)
		if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: id, UserMessageID: "input" + id, ClientMessageID: "client" + id, UserText: "inspect"}); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"ok": true, "stdout": strings.Repeat("historical evidence ", 500), "stderr": "", "exitCode": 0})
		if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: store.TurnCompleted, AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, Name: tool.CommandRun, CallID: "call", Args: []byte(`{}`)},
			{Type: store.ContentPartToolResult, Name: tool.CommandRun, CallID: "call", Ok: true, Content: string(raw)},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "current", UserMessageID: "current-input", ClientMessageID: "current-client", UserText: "continue"}); err != nil {
		t.Fatal(err)
	}
	e.tools = &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.CommandRun}, {Name: tool.HistoryGetMessage}}}
	defs, err := e.toolDefinitions(ctx, sid, store.ModeCode)
	if err != nil {
		t.Fatal(err)
	}
	req, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", defs)
	if err != nil {
		t.Fatal(err)
	}
	req.Tools = defs
	// The window fits the canonical summary, while the ordinary request crosses
	// 80%. Four cooled results are enough to fall below that threshold.
	before := requestInputTokens(req)
	cfg := provider.ModelConfig{ContextWindow: before * 11 / 10}
	req.Config = cfg
	return e, db, &resolvedModel{providerName: "mock", model: "mock-model", protocol: "openai-compatible", config: cfg}, req
}

func TestCoolingBeforeFirstRequestPreservesSummaryAndOriginals(t *testing.T) {
	e, db, resolved, req := coolingFixture(t)
	ctx, sid := context.Background(), "sess_1"
	all, err := db.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	summaries := make([]string, len(all))
	for i, m := range all {
		summaries[i] = compactMessageText(m)
	}
	changed, err := e.prepareContextRequest(ctx, sid, "current", resolved, store.ModeCode, &req, true)
	if err != nil || !changed {
		t.Fatalf("cooling failed: %v %v", changed, err)
	}
	state, err := db.ContextCooling(ctx, sid)
	if err != nil || state.ThroughTurnID != "3" {
		t.Fatalf("state: %+v %v", state, err)
	}
	usage, err := db.SessionUsage(ctx, sid)
	if err != nil || usage.RequestCount != 0 {
		t.Fatalf("cooling made a model request: %+v %v", usage, err)
	}
	after, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", req.Tools, resolved.config)
	if err != nil {
		t.Fatal(err)
	}
	after.Config, after.Tools = resolved.config, req.Tools
	if requestInputTokens(after) >= compactTrigger(resolved.config, 80) {
		t.Fatal("cooling did not avoid Compact")
	}
	canonical, err := db.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, canonical) {
		t.Fatal("cooling changed canonical history")
	}
	for i, m := range canonical {
		if compactMessageText(m) != summaries[i] {
			t.Fatal("cooling changed Compact input")
		}
	}
	raw := tool.NewBuiltinRunner(tool.WithHistorySearch(db)).Call(ctx, tool.Call{SessionID: sid, CallID: "read", Name: tool.HistoryGetMessage, Args: []byte(`{"result_ref":{"turn_id":"0","call_id":"call"},"field":"stdout","offset":0,"limit":100}`)})
	if !raw.Ok || !strings.Contains(raw.Content, "historical evidence") {
		t.Fatalf("readback: %s", raw.Content)
	}
	if err := db.SetSettings(ctx, map[string]string{config.SettingToolResultCoolingEnabled: "false"}); err != nil {
		t.Fatal(err)
	}
	restored, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", req.Tools, resolved.config)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(req.Messages, restored.Messages) {
		t.Fatal("switch off did not restore original projection")
	}
}

func notificationCoolingFixture(t *testing.T) (*Engine, *storetest.Store, *resolvedModel, provider.Request, []*store.Message) {
	t.Helper()
	e, db, _, sid := newTestEngine(t, mock.WithScript([]string{"summary of old notifications"}), mock.WithDelay(0))
	t.Cleanup(e.Stop)
	ctx := context.Background()
	var notices []*store.Message
	for i := 0; i < 6; i++ {
		id := fmt.Sprint(i)
		if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: id, UserMessageID: "input" + id, ClientMessageID: "input" + id, UserText: "continue"}); err != nil {
			t.Fatal(err)
		}
		payload := map[string]any{"title": "Game", "runID": "run", "notificationID": id, "summary": "South to play", "data": strings.Repeat("notification evidence ", 400)}
		body, _ := json.Marshal(payload)
		metadata, _ := json.Marshal(map[string]any{"widgetNotification": payload})
		notice, err := db.RecordWidgetNotice(ctx, store.WidgetNoticeInput{SessionID: sid, ClientMessageID: "notice" + id, Text: string(body), Metadata: metadata})
		if err != nil {
			t.Fatal(err)
		}
		notices = append(notices, notice.Message)
		if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: id, Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: sid, TurnID: "current", UserMessageID: "current", ClientMessageID: "current", UserText: "continue"}); err != nil {
		t.Fatal(err)
	}
	e.tools = &recordingToolRunner{defs: []provider.ToolDef{{Name: tool.HistoryGetMessage}}}
	defs, err := e.toolDefinitions(ctx, sid, store.ModeCode)
	if err != nil {
		t.Fatal(err)
	}
	req, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", defs)
	if err != nil {
		t.Fatal(err)
	}
	req.Tools = defs
	before := requestInputTokens(req)
	cfg := provider.ModelConfig{ContextWindow: before * 11 / 10}
	req.Config = cfg
	resolved := &resolvedModel{providerName: "mock", model: "mock-model", protocol: "openai-compatible", config: cfg}
	return e, db, resolved, req, notices
}

func TestNotificationCoolingPreservesReadbackSummaryAndUsage(t *testing.T) {
	e, db, resolved, req, notices := notificationCoolingFixture(t)
	ctx, sid := context.Background(), "sess_1"
	before, cfg, defs := requestInputTokens(req), resolved.config, req.Tools
	canonical, err := db.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := messageInputTurnCount(canonical); got != 7 {
		t.Fatalf("counted notification receipts as execution turns: %d", got)
	}
	changed, err := e.prepareContextRequest(ctx, sid, "current", resolved, store.ModeCode, &req, true)
	if err != nil || !changed {
		t.Fatalf("notification cooling failed: %v %v", changed, err)
	}
	state, err := db.ContextCooling(ctx, sid)
	if err != nil || state.ThroughTurnID != notices[3].TurnID {
		t.Fatalf("wrong notification watermark: %+v %v", state, err)
	}
	after, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", defs, cfg)
	if err != nil {
		t.Fatal(err)
	}
	after.Config, after.Tools = cfg, defs
	if tokens := requestInputTokens(after); tokens >= before*80/100 {
		t.Fatalf("estimate did not include notification cooling: before=%d after=%d", before, tokens)
	}
	stored, err := db.ListMessages(ctx, sid, 0)
	if err != nil || !reflect.DeepEqual(canonical, stored) {
		t.Fatalf("changed canonical notification history: %v", err)
	}
	for i, m := range stored {
		if compactMessageText(m) != compactMessageText(canonical[i]) {
			t.Fatal("cooling changed Compact input")
		}
	}
	usage, err := db.SessionUsage(ctx, sid)
	if err != nil || usage.RequestCount != 0 {
		t.Fatalf("cooling made a model request: %+v %v", usage, err)
	}
	args, _ := json.Marshal(map[string]string{"message_id": notices[0].ID})
	read := tool.NewBuiltinRunner(tool.WithHistorySearch(db)).Call(ctx, tool.Call{SessionID: sid, CallID: "read", Name: tool.HistoryGetMessage, Args: args})
	if !read.Ok || !strings.Contains(read.Content, "notification evidence") || strings.Contains(read.Content, "cold_only") {
		t.Fatalf("notification readback lost original: %s", read.Content)
	}
	if err := db.SetSettings(ctx, map[string]string{config.SettingToolResultCoolingEnabled: "false"}); err != nil {
		t.Fatal(err)
	}
	restored, err := e.builder.BuildForProviderWithTools(ctx, sid, "mock", "mock-model", "code", defs, cfg)
	if err != nil || !reflect.DeepEqual(req.Messages, restored.Messages) {
		t.Fatalf("switch off did not restore notifications: %v", err)
	}
}

func TestNotificationCoolingInsufficientGoesDirectlyToCompact(t *testing.T) {
	e, db, resolved, req, _ := notificationCoolingFixture(t)
	ctx, sid := context.Background(), "sess_1"
	commits := &coolingCommitStore{Store: db}
	e.store = commits
	e.rebuildBuilder()
	all, err := db.ListMessages(ctx, sid, 0)
	if err != nil {
		t.Fatal(err)
	}
	state := contextbuilder.CoolingCandidate(all, "current", 5)
	trial, err := e.builder.BuildForProviderWithHistory(ctx, sid, "mock", "mock-model", "code", req.Tools, all, resolved.config, state)
	if err != nil {
		t.Fatal(err)
	}
	trial.Config, trial.Tools = resolved.config, req.Tools
	after := requestInputTokens(trial)
	if after >= requestInputTokens(req) {
		t.Fatal("fixture must provide a real but insufficient cooling gain")
	}
	if pressure, err := summaryInputPressure(resolved, all, 1); err != nil || pressure {
		t.Fatalf("fixture must reach the cooling trial: %v %v", pressure, err)
	}
	threshold := after * 100 / resolved.config.ContextWindow
	if err := db.SetSettings(ctx, map[string]string{
		config.SettingToolResultCoolingKeepTurns: "5", config.SettingCompactAutoThresholdPercent: fmt.Sprint(threshold),
	}); err != nil {
		t.Fatal(err)
	}
	changed, err := e.prepareContextRequest(ctx, sid, "current", resolved, store.ModeCode, &req, true)
	if err != nil || !changed {
		t.Fatalf("direct Compact failed: %v %v", changed, err)
	}
	if commits.commits != 0 {
		t.Fatal("committed notification cooling before Compact")
	}
	state, err = db.ContextCooling(ctx, sid)
	if err != nil || state.ThroughTurnID != "" {
		t.Fatalf("unexpected watermark: %+v %v", state, err)
	}
	all, err = db.ListMessages(ctx, sid, 0)
	if err != nil || all[len(all)-1].Role != store.RoleSummary {
		t.Fatalf("missing Compact summary: %v", err)
	}
}

func TestCoolingIneligibleGoesDirectlyToCompact(t *testing.T) {
	for _, reason := range []string{"mid-turn", "disabled", "insufficient"} {
		t.Run(reason, func(t *testing.T) {
			e, db, resolved, req := coolingFixture(t)
			commits := &coolingCommitStore{Store: db}
			e.store = commits
			e.rebuildBuilder()
			allow := true
			switch reason {
			case "mid-turn":
				allow = false
			case "disabled":
				if err := db.SetSettings(context.Background(), map[string]string{config.SettingToolResultCoolingEnabled: "false"}); err != nil {
					t.Fatal(err)
				}
			case "insufficient":
				if err := db.SetSettings(context.Background(), map[string]string{config.SettingToolResultCoolingKeepTurns: "5"}); err != nil {
					t.Fatal(err)
				}
			}
			changed, err := e.prepareContextRequest(context.Background(), "sess_1", "current", resolved, store.ModeCode, &req, allow)
			if err != nil || !changed {
				t.Fatalf("compact failed: changed=%v err=%v", changed, err)
			}
			if commits.commits != 0 {
				t.Fatal("ineffective cooling was committed before Compact")
			}
			state, err := db.ContextCooling(context.Background(), "sess_1")
			if err != nil || state.ThroughTurnID != "" {
				t.Fatalf("wrote cooling: %+v %v", state, err)
			}
			all, err := db.ListMessages(context.Background(), "sess_1", 0)
			if err != nil {
				t.Fatal(err)
			}
			if all[len(all)-1].Role != store.RoleSummary {
				t.Fatal("did not Compact")
			}
		})
	}
}

func TestCoolingRequiresPressureAndKnownWindow(t *testing.T) {
	for _, reason := range []string{"below-threshold", "unknown-window", "disabled-threshold"} {
		t.Run(reason, func(t *testing.T) {
			e, db, resolved, req := coolingFixture(t)
			switch reason {
			case "below-threshold":
				resolved.config.ContextWindow *= 3
			case "unknown-window":
				resolved.config.ContextWindow = 0
			case "disabled-threshold":
				if err := db.SetSettings(context.Background(), map[string]string{config.SettingCompactAutoThresholdPercent: "0"}); err != nil {
					t.Fatal(err)
				}
			}
			req.Config = resolved.config
			changed, err := e.prepareContextRequest(context.Background(), "sess_1", "current", resolved, store.ModeCode, &req, true)
			if err != nil || changed {
				t.Fatalf("unexpected context change: %v %v", changed, err)
			}
			state, err := db.ContextCooling(context.Background(), "sess_1")
			if err != nil || state.ThroughTurnID != "" {
				t.Fatalf("state: %+v %v", state, err)
			}
		})
	}
}

func TestCoolingDefersIdleCompactUntilNextInput(t *testing.T) {
	e, db, _, _ := coolingFixture(t)
	ctx := context.Background()
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "current", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	if err := e.maybeAutoCompact(ctx, "sess_1"); err != nil {
		t.Fatal(err)
	}
	all, err := db.ListMessages(ctx, "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range all {
		if m.Role == store.RoleSummary {
			t.Fatal("idle Compact preempted cooling")
		}
	}
	if len(contextbuilder.EffectiveMessages(all)) != len(all) {
		t.Fatal("idle boundary changed")
	}
}

func TestCoolingUsesCalibratedCandidate(t *testing.T) {
	e, db, resolved, req := coolingFixture(t)
	ctx := context.Background()
	if _, err := db.RecordSessionUsage(ctx, "sess_1", store.UsageRecordInput{Provider: "mock", Model: "mock-model", EstimatedInputTokens: 100, InputUncachedTokens: 200}); err != nil {
		t.Fatal(err)
	}
	resolved.config.ContextWindow *= 2
	req.Config = resolved.config
	if requestInputTokens(req) >= compactTrigger(req.Config, 80) {
		t.Fatal("fixture must be below the uncalibrated threshold")
	}
	changed, err := e.prepareContextRequest(ctx, "sess_1", "current", resolved, store.ModeCode, &req, true)
	if err != nil || !changed {
		t.Fatalf("calibrated cooling: %v %v", changed, err)
	}
	state, err := db.ContextCooling(ctx, "sess_1")
	if err != nil || state.ThroughTurnID != "3" {
		t.Fatalf("calibrated projection not committed: %+v %v", state, err)
	}
}

func TestCoolingStillChecksCanonicalSummaryBudget(t *testing.T) {
	e, db, resolved, req := coolingFixture(t)
	ctx := context.Background()
	if changed, err := e.prepareContextRequest(ctx, "sess_1", "current", resolved, store.ModeCode, &req, true); err != nil || !changed {
		t.Fatalf("initial cooling: %v %v", changed, err)
	}
	all, err := db.ListMessages(ctx, "sess_1", 0)
	if err != nil {
		t.Fatal(err)
	}
	history, err := compactHistoryText(all, all)
	if err != nil {
		t.Fatal(err)
	}
	summaryReq, err := compactSummaryRequest(resolved, history, "", compactSummaryMaxTokens)
	if err != nil {
		t.Fatal(err)
	}
	input := requestInputTokens(summaryReq)
	resolved.config.ContextWindow = (input + compactInputMargin(input)) * 100 / 90
	cooled, err := e.builder.BuildForProviderWithTools(ctx, "sess_1", "mock", "mock-model", "code", req.Tools, resolved.config)
	if err != nil {
		t.Fatal(err)
	}
	cooled.Config, cooled.Tools = resolved.config, req.Tools
	if requestInputTokens(cooled) >= compactTrigger(cooled.Config, 80) {
		t.Fatal("fixture must fit ordinary request below threshold")
	}
	changed, err := e.prepareContextRequest(ctx, "sess_1", "current", resolved, store.ModeCode, &cooled, false)
	if err != nil || !changed {
		t.Fatalf("canonical budget did not trigger Compact: %v %v", changed, err)
	}
	state, err := db.ContextCooling(ctx, "sess_1")
	if err != nil || state.ThroughTurnID != "" {
		t.Fatalf("Compact did not reset cooling: %+v %v", state, err)
	}
}

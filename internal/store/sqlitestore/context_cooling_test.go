package sqlitestore

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func TestCoolingWatermarkIsStableAtomicAndResetByCompact(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "cooling.db")
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.CreateSession(ctx, &store.Session{ID: "s", Provider: "mock", Model: "model"}); err != nil {
		t.Fatal(err)
	}
	begin := func(i int) {
		id := fmt.Sprint(i)
		if _, err := db.BeginTurn(ctx, store.BeginTurnInput{SessionID: "s", TurnID: id, UserMessageID: "input" + id, ClientMessageID: "client" + id, UserText: "input"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 4; i++ {
		begin(i)
		if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: fmt.Sprint(i), Status: store.TurnCompleted, AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, Name: tool.CommandRun, CallID: "call", Args: []byte(`{}`)},
			{Type: store.ContentPartToolResult, Name: tool.CommandRun, CallID: "call", Ok: true, Content: strings.Repeat("result ", 1000)},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	begin(4)
	all, err := db.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	next := contextbuilder.CoolingCandidate(all, "4", 2)
	if next.ThroughTurnID != "1" {
		t.Fatalf("wrong hot tail: %+v", next)
	}
	for keep, want := range map[int]string{0: "3", 1: "2", 3: "0", 4: "", 50: ""} {
		if got := contextbuilder.CoolingCandidate(all, "4", keep); got.ThroughTurnID != want {
			t.Fatalf("keep %d: %+v", keep, got)
		}
	}
	in := store.AdvanceContextCoolingInput{SessionID: "s", RunningTurnID: "4", ExpectedLastMessageID: all[len(all)-1].ID, Next: next}
	stale := in
	stale.ExpectedLastMessageID = "stale"
	if err := db.AdvanceContextCooling(ctx, stale); !errors.Is(err, store.ErrHistoryChanged) {
		t.Fatalf("stale: %v", err)
	}
	if err := db.AdvanceContextCooling(ctx, in); err != nil {
		t.Fatal(err)
	}
	if err := db.AdvanceContextCooling(ctx, in); !errors.Is(err, store.ErrHistoryChanged) {
		t.Fatalf("CAS: %v", err)
	}
	build := func() provider.Request {
		req, err := contextbuilder.New(db, nil).BuildForProviderWithTools(ctx, "s", "mock", "model", "code", []provider.ToolDef{{Name: tool.CommandRun}, {Name: tool.HistoryGetMessage}}, provider.ModelConfig{})
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	before := build()
	var results []string
	for _, m := range before.Messages {
		for _, p := range m.Parts {
			if p.Type == provider.PartToolResult {
				results = append(results, p.Content)
			}
		}
	}
	if len(results) != 4 || !strings.Contains(results[0], `"cold_only":true`) || !strings.Contains(results[1], `"cold_only":true`) || strings.Contains(results[2], `"cold_only"`) || strings.Contains(results[3], `"cold_only"`) {
		t.Fatalf("bad cold/hot partition: %v", results)
	}
	canonical, err := db.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(all, canonical) {
		t.Fatal("projection rewrote canonical history")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, build()) {
		t.Fatal("restart changed projection")
	}
	if _, err := db.FinishTurn(ctx, store.FinishTurnInput{TurnID: "4", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	begin(5)
	after := build()
	if !reflect.DeepEqual(before.Messages, after.Messages[:len(before.Messages)]) {
		t.Fatal("new turn silently slid watermark")
	}
	all, err = db.ListMessages(ctx, "s", 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.AppendCompactSummary(ctx, store.AppendCompactSummaryInput{SessionID: "s", RunningTurnID: "5", TurnID: "5", MessageID: "summary", Text: "summary", ExpectedLastMessageID: all[len(all)-1].ID, Metadata: store.CompactMessageMetadata([]string{all[0].ID}, nil)})
	if err != nil {
		t.Fatal(err)
	}
	state, err := db.ContextCooling(ctx, "s")
	if err != nil || state != (store.ContextCooling{}) {
		t.Fatalf("compact did not reset: %+v %v", state, err)
	}
	if err := db.AdvanceContextCooling(ctx, in); !errors.Is(err, store.ErrHistoryChanged) {
		t.Fatalf("pre-compact state accepted: %v", err)
	}
}

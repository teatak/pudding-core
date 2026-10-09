package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func notificationEngine(t *testing.T) (*Engine, *storetest.Store, context.Context, *WidgetRun) {
	t.Helper()
	e, st, _, sid := newTestEngine(t)
	e.widgetRuns.once.Do(func() {}) // Drive the same scheduler deterministically.
	t.Cleanup(func() { e.Stop(); e.Wait() })
	ctx := plugin.WithRuntimeID(context.Background(), "desktop-test")
	if err := st.CreateSession(ctx, &store.Session{ID: "second", Title: "Second", Provider: "mock", Model: "mock-model"}); err != nil {
		t.Fatal(err)
	}
	item, err := st.CreateStudioItem(ctx, &store.StudioItem{ID: "widget_test", Kind: "widget", Name: "Shared form", ActiveRevision: strings.Repeat("a", 64), BindingVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	item, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: item.ID, Hash: hash, ClientRequestID: "source", CreatedAt: time.Now()}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.PutWidgetBuildReceipt(ctx, item.ID, hash, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	item.ActiveRevision = hash
	item, err = st.UpdateStudioItem(ctx, item, item.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SelectWidgetPage(ctx, item.ID, "library", ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.OpenWidgetPage(ctx, item.ID, "library", hash, "target"); err != nil {
		t.Fatal(err)
	}
	run, err := e.CreateWidgetRun(ctx, item.ID, WidgetRunCreate{TargetID: "target", RevisionHash: item.ActiveRevision, BindingVersion: item.BindingVersion, Participants: []WidgetParticipant{{SessionID: sid, Roles: []string{"reviewer", "owner"}}, {SessionID: "second", Roles: []string{"editor"}}, {Roles: []string{"observer"}}}})
	if err != nil {
		t.Fatal(err)
	}
	return e, st, ctx, run
}
func TestWidgetInformScopesAndImmutableReceipts(t *testing.T) {
	e, st, ctx, run := notificationEngine(t)
	n := WidgetNotification{ID: "started", Audience: WidgetAudience{Kind: "all"}, Delivery: "inform", Topic: "started", Message: "Read the form"}
	got, err := e.NotifyWidgetRun(ctx, run.ID, n, 0, run.Participants[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Receipts) != 1 || len(got.Receipts[0].Deliveries) != 3 || got.Receipts[0].Actor != run.Participants[0].ID {
		t.Fatal(got)
	}
	for _, d := range got.Receipts[0].Deliveries {
		if d.Status != "informed" || d.Active {
			t.Fatal(d)
		}
	}
	e.widgetTick(time.Now())
	e.Wait()
	for _, sid := range []string{"sess_1", "second"} {
		page, err := st.ListTurnsPage(ctx, sid, "", 100)
		if err != nil || len(page.Turns) != 1 || len(page.Turns[0].Messages) != 1 {
			t.Fatal(page, err)
		}
		msg := page.Turns[0].Messages[0]
		if msg.Role != store.RoleUser || !strings.Contains(string(msg.Metadata), "widgetNotification") {
			t.Fatal(msg)
		}
		events, _ := st.EventsAfter(ctx, sid, 0, 100)
		if len(events) != 1 || events[0].Kind != "widget.notice" {
			t.Fatal(events)
		}
	}
	again, err := e.NotifyWidgetRun(ctx, run.ID, n, 42, "")
	if err != nil || again.Receipts[0].StateVersion != 0 {
		t.Fatal("retry mutated receipt", again, err)
	}
	n.Message = "changed"
	if _, err = e.NotifyWidgetRun(ctx, run.ID, n, 0, ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("mutable ID accepted", err)
	}
	n.ID = "private"
	n.Audience = WidgetAudience{Kind: "selected", ParticipantIDs: []string{run.Participants[1].ID}}
	if _, err = e.NotifyWidgetRun(ctx, run.ID, n, 1, ""); err != nil {
		t.Fatal(err)
	}
	first, _ := st.ListTurnsPage(ctx, "sess_1", "", 100)
	second, _ := st.ListTurnsPage(ctx, "second", "", 100)
	if len(first.Turns) != 1 || len(second.Turns) != 2 {
		t.Fatal("directed scope leaked")
	}
	n.ID = "bad"
	n.Audience.ParticipantIDs = []string{"unbound"}
	if _, err = e.NotifyWidgetRun(ctx, run.ID, n, 2, ""); err == nil {
		t.Fatal("unknown participant accepted")
	}
	if _, err = e.WidgetRun(plugin.WithRuntimeID(context.Background(), "other-runtime"), run.ID, true); err == nil {
		t.Fatal("other runtime read run")
	}
	if _, err = e.AuthorizeWidgetRun(ctx, run.ID, "unrelated", ""); err == nil {
		t.Fatal("unrelated session authorized")
	}
	if p, err := e.AuthorizeWidgetRun(ctx, run.ID, "sess_1", ""); err != nil || len(p.Roles) != 2 {
		t.Fatal(p, err)
	}
}
func TestWidgetActionWaitsForBusySessionAndDoesNotBlockInform(t *testing.T) {
	e, st, ctx, run := notificationEngine(t)
	busy, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "sess_1", TurnID: "busy", ClientMessageID: "user", UserMessageID: "user", UserText: "Own work"})
	if err != nil {
		t.Fatal(err)
	}
	n := WidgetNotification{ID: "submit", Audience: WidgetAudience{Kind: "all"}, Delivery: "request-action", Topic: "review", Message: "Submit your review"}
	got, err := e.NotifyWidgetRun(ctx, run.ID, n, 3, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Receipts[0].Deliveries) != 3 {
		t.Fatal(got)
	}
	e.widgetTick(time.Now())
	e.Wait()
	e.widgetTick(time.Now())
	got, _ = e.WidgetRun(ctx, run.ID, false)
	ds := got.Receipts[0].Deliveries
	if ds[0].Status != "queued" || ds[1].Status != "completed" || ds[2].Status != "queued" {
		t.Fatalf("wrong per-participant outcomes: %+v %+v %+v", ds[0], ds[1], ds[2])
	}
	n.ID = "info"
	n.Delivery = "inform"
	n.Message = "Additional context"
	if _, err = e.NotifyWidgetRun(ctx, run.ID, n, 4, ""); err != nil {
		t.Fatal(err)
	}
	page, _ := st.ListTurnsPage(ctx, "sess_1", "", 100)
	if len(page.Turns) != 3 {
		t.Fatal("inform blocked behind action", len(page.Turns))
	}
	// Fulfil only the second participant. The first and human remain independently active.
	keep := []WidgetRequestKey{{NotificationID: "submit", ParticipantID: run.Participants[0].ID}, {NotificationID: "submit", ParticipantID: run.Participants[2].ID}}
	if _, err = e.SetWidgetRequests(ctx, run.ID, keep); err != nil {
		t.Fatal(err)
	}
	if _, err = e.AuthorizeWidgetRun(ctx, run.ID, "second", "submit"); err == nil {
		t.Fatal("fulfilled request still authorizes tools")
	}
	if _, err = e.AuthorizeWidgetRun(ctx, run.ID, "sess_1", "submit"); err != nil {
		t.Fatal(err)
	}
	// Cancel the waiting request before the unrelated turn finishes.
	if _, err = e.SetWidgetRequests(ctx, run.ID, keep[1:]); err != nil {
		t.Fatal(err)
	}
	if _, err = st.FinishTurn(ctx, store.FinishTurnInput{TurnID: busy.Turn.ID, Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	e.Wait()
	got, _ = e.WidgetRun(ctx, run.ID, false)
	if got.Receipts[0].Deliveries[0].TurnID != "" {
		t.Fatal("cancelled request woke model")
	}
	page, _ = st.ListTurnsPage(ctx, "second", "", 100)
	count := 0
	for _, turn := range page.Turns {
		for _, msg := range turn.Messages {
			if msg.Role == store.RoleUser && strings.Contains(msg.Text, "Submit your review") {
				count++
			}
		}
	}
	if count != 1 {
		t.Fatal("wake duplicated canonical input", count)
	}
}
func TestWidgetRunPauseExpiryAndRestartDoNotReplay(t *testing.T) {
	e, st, ctx, run := notificationEngine(t)
	n := WidgetNotification{ID: "review", Audience: WidgetAudience{Kind: "selected", ParticipantIDs: []string{run.Participants[0].ID}}, Delivery: "request-action", Topic: "review", Message: "Review"}
	if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 0, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := e.ChangeWidgetRun(ctx, run.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	if len(e.running) != 0 {
		t.Fatal("paused run woke model")
	}
	if _, err := e.AuthorizeWidgetRun(ctx, run.ID, "sess_1", "review"); err == nil {
		t.Fatal("paused input accepted")
	}
	e.widgetTick(time.Now().Add(time.Minute))
	if _, err := e.WidgetRun(ctx, run.ID, false); err == nil {
		t.Fatal("expired run retained")
	}
	page, _ := st.ListTurnsPage(ctx, "sess_1", "", 10)
	if len(page.Turns) != 1 {
		t.Fatal("canonical notice lost")
	}
	restarted := New(st, e.hub, e.resolver, st)
	defer restarted.Stop()
	if _, err := restarted.WidgetRun(ctx, run.ID, false); err == nil {
		t.Fatal("run restored after restart")
	}
	if _, err := restarted.AuthorizeWidgetRun(ctx, run.ID, "sess_1", "review"); err == nil {
		t.Fatal("old request survived restart")
	}
}

func TestWidgetPauseCancelsOnlyItsTurnAndRetainsTheRequestForResume(t *testing.T) {
	e, _, ctx, run := notificationEngine(t)
	n := WidgetNotification{ID: "review", Audience: WidgetAudience{Kind: "all"}, Delivery: "request-action", Topic: "review", Message: "Review"}
	if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 0, ""); err != nil {
		t.Fatal(err)
	}
	// Simulate one admitted run turn and another user's unrelated turn.
	actionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unrelatedCtx, unrelatedCancel := context.WithCancel(context.Background())
	defer unrelatedCancel()
	e.running["sess_1"] = newActiveTurn("own-action", cancel)
	e.running["second"] = newActiveTurn("unrelated-turn", unrelatedCancel)
	e.widgetRuns.entries[run.ID].Receipts[0].Deliveries[0].Status = "running"
	e.widgetRuns.entries[run.ID].Receipts[0].Deliveries[0].TurnID = "own-action"
	if _, err := e.ChangeWidgetRun(ctx, run.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	if actionCtx.Err() == nil || unrelatedCtx.Err() != nil {
		t.Fatal("pause cancellation scope incorrect")
	}
	got, _ := e.WidgetRun(ctx, run.ID, false)
	d := got.Receipts[0].Deliveries[0]
	if d.Status != "queued" || !d.Active || d.Attempt != 1 {
		t.Fatal("unfinished request lost", d)
	}
	delete(e.running, "sess_1")
	delete(e.running, "second")
	if _, err := e.ChangeWidgetRun(ctx, run.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	e.Wait()
	e.widgetTick(time.Now())
	got, _ = e.WidgetRun(ctx, run.ID, false)
	if got.Receipts[0].Deliveries[0].Status != "completed" {
		t.Fatal("resume failed", got.Receipts[0].Deliveries[0])
	}
}

func TestWidgetSourceRevocationPrecedesSchedulerTick(t *testing.T) {
	e, st, ctx, run := notificationEngine(t)
	item, err := st.GetStudioItem(ctx, run.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	item.BindingVersion++
	if _, err = st.UpdateStudioItem(ctx, item, item.Revision); err != nil {
		t.Fatal(err)
	}
	n := WidgetNotification{ID: "late", Audience: WidgetAudience{Kind: "all"}, Delivery: "inform", Topic: "late", Message: "No longer authorized"}
	if _, err = e.NotifyWidgetRun(ctx, run.ID, n, 0, ""); err == nil {
		t.Fatal("changed bindings still accepted a notice")
	}
	page, _ := st.ListTurnsPage(ctx, "sess_1", "", 100)
	if len(page.Turns) != 0 {
		t.Fatal("revoked notice entered history")
	}
}

type failingWidgetNoticeStore struct {
	store.Store
	remaining int
}

func (s *failingWidgetNoticeStore) RecordWidgetNotice(ctx context.Context, in store.WidgetNoticeInput) (*store.WidgetNoticeResult, error) {
	if in.SessionID == "sess_1" && s.remaining > 0 {
		s.remaining--
		return nil, errors.New("injected notice write failure")
	}
	return s.Store.RecordWidgetNotice(ctx, in)
}
func TestWidgetNoticePartialFailurePreservesRecipientOrderWithoutDuplicates(t *testing.T) {
	e, st, ctx, run := notificationEngine(t)
	e.store = &failingWidgetNoticeStore{Store: e.store, remaining: 2}
	for _, id := range []string{"first", "second"} {
		n := WidgetNotification{ID: id, Audience: WidgetAudience{Kind: "all"}, Delivery: "inform", Topic: "update", Message: id}
		if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 0, ""); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := st.ListTurnsPage(ctx, "sess_1", "", 100)
	second, _ := st.ListTurnsPage(ctx, "second", "", 100)
	if len(first.Turns) != 0 || len(second.Turns) != 2 {
		t.Fatalf("recipient failure leaked or later notice overtook it: %d %d", len(first.Turns), len(second.Turns))
	}
	e.widgetTick(time.Now())
	got, _ := e.WidgetRun(ctx, run.ID, false)
	events, _ := st.EventsAfter(ctx, "sess_1", 0, 100)
	if len(events) != 2 || events[0].UserMessageID != got.Receipts[0].Deliveries[0].MessageID || events[1].UserMessageID != got.Receipts[1].Deliveries[0].MessageID {
		t.Fatal("canonical delivery order changed", events)
	}
	second, _ = st.ListTurnsPage(ctx, "second", "", 100)
	if len(second.Turns) != 2 {
		t.Fatal("retry duplicated successful recipient notice")
	}
}

func TestWidgetResultBroadcastWakesEverySessionOnce(t *testing.T) {
	e, st, ctx, run := notificationEngine(t)
	busy, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "sess_1", TurnID: "finishing-action", ClientMessageID: "move", UserMessageID: "move", UserText: "Finish the current action"})
	if err != nil {
		t.Fatal(err)
	}
	n := WidgetNotification{ID: "result", Audience: WidgetAudience{Kind: "all"}, Delivery: "request-action", Topic: "result", Summary: "Review complete", Message: "Acknowledge the completed review"}
	if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 1, run.Participants[0].ID); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	e.Wait()
	e.widgetTick(time.Now())
	got, _ := e.WidgetRun(ctx, run.ID, false)
	if got.Receipts[0].Deliveries[0].Status != "queued" || got.Receipts[0].Deliveries[1].Status != "completed" {
		t.Fatal(got.Receipts[0].Deliveries)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: busy.Turn.ID, Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	e.Wait()
	e.widgetTick(time.Now())
	// A transport retry cannot start another response or change its summary.
	if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 1, ""); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	e.Wait()
	for _, sid := range []string{"sess_1", "second"} {
		page, err := st.ListTurnsPage(ctx, sid, "", 100)
		if err != nil {
			t.Fatal(err)
		}
		notices, actions := 0, 0
		for _, turn := range page.Turns {
			for _, msg := range turn.Messages {
				if strings.Contains(string(msg.Metadata), "widgetNotification") {
					notices++
					if !strings.Contains(string(msg.Metadata), `"summary":"Review complete"`) || !strings.Contains(msg.Text, "Acknowledge the completed review") {
						t.Fatal(msg)
					}
				}
				if strings.Contains(string(msg.Metadata), "widgetAction") {
					actions++
				}
			}
		}
		if notices != 1 || actions != 1 {
			t.Fatalf("%s: notices=%d actions=%d", sid, notices, actions)
		}
	}
	n.Summary = "changed"
	if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 1, ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("summary changed under same notification ID", err)
	}
	n.ID = "too-long"
	n.Summary = strings.Repeat("x", 1001)
	if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 1, ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("oversized summary accepted", err)
	}
	// Match Zod string limits in UTF-16 units, including localized summaries.
	n.Delivery = "inform"
	for i, summary := range []string{strings.Repeat("好", 1000), strings.Repeat("😀", 500)} {
		n.ID, n.Summary = fmt.Sprintf("localized-%d", i), summary
		if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 1, ""); err != nil {
			t.Fatal("valid localized summary rejected", err)
		}
		n.ID, n.Summary = n.ID+"-long", summary+"a"
		if _, err := e.NotifyWidgetRun(ctx, run.ID, n, 1, ""); !errors.Is(err, ErrWidgetRun) {
			t.Fatal("oversized localized summary accepted", err)
		}
	}
}

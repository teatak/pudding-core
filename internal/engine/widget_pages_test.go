package engine

import (
	"testing"
	"time"
)

func TestWidgetRestorationPausesAndRevokesOldWork(t *testing.T) {
	e, st, ctx, old := notificationEngine(t)
	n := WidgetNotification{ID: "pending", Audience: WidgetAudience{Kind: "all"}, Delivery: "request-action", Message: "Act"}
	if _, err := e.NotifyWidgetRun(ctx, old.ID, n, 0, ""); err != nil {
		t.Fatal(err)
	}
	page, restored, err := e.OpenWidgetPage(ctx, old.ItemID, "library", old.RevisionHash, "fresh-target")
	if err != nil || restored.Status != "paused" || restored.ID == old.ID || len(restored.Receipts) != 0 || page.TargetID != "fresh-target" {
		t.Fatal(page, restored, err)
	}
	if _, err = e.AuthorizeWidgetRun(ctx, old.ID, "sess_1", ""); err == nil {
		t.Fatal("old actor authorized")
	}
	if _, err = e.NotifyWidgetRun(ctx, old.ID, n, 0, ""); err == nil {
		t.Fatal("old notice delivered")
	}
	e.widgetTick(time.Now())
	e.Wait()
	turns, _ := st.ListTurnsPage(ctx, "sess_1", "", 100)
	if len(turns.Turns) != 1 {
		t.Fatal("restoration replayed pending work", turns)
	}
	if _, err = e.AuthorizeWidgetRun(ctx, restored.ID, "sess_1", ""); err == nil {
		t.Fatal("paused actor authorized")
	}
	if _, err = e.ChangeWidgetRun(ctx, restored.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.AuthorizeWidgetRun(ctx, restored.ID, "sess_1", ""); err != nil {
		t.Fatal(err)
	}
	if err = e.CloseWidgetPages(ctx, old.ItemID, "library"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.WidgetRun(ctx, restored.ID, true); err == nil {
		t.Fatal("closed run kept alive")
	}
}

func TestRestoredWidgetRevalidatesParticipantsAndStopKeepsState(t *testing.T) {
	e, st, ctx, old := notificationEngine(t)
	page, err := st.GetWidgetPageByTarget(ctx, old.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.WriteWidgetPage(ctx, page.ID, old.TargetID, 0, []byte(`{"progress":7}`)); err != nil {
		t.Fatal(err)
	}
	// Simulate loss of all process-local executions without changing durable data.
	e.widgetRuns.Lock()
	e.widgetRuns.entries = nil
	e.widgetRuns.Unlock()
	page, restored, err := e.OpenWidgetPage(ctx, old.ItemID, "library", old.RevisionHash, "after-restart")
	if err != nil || page.Version != 1 || restored.Status != "paused" || len(restored.Receipts) != 0 {
		t.Fatal(page, restored, err)
	}
	if _, err = st.ArchiveSession(ctx, "second"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.ChangeWidgetRun(ctx, restored.ID, "resume"); err == nil {
		t.Fatal("archived participant resumed")
	}
	if _, err = e.ChangeWidgetRun(ctx, restored.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	page, restored, err = e.OpenWidgetPage(ctx, old.ItemID, "library", old.RevisionHash, "after-stop")
	if err != nil || restored != nil || page.Version != 1 || string(page.Data) != `{"progress":7}` {
		t.Fatal("stop discarded state", page, restored, err)
	}
}

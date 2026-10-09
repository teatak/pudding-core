package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
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

func TestWidgetUpgradeKeepsOldRunUntilItsPageSwitches(t *testing.T) {
	e, st, ctx, old := notificationEngine(t)
	item, err := st.GetStudioItem(ctx, old.ItemID)
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Repeat("b", 64)
	item, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: item.ID, Hash: next, ClientRequestID: "upgrade", CreatedAt: time.Now()}, item.HeadRevision)
	if err != nil {
		t.Fatal(err)
	}
	if err = st.PutWidgetBuildReceipt(ctx, item.ID, next, []byte(`{"ok":true}`)); err != nil {
		t.Fatal(err)
	}
	item.ActiveRevision = next
	if _, err = st.UpdateStudioItem(ctx, item, item.Revision); err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	if _, err = e.AuthorizeWidgetRun(ctx, old.ID, "sess_1", ""); err != nil {
		t.Fatal("upgrade revoked old actor", err)
	}
	if _, err = e.NotifyWidgetRun(ctx, old.ID, WidgetNotification{ID: "after-upgrade", Audience: WidgetAudience{Kind: "all"}, Delivery: "inform", Message: "Old page still works"}, 0, ""); err != nil {
		t.Fatal(err)
	}
	// Another page uses the new default and has a separate run.
	if hash, err := e.SelectWidgetPage(ctx, item.ID, "second", ""); err != nil || hash != next {
		t.Fatal(hash, err)
	}
	if _, _, err = e.OpenWidgetPage(ctx, item.ID, "second", next, "new-target"); err != nil {
		t.Fatal(err)
	}
	definition := old.WidgetRunCreate
	definition.TargetID = "new-target"
	definition.RevisionHash = next
	newRun, err := e.CreateWidgetRun(ctx, item.ID, definition)
	if err != nil {
		t.Fatal(err)
	}
	// A host reload restores the old source's interaction, paused, after upgrade.
	_, restored, err := e.OpenWidgetPage(ctx, item.ID, "library", old.RevisionHash, "restored")
	if err != nil || restored == nil || restored.Status != "paused" {
		t.Fatal(restored, err)
	}
	if _, err = e.ChangeWidgetRun(ctx, restored.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.AuthorizeWidgetRun(ctx, restored.ID, "sess_1", ""); err != nil {
		t.Fatal(err)
	}
	if _, err = e.SelectWidgetPage(ctx, item.ID, "library", next); err != nil {
		t.Fatal(err)
	}
	if _, err = e.WidgetRun(ctx, restored.ID, true); err == nil {
		t.Fatal("switched page kept old run")
	}
	if _, err = e.AuthorizeWidgetRun(ctx, newRun.ID, "sess_1", ""); err != nil {
		t.Fatal("switch affected another page", err)
	}
}

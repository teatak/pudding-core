package engine

import (
	"context"
	"errors"
	"github.com/teatak/pudding-core/internal/plugin"
	"testing"
	"time"
)

func TestExternalWidgetParticipantsIdentityAndLifecycle(t *testing.T) {
	e, _, ctx, run := notificationEngine(t)
	members := []WidgetParticipant{{}, {ExternalID: "external-a", Name: "Codex A"}, {ExternalID: "external-b", Name: "Codex B"}}
	next, err := e.SetWidgetParticipants(ctx, run.ID, members)
	if err != nil {
		t.Fatal(err)
	}
	a, b := next.Participants[1], next.Participants[2]
	if a.ID == b.ID || a.ID == next.Participants[0].ID {
		t.Fatal("identities collapsed", next)
	}
	again, err := e.SetWidgetParticipants(ctx, run.ID, members)
	if err != nil || again.Participants[1].ID != a.ID {
		t.Fatal("identity changed", again, err)
	}
	for _, invalid := range [][]WidgetParticipant{
		{{ExternalID: "external-a", SessionID: "second", Name: "Mixed"}},
		{{ExternalID: "external-a", Name: "A"}, {ExternalID: "external-a", Name: "B"}},
		{{ExternalID: "external-a"}}, {{ExternalID: "forged", Name: "A"}},
	} {
		if _, err = e.SetWidgetParticipants(ctx, run.ID, invalid); !errors.Is(err, ErrWidgetRun) {
			t.Fatal("accepted invalid participants", invalid, err)
		}
	}
	notice := WidgetNotification{ID: "move", Audience: WidgetAudience{Kind: "selected", ParticipantIDs: []string{a.ID}}, Delivery: "request-action", Topic: "move", Message: "Your turn"}
	next, err = e.NotifyWidgetRun(ctx, run.ID, notice, 4, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	e.widgetTick(time.Now())
	next, _ = e.WidgetRun(ctx, run.ID, false)
	d := next.Receipts[0].Deliveries[0]
	if !d.Active || d.Status != "queued" || d.TurnID != "" || d.MessageID != "" {
		t.Fatal("external notification started an internal model", d)
	}
	if who, err := e.AuthorizeExternalWidgetRun(ctx, run.ID, "external-a", "move"); err != nil || who.ID != a.ID {
		t.Fatal(who, err)
	}
	if who, err := e.AuthorizeExternalWidgetRun(ctx, run.ID, "external-b", "move"); err != nil || who.ID != b.ID {
		t.Fatal("notification audience must not restrict participant tools", who, err)
	}
	for _, id := range []string{"external-outsider", ""} {
		if _, err := e.AuthorizeExternalWidgetRun(ctx, run.ID, id, "move"); !errors.Is(err, ErrWidgetRun) {
			t.Fatal("wrong actor accepted", id, err)
		}
	}
	if _, err := e.AuthorizeWidgetRun(ctx, run.ID, "external-a", ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("external identity became a session")
	}
	other := plugin.WithRuntimeID(context.Background(), "other-desktop")
	if _, err := e.AuthorizeExternalWidgetRun(other, run.ID, "external-a", ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("cross-runtime authorization")
	}
	if _, err = e.ChangeWidgetRun(ctx, run.ID, "pause"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.AuthorizeExternalWidgetRun(ctx, run.ID, "external-a", ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("paused authorization")
	}
	if _, err = e.ChangeWidgetRun(ctx, run.ID, "resume"); err != nil {
		t.Fatal(err)
	}
	if _, err = e.SetWidgetRequests(ctx, run.ID, nil); err != nil {
		t.Fatal(err)
	}
	if who, err := e.AuthorizeExternalWidgetRun(ctx, run.ID, "external-a", "move"); err != nil || who.ID != a.ID {
		t.Fatal("stale notification blocked participant tools", who, err)
	}
	if _, err = e.SetWidgetParticipants(ctx, run.ID, members[:1]); err != nil {
		t.Fatal(err)
	}
	if _, err = e.AuthorizeExternalWidgetRun(ctx, run.ID, "external-a", ""); !errors.Is(err, ErrWidgetRun) {
		t.Fatal("removed participant authorization")
	}
}

package sqlitestore

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestComputerAppGrantRevocationPreservesHistoryAndOtherGrants(t *testing.T) {
	ctx := context.Background()
	st, path := openTestStore(t)
	for _, id := range []string{"session", "other"} {
		createTestSession(t, st, id)
		if err := st.GrantComputerApp(ctx, id, "com.example.Editor"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.GrantComputerApp(ctx, "session", "com.example.Notes"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: "session", TurnID: "turn", ClientMessageID: "input", UserMessageID: "user", UserText: "Keep this history"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("Kept")}); err != nil {
		t.Fatal(err)
	}
	beforeSession, err := st.GetSession(ctx, "session")
	if err != nil {
		t.Fatal(err)
	}
	beforeMessages, err := st.ListMessages(ctx, "session", 0)
	if err != nil || len(beforeMessages) != 2 {
		t.Fatalf("history fixture: %+v %v", beforeMessages, err)
	}
	grants, err := st.ListComputerAppGrants(ctx, "session")
	if err != nil || len(grants) != 2 || grants[0].CreatedAt.IsZero() {
		t.Fatalf("list grants: %+v %v", grants, err)
	}
	if err := st.GrantComputerApp(ctx, "session", "com.example.Editor"); err != nil {
		t.Fatal(err)
	}
	if duplicate, err := st.ListComputerAppGrants(ctx, "session"); err != nil || !reflect.DeepEqual(duplicate, grants) {
		t.Fatalf("reapproval changed grant identity or time: %+v %v", duplicate, err)
	}
	for range 2 {
		if err := st.RevokeComputerAppGrant(ctx, "session", "com.example.Editor"); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if grants, err := reopened.ListComputerAppGrants(ctx, "session"); err != nil || len(grants) != 1 || grants[0].AppID != "com.example.Notes" {
		t.Fatalf("revocation did not persist or removed another app: %+v %v", grants, err)
	}
	if granted, err := reopened.HasComputerAppGrant(ctx, "other", "com.example.Editor"); err != nil || !granted {
		t.Fatalf("another session lost consent: %v %v", granted, err)
	}
	afterSession, err := reopened.GetSession(ctx, "session")
	if err != nil || !reflect.DeepEqual(beforeSession, afterSession) {
		t.Fatalf("session changed: %+v %v", afterSession, err)
	}
	afterMessages, err := reopened.ListMessages(ctx, "session", 0)
	if err != nil || !reflect.DeepEqual(beforeMessages, afterMessages) {
		t.Fatalf("history changed: %+v %v", afterMessages, err)
	}
}

func TestComputerAppGrantManagementRequiresExistingSession(t *testing.T) {
	ctx := context.Background()
	st, _ := openTestStore(t)
	if _, err := st.ListComputerAppGrants(ctx, "missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing session list: %v", err)
	}
	if err := st.RevokeComputerAppGrant(ctx, "missing", "com.example.App"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing session revoke: %v", err)
	}
	createTestSession(t, st, "empty")
	if grants, err := st.ListComputerAppGrants(ctx, "empty"); err != nil || grants == nil || len(grants) != 0 {
		t.Fatalf("empty list must serialize as []: %+v %v", grants, err)
	}
	if err := st.RevokeComputerAppGrant(ctx, "empty", " "); !errors.Is(err, store.ErrInvalidComputerAppGrant) {
		t.Fatalf("empty appID revoke: %v", err)
	}
}

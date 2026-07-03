//go:build sqlite_fts5

package sqlitestore

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestSearchMessagesUsesFTS5AndSessionScope(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_a")
	createTestSession(t, st, "sess_b")

	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_a",
		TurnID:          "turn_a",
		UserMessageID:   "msg_a_user",
		ClientMessageID: "client_a",
		UserText:        "我们讨论过 Device Analytics Dashboard 的导入。",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "turn_a",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("Dashboard 需要先解析 HTML, 再提取指标。"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_b",
		TurnID:          "turn_b",
		UserMessageID:   "msg_b_user",
		ClientMessageID: "client_b",
		UserText:        "Dashboard in another session should not leak.",
	}); err != nil {
		t.Fatal(err)
	}

	hits, err := st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID: "sess_a",
		Query:     "Dashboard",
		Limit:     10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) == 0 {
		t.Fatal("expected hits")
	}
	for _, hit := range hits {
		if hit.SessionID != "sess_a" {
			t.Fatalf("search leaked another session: %+v", hit)
		}
	}
}

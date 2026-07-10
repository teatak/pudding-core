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

	literalHits, err := st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID: "sess_a",
		Query:     "Device Analytics Dashboard",
		Limit:     10,
		Literal:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(literalHits) == 0 {
		t.Fatal("expected literal phrase hit")
	}

	literalHits, err = st.SearchMessages(ctx, store.MessageSearchInput{
		SessionID: "sess_a",
		Query:     `Device "Analytics" Dashboard`,
		Limit:     10,
		Literal:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(literalHits) == 0 {
		t.Fatal("punctuation in a natural-language query should not suppress matches")
	}
}

func TestLiteralSearchUsesSegmentedTermsAndMixedScript(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_segmented_search")

	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_segmented_search",
		TurnID:          "turn_segmented_exact",
		UserMessageID:   "msg_segmented_exact",
		ClientMessageID: "client_segmented_exact",
		UserText:        "DeepSeek模型的GPT4配置已经完成，法国队将在今晚参加一场重要比赛。",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "turn_segmented_exact",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("配置完成。"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_segmented_search",
		TurnID:          "turn_segmented_partial",
		UserMessageID:   "msg_segmented_partial",
		ClientMessageID: "client_segmented_partial",
		UserText:        "比赛快开始了。",
	}); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"DeepSeek模型GPT4", "配置DeepSeek", "法国比赛"} {
		hits, err := st.SearchMessages(ctx, store.MessageSearchInput{
			SessionID: "sess_segmented_search",
			Query:     query,
			Limit:     10,
			Literal:   true,
		})
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if len(hits) == 0 || hits[0].ID != "msg_segmented_exact" {
			t.Fatalf("search %q returned %+v, want exact segmented hit first", query, hits)
		}
	}
}

func TestDeleteSessionRepairsMessagesFTS5(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_fts_repair")
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       "sess_fts_repair",
		TurnID:          "turn_fts_repair",
		UserMessageID:   "msg_fts_repair_user",
		ClientMessageID: "client_fts_repair",
		UserText:        "message that should be indexed",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{
		TurnID:         "turn_fts_repair",
		Status:         store.TurnCompleted,
		AssistantParts: store.TextPart("assistant indexed text"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`DROP TABLE messages_fts`); err != nil {
		t.Fatal(err)
	}

	if err := st.DeleteSession(ctx, "sess_fts_repair"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE id='sess_fts_repair'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("session should be deleted after fts repair, count=%d", count)
	}
}

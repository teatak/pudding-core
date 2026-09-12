package engine

import (
	"context"
	"testing"

	"github.com/teatak/pudding-core/internal/provider/mock"
)

// 客户端压缩必须把 clientMessageID 落到压缩 turn 上:前端的 pending 行与 canonical
// turn 靠它复用同一个列表项 key(硬约束 13)。没有这个键时压缩行只能卸载+挂载,
// 贴底偏移会在压缩结束时跳一次。
func TestCompactKeepsClientMessageIDForOverlayReconciliation(t *testing.T) {
	ctx := context.Background()
	eng, ms, _, sessionID := newTestEngine(t, mock.WithScript([]string{"summary text"}))
	for _, text := range []string{"first", "second", "third"} {
		if _, err := eng.Submit(ctx, SubmitInput{
			SessionID:       sessionID,
			ClientMessageID: "client_" + text,
			Text:            text,
		}); err != nil {
			t.Fatalf("submit %s: %v", text, err)
		}
		waitTurnDone(t, ms, sessionID)
	}

	const clientMessageID = "compact:client_row"
	res, err := eng.Compact(ctx, CompactInput{SessionID: sessionID, ClientMessageID: clientMessageID})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if res.TurnID == "" {
		t.Fatal("compact returned empty turn id")
	}

	page, err := ms.ListTurnsPage(ctx, sessionID, "", 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, turn := range page.Turns {
		if turn.ID != res.TurnID {
			continue
		}
		if turn.ClientMessageID != clientMessageID {
			t.Fatalf("compact turn clientMessageID = %q, want %q", turn.ClientMessageID, clientMessageID)
		}
		return
	}
	t.Fatalf("compact turn %s not found in %d turns", res.TurnID, len(page.Turns))
}

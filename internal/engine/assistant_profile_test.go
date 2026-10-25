package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/storetest"
)

func TestAssistantProfileReachesProviderAndRefreshesOnNextTurn(t *testing.T) {
	ctx := context.Background()
	db := storetest.New(t)
	capture := &captureClient{reqCh: make(chan provider.Request, 4)}
	eng := New(db, event.NewHub(), mapResolver{"capture": capture}, db)
	defer eng.Stop()
	if err := db.PutProviderProfile(ctx, &store.ProviderProfile{ID: "capture", Protocol: "openai-compatible", Models: []store.ProviderModel{{ID: "first"}, {ID: "second"}}}); err != nil {
		t.Fatal(err)
	}
	opened, err := db.OpenAssistant(ctx, "Puddy", "capture", "first", store.AssistantAppearance{"version": 1, "palette": "original", "body": "classic", "eyes": "classic", "mouth": "classic", "headwear": "antenna", "arms": "classic", "eyewear": "classic", "faceSticker": "blush"})
	if err != nil {
		t.Fatal(err)
	}
	sid := opened.Assistant.SessionID
	request := func(id string) provider.Request {
		t.Helper()
		if _, err := eng.Submit(ctx, SubmitInput{SessionID: sid, ClientMessageID: id, Text: "你叫什么？"}); err != nil {
			t.Fatal(err)
		}
		waitTurnDone(t, db, sid)
		if turn, ok := turnByClientID(t, db, sid, id); !ok || turn.Status != store.TurnCompleted {
			t.Fatalf("turn failed: %+v", turn)
		}
		select {
		case req := <-capture.reqCh:
			return req
		default:
			t.Fatal("provider did not receive the request")
			return provider.Request{}
		}
	}
	first := request("first-name")
	if !strings.Contains(first.System, "Chinese name is 小布") || !strings.Contains(first.System, "English name is Puddy") {
		t.Fatal("provider did not receive the default assistant identity")
	}
	input := store.SaveAssistantInput{ExpectedRevision: &opened.Assistant.Revision, Name: "小星", UserName: "杨同学", Appearance: opened.Assistant.Appearance, CommunicationPreference: "请用中文简短回答"}
	if _, err := db.SaveAssistant(ctx, input); err != nil {
		t.Fatal(err)
	}
	model := "second"
	if _, err := db.UpdateSession(ctx, sid, store.SessionUpdate{Model: &model}); err != nil {
		t.Fatal(err)
	}
	second := request("updated-name")
	if second.Model != "second" || !strings.Contains(second.System, `"name":"小星"`) || !strings.Contains(second.System, "请用中文简短回答") {
		t.Fatal("updated profile did not reach the newly selected model")
	}
	if strings.Contains(second.System, "Your Chinese name is 小布") {
		t.Fatal("custom name retained the default identity")
	}
}

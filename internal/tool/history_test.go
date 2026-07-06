package tool

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

type fakeHistorySource struct {
	input        store.MessageSearchInput
	getSessionID string
	getMessageID string
	hits         []*store.Message
	message      *store.Message
	err          error
}

func (f *fakeHistorySource) SearchMessages(_ context.Context, in store.MessageSearchInput) ([]*store.Message, error) {
	f.input = in
	return f.hits, f.err
}

func (f *fakeHistorySource) GetMessage(_ context.Context, sessionID string, messageID string) (*store.Message, error) {
	f.getSessionID = sessionID
	f.getMessageID = messageID
	return f.message, f.err
}

func TestHistorySearchDefaultsToCurrentSession(t *testing.T) {
	src := &fakeHistorySource{hits: []*store.Message{{
		ID:        "msg_1",
		SessionID: "sess_1",
		Role:      store.RoleUser,
		Kind:      store.MessageKindText,
		Text:      "previous dashboard discussion",
		CreatedAt: time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
	}}}
	res := NewBuiltinRunner(WithHistorySearch(src)).Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      HistorySearch,
		Args:      json.RawMessage(`{"query":"dashboard"}`),
	})
	if !res.Ok {
		t.Fatalf("search should succeed: %+v", res)
	}
	if src.input.SessionID != "sess_1" || src.input.Query != "dashboard" || src.input.Limit != historySearchDefaultLimit {
		t.Fatalf("unexpected search input: %+v", src.input)
	}
	payload := decodeToolResult(t, res)
	if payload["hit_count"] != float64(1) {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHistoryGetMessageReturnsPartsAndLocalFolders(t *testing.T) {
	src := &fakeHistorySource{message: &store.Message{
		ID:        "msg_1",
		SessionID: "sess_1",
		TurnID:    "turn_1",
		Role:      store.RoleUser,
		Kind:      store.MessageKindText,
		Text:      "look at this",
		Parts: store.UserInputParts("look at this", []store.ContentPart{
			store.AttachmentPart(store.Attachment{
				ID:            "att_1",
				Name:          "demo.txt",
				AttachmentKey: "sessions/sess_1/blobs/demo.txt",
				MIME:          "text/plain",
				Size:          12,
				SourcePath:    "/tmp/demo.txt",
			}),
			store.LocalFolderPart(store.LocalFolder{
				ID:     "folder_1",
				Name:   "files",
				Path:   "/tmp/files",
				Origin: "local_path",
			}),
			{Type: store.ContentPartText, Text: "look at this"},
		}),
		CreatedAt: time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC),
	}}
	res := NewBuiltinRunner(WithHistorySearch(src)).Call(context.Background(), Call{
		SessionID: "sess_1",
		Name:      HistoryGetMessage,
		Args:      json.RawMessage(`{"message_id":"msg_1"}`),
	})
	if !res.Ok {
		t.Fatalf("get message should succeed: %+v", res)
	}
	if src.getSessionID != "sess_1" || src.getMessageID != "msg_1" {
		t.Fatalf("unexpected get input: %s %s", src.getSessionID, src.getMessageID)
	}
	payload := decodeToolResult(t, res)
	if payload["message_id"] != "msg_1" || payload["text"] != "look at this" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if _, ok := payload["parts"].([]any); !ok {
		t.Fatalf("expected parts: %+v", payload)
	}
	if _, ok := payload["attachments"].([]any); !ok {
		t.Fatalf("expected attachments: %+v", payload)
	}
	if _, ok := payload["local_folders"].([]any); !ok {
		t.Fatalf("expected local_folders: %+v", payload)
	}
}

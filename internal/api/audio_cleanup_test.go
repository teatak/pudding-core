package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

func TestClearASRRecordingsIsSessionScoped(t *testing.T) {
	srv, st := newTestServer(t)
	ctx := context.Background()
	for _, sessionID := range []string{"sess_target", "sess_other"} {
		if err := st.CreateSession(ctx, &store.Session{ID: sessionID, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}

	targetASR := cleanupTestAttachment("sess_target", "target_asr", attachment.OriginASRAudio)
	targetUpload := cleanupTestAttachment("sess_target", "target_upload", attachment.OriginUpload)
	targetVoice := cleanupTestAttachment("sess_target", "target_voice", attachment.OriginVoiceAudio)
	otherASR := cleanupTestAttachment("sess_other", "other_asr", attachment.OriginASRAudio)
	otherVoice := cleanupTestAttachment("sess_other", "other_voice", attachment.OriginVoiceAudio)

	beginCleanupTestTurn(t, st, "sess_target", "target", []store.ContentPart{
		store.AttachmentPart(targetASR),
		store.AttachmentPart(targetUpload),
	})
	beginCleanupTestTurn(t, st, "sess_other", "other", []store.ContentPart{
		store.AttachmentPart(otherASR),
		store.AttachmentPart(otherVoice),
	})
	queueCleanupTestInput(t, st, "sess_target", "target", targetVoice)
	queueCleanupTestInput(t, st, "sess_other", "other", otherVoice)

	resp := req(t, http.MethodDelete, srv.URL+"/sessions/sess_target/audio/asr-recordings", nil)
	payload := decodeJSON[clearASRRecordingsResponse](t, resp)
	if !payload.OK || payload.Attachments != 2 || payload.Messages != 1 || payload.QueuedInputs != 1 || payload.DeleteErrors != 0 {
		t.Fatalf("unexpected cleanup response: %+v", payload)
	}

	targetMessages, err := st.ListMessages(ctx, "sess_target", 0)
	if err != nil {
		t.Fatal(err)
	}
	targetMessageAttachments := store.AttachmentsFromParts(targetMessages[0].Parts)
	if len(targetMessageAttachments) != 1 || targetMessageAttachments[0].Origin != attachment.OriginUpload {
		t.Fatalf("target message attachments = %+v, want only upload", targetMessageAttachments)
	}
	targetQueued, err := st.ListQueuedInputs(ctx, "sess_target")
	if err != nil {
		t.Fatal(err)
	}
	if len(targetQueued) != 1 || len(store.AttachmentsFromParts(targetQueued[0].Parts)) != 0 {
		t.Fatalf("target queued input still contains recording: %+v", targetQueued)
	}

	otherMessages, err := st.ListMessages(ctx, "sess_other", 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.AttachmentsFromParts(otherMessages[0].Parts)); got != 2 {
		t.Fatalf("other session message attachments = %d, want 2", got)
	}
	otherQueued, err := st.ListQueuedInputs(ctx, "sess_other")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(store.AttachmentsFromParts(otherQueued[0].Parts)); got != 1 {
		t.Fatalf("other session queued attachments = %d, want 1", got)
	}

	legacy := req(t, http.MethodDelete, srv.URL+"/settings/audio/asr-recordings", nil)
	legacy.Body.Close()
	if legacy.StatusCode != http.StatusNotFound {
		t.Fatalf("legacy global cleanup route status = %d, want 404", legacy.StatusCode)
	}
}

func cleanupTestAttachment(sessionID, id, origin string) store.Attachment {
	return store.Attachment{
		ID:            id,
		Name:          id + ".wav",
		AttachmentKey: "sessions/" + sessionID + "/blobs/" + id + ".wav",
		MIME:          "audio/wav",
		Size:          1,
		Origin:        origin,
	}
}

func beginCleanupTestTurn(t *testing.T, st store.Store, sessionID, suffix string, parts []store.ContentPart) {
	t.Helper()
	if _, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID:       sessionID,
		TurnID:          "turn_" + suffix,
		UserMessageID:   "message_" + suffix,
		ClientMessageID: "client_" + suffix,
		UserText:        "voice input",
		UserParts:       parts,
		Provider:        "mock",
		Model:           "mock",
	}); err != nil {
		t.Fatal(err)
	}
}

func queueCleanupTestInput(t *testing.T, st store.Store, sessionID, suffix string, item store.Attachment) {
	t.Helper()
	if _, err := st.QueueInput(context.Background(), store.QueueInputInput{
		SessionID:       sessionID,
		ClientMessageID: "queued_" + suffix,
		Text:            "queued voice input",
		Parts:           []store.ContentPart{store.AttachmentPart(item)},
		Provider:        "mock",
		Model:           "mock",
	}); err != nil {
		t.Fatal(err)
	}
}

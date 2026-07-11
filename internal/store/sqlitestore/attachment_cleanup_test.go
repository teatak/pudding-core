package sqlitestore

import (
	"context"
	"errors"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

func TestRemoveAttachmentsByOriginIsSessionScoped(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	createTestSession(t, st, "sess_target")
	createTestSession(t, st, "sess_other")

	targetASR := sqliteCleanupAttachment("sess_target", "target_asr", attachment.OriginASRAudio)
	targetUpload := sqliteCleanupAttachment("sess_target", "target_upload", attachment.OriginUpload)
	targetVoice := sqliteCleanupAttachment("sess_target", "target_voice", attachment.OriginVoiceAudio)
	otherASR := sqliteCleanupAttachment("sess_other", "other_asr", attachment.OriginASRAudio)
	otherVoice := sqliteCleanupAttachment("sess_other", "other_voice", attachment.OriginVoiceAudio)

	sqliteBeginCleanupTurn(t, st, "sess_target", "target", []store.ContentPart{
		store.AttachmentPart(targetASR),
		store.AttachmentPart(targetUpload),
	})
	sqliteBeginCleanupTurn(t, st, "sess_other", "other", []store.ContentPart{
		store.AttachmentPart(otherASR),
		store.AttachmentPart(otherVoice),
	})
	sqliteQueueCleanupInput(t, st, "sess_target", "target", targetVoice)
	sqliteQueueCleanupInput(t, st, "sess_other", "other", otherVoice)

	asrResult, err := st.RemoveAttachmentsByOrigin(ctx, "sess_target", attachment.OriginASRAudio)
	if err != nil {
		t.Fatal(err)
	}
	if len(asrResult.Attachments) != 1 || asrResult.MessageCount != 1 || asrResult.QueuedInputCount != 0 || asrResult.Attachments[0].SessionID != "sess_target" {
		t.Fatalf("unexpected ASR cleanup result: %+v", asrResult)
	}
	voiceResult, err := st.RemoveAttachmentsByOrigin(ctx, "sess_target", attachment.OriginVoiceAudio)
	if err != nil {
		t.Fatal(err)
	}
	if len(voiceResult.Attachments) != 1 || voiceResult.MessageCount != 0 || voiceResult.QueuedInputCount != 1 || voiceResult.Attachments[0].SessionID != "sess_target" {
		t.Fatalf("unexpected voice cleanup result: %+v", voiceResult)
	}

	targetMessages, err := st.ListMessages(ctx, "sess_target", 0)
	if err != nil {
		t.Fatal(err)
	}
	targetAttachments := store.AttachmentsFromParts(targetMessages[0].Parts)
	if len(targetAttachments) != 1 || targetAttachments[0].Origin != attachment.OriginUpload {
		t.Fatalf("target message attachments = %+v, want only upload", targetAttachments)
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

	if _, err := st.RemoveAttachmentsByOrigin(ctx, "missing", attachment.OriginASRAudio); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing session error = %v, want ErrNotFound", err)
	}
}

func sqliteCleanupAttachment(sessionID, id, origin string) store.Attachment {
	return store.Attachment{
		ID:            id,
		Name:          id + ".wav",
		AttachmentKey: "sessions/" + sessionID + "/blobs/" + id + ".wav",
		MIME:          "audio/wav",
		Size:          1,
		Origin:        origin,
	}
}

func sqliteBeginCleanupTurn(t *testing.T, st store.Store, sessionID, suffix string, parts []store.ContentPart) {
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

func sqliteQueueCleanupInput(t *testing.T, st store.Store, sessionID, suffix string, item store.Attachment) {
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

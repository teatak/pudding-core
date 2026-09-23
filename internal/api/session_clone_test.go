package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/store"
)

func TestCloneSessionWithTempAttachmentKeepsIndependentTempFiles(t *testing.T) {
	srv, st, homeDir := newTestServerWithHome(t)
	source := decodeJSON[store.Session](t, req(t, http.MethodPost, srv.URL+"/sessions", map[string]string{
		"title": "Pasted text", "provider": "mock", "model": "mock",
	}))
	text := strings.Repeat("Long pasted text for an independent branch.\n", 150)
	uploaded := uploadCloneTestTempAttachment(t, srv.URL, text)
	service := attachment.NewService(homeDir)
	regular, err := service.StoreReader(source.ID, "note.txt", "text/plain", strings.NewReader("ordinary attachment"))
	if err != nil {
		t.Fatal(err)
	}
	response := req(t, http.MethodPost, srv.URL+"/sessions/"+source.ID+"/submit", map[string]any{
		"clientMessageID": "clone-temp-input",
		"parts":           []store.ContentPart{store.AttachmentPart(uploaded), store.AttachmentPart(regular)},
	})
	if response.StatusCode != http.StatusAccepted {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("submit status=%d body=%s", response.StatusCode, body)
	}
	submitted := decodeJSON[engine.SubmitResult](t, response)
	waitCloneTestTurn(t, st, source.ID, submitted.TurnID)
	// Tool references live in a different canonical message from the attachment.
	toolPath := "attachments/" + filepath.Base(uploaded.AttachmentKey)
	toolArgs, err := json.Marshal(map[string]string{"scope": "temp", "path": toolPath})
	if err != nil {
		t.Fatal(err)
	}
	historyText := "Original file path: " + toolPath
	if _, err := st.BeginTurn(context.Background(), store.BeginTurnInput{
		SessionID: source.ID, TurnID: "read_temp_turn", UserMessageID: "read_temp_user", ClientMessageID: "read_temp_input", UserText: "Read the attachment",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(context.Background(), store.FinishTurnInput{
		TurnID: "read_temp_turn", Status: store.TurnCompleted,
		AssistantParts: []store.ContentPart{
			{Type: store.ContentPartToolUse, CallID: "read_temp", Name: "builtin_file_read", Args: toolArgs},
			{Type: store.ContentPartToolResult, CallID: "read_temp", Name: "builtin_file_read", Ok: true, Content: strings.Join([]string{uploaded.AttachmentKey, uploaded.URL, toolPath}, "\n")},
			{Type: store.ContentPartText, Text: historyText},
		},
	}); err != nil {
		t.Fatal(err)
	}
	turn := waitCloneTestTurn(t, st, source.ID, "read_temp_turn")
	boundary := turn.Messages[len(turn.Messages)-1]
	if boundary.Role != store.RoleAssistant {
		t.Fatalf("clone boundary must be an assistant message: %+v", boundary)
	}
	cloned := cloneAttachmentTestSession(t, srv.URL, source.ID, boundary.ID)
	clonedMessages, err := st.ListMessages(context.Background(), cloned.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	items := store.AttachmentsFromParts(clonedMessages[0].Parts)
	if len(items) != 2 {
		t.Fatalf("cloned attachments=%+v", items)
	}
	tempCopy, regularCopy := items[0], items[1]
	if tempCopy.AttachmentKey == uploaded.AttachmentKey || !strings.HasPrefix(tempCopy.AttachmentKey, "sessions/draft/blobs/") {
		t.Fatalf("temp attachment needs an independent draft key: source=%+v clone=%+v", uploaded, tempCopy)
	}
	metadata := tempCopy
	metadata.AttachmentKey, metadata.URL = uploaded.AttachmentKey, uploaded.URL
	if !reflect.DeepEqual(metadata, uploaded) {
		t.Fatalf("temp metadata changed: source=%+v clone=%+v", uploaded, tempCopy)
	}
	if regularCopy.Origin != attachment.OriginUpload || !strings.HasPrefix(regularCopy.AttachmentKey, "sessions/"+cloned.ID+"/blobs/") {
		t.Fatalf("ordinary attachment must stay session-owned: %+v", regularCopy)
	}
	checkCloneTestToolReferences(t, clonedMessages, tempCopy, historyText)
	readCloneTestAttachment(t, srv.URL, uploaded, text)
	readCloneTestAttachment(t, srv.URL, tempCopy, text)
	readCloneTestAttachment(t, srv.URL, regularCopy, "ordinary attachment")

	// A branch must survive subsequent writes and deletion of the source temp file.
	sourcePath, ok, err := service.Path(attachment.DraftSessionID, uploaded.AttachmentKey)
	if err != nil || !ok {
		t.Fatalf("source temp path: ok=%v err=%v", ok, err)
	}
	if err := os.WriteFile(sourcePath, []byte("changed after branching"), 0o600); err != nil {
		t.Fatal(err)
	}
	readCloneTestAttachment(t, srv.URL, tempCopy, text)
	if err := service.Delete(attachment.DraftSessionID, uploaded.AttachmentKey); err != nil {
		t.Fatal(err)
	}
	readCloneTestAttachment(t, srv.URL, tempCopy, text)

	second := cloneAttachmentTestSession(t, srv.URL, cloned.ID, clonedMessages[len(clonedMessages)-1].ID)
	secondMessages, err := st.ListMessages(context.Background(), second.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	secondItems := store.AttachmentsFromParts(secondMessages[0].Parts)
	if len(secondItems) != 2 || secondItems[0].AttachmentKey == tempCopy.AttachmentKey || secondItems[0].Origin != attachment.OriginTemp {
		t.Fatalf("repeated clone must retain an independent temp attachment: %+v", secondItems)
	}
	checkCloneTestToolReferences(t, secondMessages, secondItems[0], historyText)
	readCloneTestAttachment(t, srv.URL, secondItems[0], text)
	response = req(t, http.MethodPost, srv.URL+"/sessions/"+second.ID+"/submit", map[string]any{
		"clientMessageID": "resubmit-cloned-attachments",
		"parts":           []store.ContentPart{store.AttachmentPart(secondItems[0]), store.AttachmentPart(secondItems[1])},
	})
	if response.StatusCode != http.StatusAccepted {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("resubmit cloned attachments status=%d body=%s", response.StatusCode, body)
	}
	resubmitted := decodeJSON[engine.SubmitResult](t, response)
	waitCloneTestTurn(t, st, second.ID, resubmitted.TurnID)
}

func TestCloneSessionRejectsForeignAttachmentsAndCleansCopies(t *testing.T) {
	for _, origin := range []string{attachment.OriginUpload, attachment.OriginTemp} {
		t.Run(origin, func(t *testing.T) {
			srv, st, homeDir := newTestServerWithHome(t)
			ctx := context.Background()
			for _, id := range []string{"clone_source", "foreign_source"} {
				if err := st.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
					t.Fatal(err)
				}
			}
			service := attachment.NewService(homeDir)
			regular, err := service.StoreReader("clone_source", "regular.txt", "text/plain", strings.NewReader("keep regular source"))
			if err != nil {
				t.Fatal(err)
			}
			temp := uploadCloneTestTempAttachment(t, srv.URL, "keep temp source")
			foreign, err := service.StoreReader("foreign_source", "foreign.txt", "text/plain", strings.NewReader("foreign session"))
			if err != nil {
				t.Fatal(err)
			}
			foreign.Origin = origin
			if _, err := st.BeginTurn(ctx, store.BeginTurnInput{
				SessionID: "clone_source", TurnID: "clone_turn", UserMessageID: "clone_message", ClientMessageID: "clone_client",
				UserParts: []store.ContentPart{store.AttachmentPart(regular), store.AttachmentPart(temp), store.AttachmentPart(foreign)},
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "clone_turn", Status: store.TurnCompleted, AssistantParts: store.TextPart("answer")}); err != nil {
				t.Fatal(err)
			}
			messages, err := st.ListMessages(ctx, "clone_source", 0)
			if err != nil {
				t.Fatal(err)
			}
			before := cloneTestAttachmentFiles(t, homeDir)
			response := req(t, http.MethodPost, srv.URL+"/sessions/clone_source/clone", cloneSessionReq{
				ThroughMessageID: messages[len(messages)-1].ID, TitleSuffix: " branch",
			})
			response.Body.Close()
			if response.StatusCode < http.StatusBadRequest {
				t.Fatalf("foreign %s attachment accepted: status=%d", origin, response.StatusCode)
			}
			if after := cloneTestAttachmentFiles(t, homeDir); !reflect.DeepEqual(after, before) {
				t.Fatalf("failed clone must remove only its own copies: before=%v after=%v", before, after)
			}
			sessions, err := st.ListSessions(ctx)
			if err != nil || len(sessions) != 2 {
				t.Fatalf("failed clone created a session: sessions=%+v err=%v", sessions, err)
			}
		})
	}
}

func checkCloneTestToolReferences(t *testing.T, messages []*store.Message, item store.Attachment, historyText string) {
	t.Helper()
	var toolUse, toolResult *store.ContentPart
	textPreserved := false
	for _, message := range messages {
		for i := range message.Parts {
			part := &message.Parts[i]
			switch {
			case part.Type == store.ContentPartToolUse && part.CallID == "read_temp":
				if len(store.AttachmentsFromParts(message.Parts)) != 0 {
					t.Fatal("tool-use fixture must be in a message without attachments")
				}
				toolUse = part
			case part.Type == store.ContentPartToolResult && part.CallID == "read_temp":
				toolResult = part
			case part.Type == store.ContentPartText && part.Text == historyText:
				textPreserved = true
			}
		}
	}
	if toolUse == nil || toolResult == nil {
		t.Fatal("cloned history is missing the temp file tool call or result")
	}
	var args map[string]string
	if err := json.Unmarshal(toolUse.Args, &args); err != nil {
		t.Fatal(err)
	}
	toolPath := "attachments/" + filepath.Base(item.AttachmentKey)
	if args["scope"] != "temp" || args["path"] != toolPath || toolResult.Content != strings.Join([]string{item.AttachmentKey, item.URL, toolPath}, "\n") {
		t.Fatalf("cloned tool history still points to its source: call=%+v result=%+v", toolUse, toolResult)
	}
	if !textPreserved {
		t.Fatal("ordinary assistant text was rewritten")
	}
}

func uploadCloneTestTempAttachment(t *testing.T, serverURL, text string) store.Attachment {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("origin", attachment.OriginTemp); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file", "pasted-text.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, text); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, serverURL+"/sessions/draft/attachments", &body)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+testToken)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		data, _ := io.ReadAll(response.Body)
		t.Fatalf("temp upload status=%d body=%s", response.StatusCode, data)
	}
	return decodeJSON[store.Attachment](t, response)
}

func cloneAttachmentTestSession(t *testing.T, serverURL, sessionID, messageID string) store.Session {
	t.Helper()
	response := req(t, http.MethodPost, serverURL+"/sessions/"+sessionID+"/clone", cloneSessionReq{
		ThroughMessageID: messageID, TitleSuffix: " branch",
	})
	if response.StatusCode != http.StatusCreated {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("clone status=%d body=%s", response.StatusCode, body)
	}
	return decodeJSON[store.Session](t, response)
}

func readCloneTestAttachment(t *testing.T, serverURL string, item store.Attachment, want string) {
	t.Helper()
	response := req(t, http.MethodGet, serverURL+item.URL, nil)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || string(body) != want {
		t.Fatalf("read attachment %q: status=%d body=%q err=%v", item.AttachmentKey, response.StatusCode, body, err)
	}
}

func waitCloneTestTurn(t *testing.T, st store.Store, sessionID, turnID string) *store.ConversationTurn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		turn, err := st.GetConversationTurn(context.Background(), sessionID, turnID)
		if err != nil {
			t.Fatal(err)
		}
		if turn.Status == store.TurnCompleted {
			return turn
		}
		if turn.Status != store.TurnRunning {
			t.Fatalf("turn failed: %+v", turn)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for the mock assistant")
	return nil
}

func cloneTestAttachmentFiles(t *testing.T, homeDir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	for _, root := range []string{filepath.Join(homeDir, "attachments"), filepath.Join(homeDir, "temp", "attachments")} {
		if err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := os.ReadFile(path)
			files[path] = string(body)
			return err
		}); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

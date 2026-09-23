package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/memstore"
)

func TestAttachmentExportDraftUsesCurrentCanonicalReference(t *testing.T) {
	for _, scope := range []string{"temp", "project"} {
		t.Run(scope, func(t *testing.T) {
			ctx := context.Background()
			homeDir, project := t.TempDir(), t.TempDir()
			data := []byte("long pasted text available in a later turn")
			stored, err := attachment.NewService(homeDir).StoreReader(attachment.DraftSessionID, "pasted.txt", "text/plain", bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			stored.Origin = attachment.OriginTemp
			st := memstore.New()
			persistExportDraftReference(t, st, "session-a", []store.ContentPart{store.AttachmentPart(stored)})
			runner := NewBuiltinRunner(WithHomeDir(homeDir), WithHistorySearch(st))
			t.Cleanup(func() { _ = runner.Close() })
			args := map[string]any{"scope": scope, "attachmentKey": stored.AttachmentKey}
			if scope == "project" {
				args["path"] = "pasted.txt"
			}
			raw, _ := json.Marshal(args)
			call := Call{SessionID: "session-a", TurnID: "later-turn", Name: AttachmentExport, Mode: store.ModeCode, ProjectDirs: []string{project}, Args: raw}
			result := runner.Call(ctx, call)
			if !result.Ok {
				t.Fatalf("current canonical temp attachment could not be exported: %+v", result)
			}
			payload := decodeToolResult(t, result)
			if payload["scope"] != scope || payload["attachmentKey"] != stored.AttachmentKey || payload["bytes"] != float64(len(data)) {
				t.Fatalf("wrong export metadata: %+v", payload)
			}
			absolute := payload["absolutePath"].(string)
			if copied, err := os.ReadFile(absolute); err != nil || !bytes.Equal(copied, data) {
				t.Fatalf("export bytes changed: %q %v", copied, err)
			}
			readArgs, _ := json.Marshal(map[string]any{"scope": scope, "path": payload["path"]})
			read := runner.Call(ctx, Call{SessionID: "session-a", TurnID: "another-turn", Name: FileRead, Mode: store.ModeCode, ProjectDirs: []string{project}, Args: readArgs})
			if !read.Ok || decodeToolResult(t, read)["content"] != string(data) {
				t.Fatalf("export not reusable across turns: %+v", read)
			}
			var managed []string
			if scope == "temp" {
				artifactDir, exists, err := home.ExistingSessionArtifacts(homeDir, "session-a")
				if err != nil || !exists {
					t.Fatalf("artifact root unavailable: %v %v", exists, err)
				}
				managed = []string{artifactDir}
			}
			commandArgs, _ := json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{"cat", absolute})})
			risk, classified := ClassifyToolCallForProject(CommandRun, commandArgs, []string{project}, managed...)
			if !classified || !risk.LowRisk || len(risk.requiredProjectPaths) != 0 {
				t.Fatalf("export cannot enter the command analysis path: %+v", risk)
			}
			// Removing the canonical reference revokes draft export authority even
			// when the blob and previously exported analysis copy still exist.
			if _, err := st.RemoveAttachmentsByOrigin(ctx, "session-a", attachment.OriginTemp); err != nil {
				t.Fatal(err)
			}
			second := runner.Call(ctx, call)
			if second.Ok || decodeToolResult(t, second)["reason"] != "attachment_not_found" {
				t.Fatalf("removed canonical reference still authorized draft export: %+v", second)
			}
			if copied, err := os.ReadFile(absolute); err != nil || !bytes.Equal(copied, data) {
				t.Fatalf("failed export changed prior output: %q %v", copied, err)
			}
		})
	}
}

func TestAttachmentExportDraftRejectsUnownedReferences(t *testing.T) {
	for _, kind := range []string{"no-reference", "other-session", "wrong-origin", "text-only", "wrong-key", "noncanonical-key", "no-history", "history-failure", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			homeDir, project := t.TempDir(), t.TempDir()
			stored, err := attachment.NewService(homeDir).StoreReader(attachment.DraftSessionID, "pasted.txt", "text/plain", bytes.NewBufferString("private draft"))
			if err != nil {
				t.Fatal(err)
			}
			stored.Origin = attachment.OriginTemp
			key := stored.AttachmentKey
			st := memstore.New()
			var parts []store.ContentPart
			sessionID := "session-a"
			switch kind {
			case "other-session":
				sessionID, parts = "session-b", []store.ContentPart{store.AttachmentPart(stored)}
			case "wrong-origin":
				stored.Origin = attachment.OriginUpload
				parts = []store.ContentPart{store.AttachmentPart(stored)}
			case "text-only":
				parts = []store.ContentPart{{Type: store.ContentPartText, Text: key}}
			case "wrong-key":
				stored.AttachmentKey += ".other"
				parts = []store.ContentPart{store.AttachmentPart(stored)}
			case "noncanonical-key":
				key = "sessions/draft/blobs/nested/../" + filepath.Base(key)
				stored.AttachmentKey = key
				parts = []store.ContentPart{store.AttachmentPart(stored)}
			case "no-history", "history-failure", "cancelled":
				parts = []store.ContentPart{store.AttachmentPart(stored)}
			}
			persistExportDraftReference(t, st, sessionID, parts)
			options := []BuiltinOption{WithHomeDir(homeDir)}
			if kind == "history-failure" {
				options = append(options, WithHistorySearch(failingExportDraftHistory{st}))
			} else if kind != "no-history" {
				options = append(options, WithHistorySearch(st))
			}
			runner := NewBuiltinRunner(options...)
			t.Cleanup(func() { _ = runner.Close() })
			ctx := context.Background()
			if kind == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			for _, scope := range []string{"temp", "project"} {
				args := map[string]any{"scope": scope, "attachmentKey": key}
				if scope == "project" {
					args["path"] = "exported.txt"
				}
				raw, _ := json.Marshal(args)
				result := runner.Call(ctx, Call{SessionID: "session-a", Name: AttachmentExport, ProjectDirs: []string{project}, Args: raw})
				if result.Ok {
					t.Errorf("scope=%s unowned draft exported: %+v", scope, result)
				}
			}
			if _, exists, err := home.ExistingSessionArtifacts(homeDir, "session-a"); err != nil || exists {
				t.Errorf("rejected export created artifact directory: %v %v", exists, err)
			}
			if entries, err := os.ReadDir(project); err != nil || len(entries) != 0 {
				t.Errorf("rejected export wrote project: %v %v", entries, err)
			}
			if _, err := os.Stat(filepath.Join(home.TempPath(homeDir), "attachments", filepath.Base(key))); err != nil {
				t.Errorf("rejected export changed source: %v", err)
			}
		})
	}
}

func TestAttachmentExportDraftFollowsForkedCanonicalReference(t *testing.T) {
	ctx := context.Background()
	homeDir := t.TempDir()
	svc := attachment.NewService(homeDir)
	source, err := svc.StoreReader(attachment.DraftSessionID, "pasted.txt", "text/plain", bytes.NewBufferString("forked text"))
	if err != nil {
		t.Fatal(err)
	}
	source.Origin = attachment.OriginTemp
	copy, err := svc.CopyToSession(attachment.DraftSessionID, attachment.DraftSessionID, source)
	if err != nil {
		t.Fatal(err)
	}
	st := memstore.New()
	persistExportDraftReference(t, st, "source-session", []store.ContentPart{store.AttachmentPart(source)})
	if _, err := st.CloneSession(ctx, store.CloneSessionInput{SourceSessionID: "source-session", ThroughMessageID: "input-message", TargetSessionID: "fork-session", TitleSuffix: " fork", AttachmentReplacements: map[string]store.Attachment{source.AttachmentKey: copy}}); err != nil {
		t.Fatal(err)
	}
	runner := NewBuiltinRunner(WithHomeDir(homeDir), WithHistorySearch(st))
	t.Cleanup(func() { _ = runner.Close() })
	for _, test := range []struct {
		session, key string
		allowed      bool
	}{
		{"source-session", source.AttachmentKey, true},
		{"fork-session", copy.AttachmentKey, true},
		{"source-session", copy.AttachmentKey, false},
		{"fork-session", source.AttachmentKey, false},
	} {
		raw, _ := json.Marshal(map[string]string{"scope": "temp", "attachmentKey": test.key})
		result := runner.Call(ctx, Call{SessionID: test.session, Name: AttachmentExport, Args: raw})
		if result.Ok != test.allowed {
			t.Errorf("session=%s key=%s allowed=%v: %+v", test.session, test.key, test.allowed, result)
		}
	}
}

func persistExportDraftReference(t *testing.T, st *memstore.Memstore, sessionID string, parts []store.ContentPart) {
	t.Helper()
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: sessionID, Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.BeginTurn(ctx, store.BeginTurnInput{SessionID: sessionID, TurnID: "input-turn", UserMessageID: "input-message", ClientMessageID: "input-client", UserParts: parts}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FinishTurn(ctx, store.FinishTurnInput{TurnID: "input-turn", Status: store.TurnCompleted}); err != nil {
		t.Fatal(err)
	}
}

type failingExportDraftHistory struct{ *memstore.Memstore }

func (f failingExportDraftHistory) ListMessages(context.Context, string, int) ([]*store.Message, error) {
	return nil, errors.New("test history read failure")
}

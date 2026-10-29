package sqlitestore

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestAssistantTaskOriginMigration(t *testing.T) {
	s, path := openTestStore(t)
	ctx := context.Background()
	profile, err := s.OpenAssistant(ctx, "小布", "mock", "mock", store.AssistantAppearance{"version": 1})
	if err != nil {
		t.Fatal(err)
	}
	sid := profile.Assistant.SessionID
	beginTestTurn(t, s, sid, "source", "source-message", "source-client")
	cases := []struct {
		name                string
		create, call, owned bool
	}{
		{"created", true, true, true},
		{"reused", false, true, false},
		{"missing-evidence", true, false, false},
		{"wrong-call", true, true, false},
		{"wrong-author", true, true, false},
	}
	for _, tc := range cases {
		worker := &store.Session{ID: tc.name, Title: tc.name, Provider: "mock", Model: "mock"}
		if !tc.create {
			if err := s.CreateSession(ctx, worker); err != nil {
				t.Fatal(err)
			}
		}
		if tc.call {
			args := map[string]string{"title": tc.name, "prompt": "Work"}
			if !tc.create {
				args["session_id"] = worker.ID
			}
			raw, _ := json.Marshal(args)
			callID := tc.name
			if tc.name == "wrong-call" {
				callID = "unrelated"
			}
			out, err := s.AppendTurnOutput(ctx, store.AppendTurnOutputInput{TurnID: "source", Parts: []store.ContentPart{{Type: store.ContentPartToolUse, CallID: callID, Name: "builtin_assistant_task_dispatch", Args: raw}}})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "wrong-author" {
				if _, err := s.db.Exec(`UPDATE messages SET role='user' WHERE id=?`, out.Messages[0].ID); err != nil {
					t.Fatal(err)
				}
			}
		}
		_, err := s.AssignAssistantTask(ctx, store.AssignAssistantTaskInput{AssistantSessionID: sid, SourceTurnID: "source", CallID: tc.name, Title: tc.name, Session: worker, CreateSession: tc.create, Input: store.QueueInputInput{SessionID: worker.ID, ClientMessageID: tc.name, Text: "Work", Provider: "mock", Model: "mock", Mode: store.ModeChat}})
		if err != nil {
			t.Fatal(err)
		}
		if created, err := s.IsAssistantCreatedSession(ctx, sid, worker.ID); err != nil || created != tc.create {
			t.Fatal("incorrect live creation origin", tc.name, created, err)
		}
		if _, err := s.PromoteNextQueuedInput(ctx, store.PromoteQueuedInputInput{SessionID: worker.ID, TurnID: tc.name + "-turn", UserMessageID: tc.name + "-input"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.FinishTurn(ctx, store.FinishTurnInput{TurnID: tc.name + "-turn", Status: store.TurnCompleted}); err != nil {
			t.Fatal(err)
		}
	}
	// Reopen a real V58 database: trusted original dispatch calls recover origin,
	// while reused sessions and unrelated messages must never gain the exemption.
	if _, err := s.db.Exec(`ALTER TABLE assistant_tasks DROP COLUMN created_session; PRAGMA user_version=58;`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, tc := range cases {
		if created, err := s.IsAssistantCreatedSession(ctx, sid, tc.name); err != nil || created != tc.owned {
			t.Fatal("incorrect migrated origin", tc.name, created, err)
		}
	}
	if created, err := s.IsAssistantCreatedSession(ctx, "another-assistant", "created"); err != nil || created {
		t.Fatal("origin leaked to another assistant", created, err)
	}
	// The marker survives conversation-history compaction/removal.
	if _, err := s.db.Exec(`DELETE FROM messages WHERE session_id=?`, sid); err != nil {
		t.Fatal(err)
	}
	if created, err := s.IsAssistantCreatedSession(ctx, sid, "created"); err != nil || !created {
		t.Fatal("origin depended on live message history", created, err)
	}
}

package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func TestNoteEditLeaseAtomicCommitAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lease.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { s.Close() }()
	user := store.ContentAuthor{Kind: "user"}
	createTestSession(t, s, "writer")
	if _, err = s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "edit"}); err != nil {
		t.Fatal(err)
	}
	ai := store.ContentAuthor{Kind: "session", SessionID: "writer", TurnID: "turn"}
	body := "前言🙂\r\n\r\n目标段落。\r\n\r\nEnd\r\n"
	doc, err := s.CreateNote(ctx, &store.StudioItem{ID: "doc", Kind: "note", Name: "Notes", SourceSessionID: "writer"}, body, user)
	if err != nil {
		t.Fatal(err)
	}
	begin := store.NoteEditRequest{Action: "begin", ClientRequestID: "begin", ExpectedHash: doc.ContentHash, Target: "目标段落。", Author: ai}
	op, err := s.ChangeNoteEdit(ctx, "doc", begin)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.ChangeNoteEdit(ctx, "doc", begin)
	if err != nil || retry.ID != op.ID || retry.LeaseToken != op.LeaseToken {
		t.Fatal("begin retry", retry, err)
	}
	snapshot, err := s.GetNoteEditState(ctx, "doc")
	if err != nil || len(snapshot.Operations) != 1 || snapshot.Operations[0].LeaseToken != "" || snapshot.Operations[0].Target != "" {
		t.Fatal("snapshot leaked secret", snapshot, err)
	}
	badBody := strings.Replace(body, "目标", "错误", 1)
	attempts := []store.NoteWrite{
		{ClientRequestID: "body", Body: &badBody, ExpectedHash: &doc.ContentHash, Author: user},
		{ClientRequestID: "edits", Edits: []store.NoteEdit{{Old: "目标", New: "错误"}}, Author: ai},
	}
	for _, in := range attempts {
		var locked *store.NoteLocked
		if _, err = s.WriteNote(ctx, "doc", in); !errors.As(err, &locked) {
			t.Fatalf("lock bypass: %v", err)
		}
	}
	// Preserve-only archives a buffer, without modifying the protected working body.
	if _, err = s.WriteNote(ctx, "doc", store.NoteWrite{ClientRequestID: "preserve", Body: &badBody, ExpectedHash: &doc.ContentHash, PreserveOnly: true, Author: user}); err != nil {
		t.Fatal(err)
	}
	disjoint := strings.Replace(body, "前言", "扩展的前言", 1)
	doc, err = s.WriteNote(ctx, "doc", store.NoteWrite{ClientRequestID: "human", Body: &disjoint, ExpectedHash: &doc.ContentHash, Author: user})
	if err != nil {
		t.Fatal("disjoint write", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = s.GetNoteEditState(ctx, "doc")
	if err != nil || snapshot.Operations[0].From != strings.Index(disjoint, "目标") {
		t.Fatal("restart/remap", snapshot, err)
	}
	replacement := "AI 完成🙂。"
	commit := store.NoteWrite{ClientRequestID: "commit", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &replacement, Author: ai}
	wrong := commit
	wrong.ClientRequestID = "wrong"
	wrong.LeaseToken = "wrong"
	if _, err = s.WriteNote(ctx, "doc", wrong); err == nil {
		t.Fatal("wrong token accepted")
	}
	wrong = commit
	wrong.ClientRequestID = "wrong-owner"
	wrong.Author = user
	if _, err = s.WriteNote(ctx, "doc", wrong); err == nil {
		t.Fatal("wrong owner accepted")
	}
	doc, err = s.WriteNote(ctx, "doc", commit)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(disjoint, "目标段落。", replacement, 1)
	if doc.Body != want {
		t.Fatalf("lost disjoint text: %q", doc.Body)
	}
	done, err := s.GetNoteEditState(ctx, "doc")
	if err != nil || done.Operations[0].State != "committed" || done.Operations[0].RevisionID != doc.RevisionID {
		t.Fatal("missing atomic result", done, err)
	}
	// Later edits survive a replay and a selective undo of the AI commit.
	later := want + "Human footer\n"
	laterDoc, err := s.WriteNote(ctx, "doc", store.NoteWrite{ClientRequestID: "later", Body: &later, ExpectedHash: &doc.ContentHash, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	replay, err := s.WriteNote(ctx, "doc", commit)
	if err != nil || replay.RevisionID != doc.RevisionID {
		t.Fatal("commit replay", err)
	}
	current, _ := s.GetNote(ctx, "doc")
	if current.RevisionID != laterDoc.RevisionID {
		t.Fatal("replay moved head")
	}
	undo, err := s.WriteNote(ctx, "doc", store.NoteWrite{ClientRequestID: "undo", UndoRevision: doc.RevisionID, Author: user})
	if err != nil || undo.Body != disjoint+"Human footer\n" {
		t.Fatal("selective undo", undo, err)
	}
}

func TestNoteEditExpiryCancelAndValidation(t *testing.T) {
	ctx := context.Background()
	s, _ := openTestStore(t)
	user := store.ContentAuthor{Kind: "user"}
	doc, err := s.CreateNote(ctx, &store.StudioItem{ID: "lease", Kind: "note", Name: "Lease"}, "中文🙂\n\nSame\n\nSame\n", user)
	if err != nil {
		t.Fatal(err)
	}
	begin := func(id string, from, to int, target string) (*store.NoteEditOperation, error) {
		return s.ChangeNoteEdit(ctx, "lease", store.NoteEditRequest{Action: "begin", ClientRequestID: id, ExpectedHash: doc.ContentHash, From: from, To: to, Target: target, Author: user})
	}
	if _, err = begin("utf8", 1, 3, ""); err == nil {
		t.Fatal("split UTF8 accepted")
	}
	if _, err = begin("ambiguous", 0, 0, "Same"); err == nil {
		t.Fatal("ambiguous target accepted")
	}
	op, err := begin("valid", 0, len("中文🙂"), "")
	if err != nil {
		t.Fatal(err)
	}
	var locked *store.NoteLocked
	if _, err = begin("overlap", 0, 3, ""); !errors.As(err, &locked) {
		t.Fatal("overlap accepted", err)
	}
	renew := store.NoteEditRequest{Action: "renew", ClientRequestID: "renew", OperationID: op.ID, LeaseToken: op.LeaseToken, Author: user}
	if _, err = s.ChangeNoteEdit(ctx, "lease", renew); err != nil {
		t.Fatal(err)
	}
	if err = s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		op.ExpiresAt = time.Now().Add(-time.Second).UnixMilli()
		return saveNoteEditTx(ctx, tx, op)
	}); err != nil {
		t.Fatal(err)
	}
	replacement := "bad"
	if _, err = s.WriteNote(ctx, "lease", store.NoteWrite{ClientRequestID: "late", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &replacement, Author: user}); err == nil {
		t.Fatal("expired commit accepted")
	}
	renew.ClientRequestID = "late-renew"
	if _, err = s.ChangeNoteEdit(ctx, "lease", renew); err == nil {
		t.Fatal("expired lease resurrected")
	}
	op, err = begin("after-expiry", 0, len("中文🙂"), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeNoteEdit(ctx, "lease", store.NoteEditRequest{Action: "cancel", ClientRequestID: "cancel", OperationID: op.ID, Author: user}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.WriteNote(ctx, "lease", store.NoteWrite{ClientRequestID: "cancelled", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &replacement, Author: user}); err == nil {
		t.Fatal("cancelled commit accepted")
	}
}

func TestNoteBatchStreamCommitUndoAndCancellation(t *testing.T) {
	ctx := context.Background()
	s, _ := openTestStore(t)
	user := store.ContentAuthor{Kind: "user"}
	createTestSession(t, s, "batch-writer")
	_, err := s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "batch-writer", TurnID: "batch-turn", UserMessageID: "batch-message", ClientMessageID: "batch-input", UserText: "edit"})
	if err != nil {
		t.Fatal(err)
	}
	ai := store.ContentAuthor{Kind: "session", SessionID: "batch-writer", TurnID: "batch-turn"}
	body := "Intro🙂\r\n\r\nFirst old.\r\n\r\nHuman middle.\r\n\r\nSecond old.\r\n\r\nEnd.\r\n"
	doc, err := s.CreateNote(ctx, &store.StudioItem{ID: "batch", Kind: "page", Name: "Batch"}, body, user)
	if err != nil {
		t.Fatal(err)
	}
	begin := store.NoteEditRequest{Action: "begin", ClientRequestID: "batch-begin", ExpectedHash: doc.ContentHash, Ranges: []store.NoteEditRange{{Target: "First old."}, {Target: "Second old."}}, Author: ai}
	op, err := s.ChangeNoteEdit(ctx, "batch", begin)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.GetNoteEditState(ctx, "batch")
	if err != nil || len(snapshot.Operations[0].Ranges) != 2 {
		t.Fatal(snapshot, err)
	}
	for _, r := range snapshot.Operations[0].Ranges {
		if r.Target != "" {
			t.Fatal("snapshot leaks target")
		}
	}
	// Typing in the gap remains possible while both outer passages are reserved.
	human := strings.Replace(body, "Human middle.", "Human middle expanded independently.", 1)
	doc, err = s.WriteNote(ctx, "batch", store.NoteWrite{ClientRequestID: "batch-human", Body: &human, ExpectedHash: &doc.ContentHash, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	commit := store.NoteWrite{ClientRequestID: "batch-commit", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacements: []string{"First new🙂.\r\n\r\nAnother new paragraph.", ""}, Author: ai}
	bad := commit
	bad.ClientRequestID = "bad-batch"
	bad.Replacements = []string{"Only first"}
	if _, err = s.WriteNote(ctx, "batch", bad); err == nil {
		t.Fatal("partial batch committed")
	}
	unchanged, _ := s.GetNote(ctx, "batch")
	if unchanged.Body != human {
		t.Fatal("failed batch changed body")
	}
	saved, err := s.WriteNote(ctx, "batch", commit)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(strings.Replace(human, "First old.", commit.Replacements[0], 1), "Second old.", "", 1)
	if saved.Body != want {
		t.Fatal("lost gap edit", saved.Body)
	}
	// Later user edits survive one undo of the complete two-range replacement.
	later := want + "User footer.\r\n"
	_, err = s.WriteNote(ctx, "batch", store.NoteWrite{ClientRequestID: "batch-later", Body: &later, ExpectedHash: &saved.ContentHash, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	undone, err := s.WriteNote(ctx, "batch", store.NoteWrite{ClientRequestID: "batch-undo", UndoRevision: saved.RevisionID, Author: user})
	if err != nil || undone.Body != human+"User footer.\r\n" {
		t.Fatal("batch undo", undone, err)
	}
	begin.ClientRequestID = "cancel-begin"
	begin.ExpectedHash = undone.ContentHash
	op, err = s.ChangeNoteEdit(ctx, "batch", begin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.ChangeNoteEdit(ctx, "batch", store.NoteEditRequest{Action: "cancel", ClientRequestID: "batch-cancel", OperationID: op.ID, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	commit.ClientRequestID = "cancelled-commit"
	commit.OperationID = op.ID
	commit.LeaseToken = op.LeaseToken
	if _, err = s.WriteNote(ctx, "batch", commit); err == nil {
		t.Fatal("cancelled batch committed")
	}
	begin.ClientRequestID = "stopped-begin"
	op, err = s.ChangeNoteEdit(ctx, "batch", begin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishTurn(ctx, store.FinishTurnInput{TurnID: "batch-turn", Status: store.TurnCancelled}); err != nil {
		t.Fatal(err)
	}
	commit.ClientRequestID = "stopped-commit"
	commit.OperationID = op.ID
	commit.LeaseToken = op.LeaseToken
	if _, err = s.WriteNote(ctx, "batch", commit); err == nil {
		t.Fatal("stopped turn committed")
	}
	snapshot, err = s.GetNoteEditState(ctx, "batch")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range snapshot.Operations {
		if r.State == "active" {
			t.Fatal("stopped turn kept a lock")
		}
	}
}

func TestNoteInsertionPointsCommitCancelAndUndo(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		at         int
	}{
		{"empty", "", 0}, {"inline", "Hello world.\n\nLast.\n", 6},
		{"between", "Hello world.\n\nLast.\n", 14}, {"append", "Hello.\n", 7},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			s, _ := openTestStore(t)
			user := store.ContentAuthor{Kind: "user"}
			createTestSession(t, s, "insert-writer")
			if _, err := s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "insert-writer", TurnID: "insert-turn", UserMessageID: "message", ClientMessageID: "input", UserText: "insert"}); err != nil {
				t.Fatal(err)
			}
			ai := store.ContentAuthor{Kind: "session", SessionID: "insert-writer", TurnID: "insert-turn"}
			doc, err := s.CreateNote(ctx, &store.StudioItem{ID: "insert", Kind: "page", Name: "Insertion"}, tc.body, user)
			if err != nil {
				t.Fatal(err)
			}
			begin := store.NoteEditRequest{Action: "begin", ClientRequestID: "begin", ExpectedHash: doc.ContentHash, From: tc.at, To: tc.at, Author: ai}
			op, err := s.ChangeNoteEdit(ctx, "insert", begin)
			if err != nil {
				t.Fatal(err)
			}
			overlap := begin
			overlap.ClientRequestID = "overlap"
			var locked *store.NoteLocked
			if _, err = s.ChangeNoteEdit(ctx, "insert", overlap); !errors.As(err, &locked) {
				t.Fatal("insertion point was not protected", err)
			}
			bad := tc.body[:tc.at] + "Human" + tc.body[tc.at:]
			if _, err = s.WriteNote(ctx, "insert", store.NoteWrite{ClientRequestID: "bad", Body: &bad, ExpectedHash: &doc.ContentHash, Author: user}); !errors.As(err, &locked) {
				t.Fatal("insertion lock bypass", err)
			}
			text := "New 中文🙂\n\n"
			written, err := s.WriteNote(ctx, "insert", store.NoteWrite{ClientRequestID: "commit", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &text, Author: ai})
			if err != nil || written.Body != tc.body[:tc.at]+text+tc.body[tc.at:] {
				t.Fatal("insertion", written, err)
			}
			undone, err := s.WriteNote(ctx, "insert", store.NoteWrite{ClientRequestID: "undo", UndoRevision: written.RevisionID, Author: user})
			if err != nil || undone.Body != tc.body {
				t.Fatal("undo insertion", undone, err)
			}
			begin.ClientRequestID = "begin-again"
			begin.ExpectedHash = undone.ContentHash
			op, err = s.ChangeNoteEdit(ctx, "insert", begin)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.ChangeNoteEdit(ctx, "insert", store.NoteEditRequest{Action: "cancel", ClientRequestID: "cancel", OperationID: op.ID, Author: user}); err != nil {
				t.Fatal(err)
			}
			if _, err = s.WriteNote(ctx, "insert", store.NoteWrite{ClientRequestID: "late", OperationID: op.ID, LeaseToken: op.LeaseToken, Replacement: &text, Author: ai}); err == nil {
				t.Fatal("cancelled insertion accepted")
			}
		})
	}
}

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

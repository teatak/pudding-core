package sqlitestore

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
)

func TestDocumentConcurrentEditingVersionsAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	createTestSession(t, s, "writer")
	if _, err = s.BeginTurn(ctx, store.BeginTurnInput{SessionID: "writer", TurnID: "turn", UserMessageID: "message", ClientMessageID: "input", UserText: "edit"}); err != nil {
		t.Fatal(err)
	}
	user := store.ContentAuthor{Kind: "user"}
	ai := store.ContentAuthor{Kind: "session", SessionID: "writer", TurnID: "turn"}
	doc, err := s.CreateDocument(ctx, &store.StudioItem{ID: "doc", Kind: "doc", Name: "Notes", SourceSessionID: "writer"}, "One\n\nTwo\n", user)
	if err != nil {
		t.Fatal(err)
	}
	initial := *doc
	humanBody := "Human one\n\nTwo\n"
	doc, err = s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "human", Body: &humanBody, ExpectedHash: &doc.ContentHash, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	edit := store.DocumentWrite{ClientRequestID: "ai", Edits: []store.DocumentEdit{{Old: "Two", New: "AI two"}}, Author: ai}
	doc, err = s.WriteDocument(ctx, "doc", edit)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Body != "Human one\n\nAI two\n" {
		t.Fatal("concurrent edits lost", doc.Body)
	}
	if _, err = s.WriteDocument(ctx, "doc", edit); err != nil {
		t.Fatal("retry not idempotent", err)
	}
	edit.Edits[0].New = "changed retry"
	if _, err = s.WriteDocument(ctx, "doc", edit); err == nil {
		t.Fatal("request ID reuse accepted")
	}
	dirty := "Unsaved human buffer"
	var conflict *store.ContentConflict
	if _, err = s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "conflict", Body: &dirty, ExpectedHash: &initial.ContentHash, Author: user}); !errors.As(err, &conflict) || conflict.CurrentHash != doc.ContentHash {
		t.Fatal("missing conflict", err)
	}
	preserved, err := s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "preserve", Body: &dirty, ExpectedHash: &initial.ContentHash, PreserveOnly: true, Author: user})
	if err != nil || preserved.Body != doc.Body {
		t.Fatal("preserve replaced working body", preserved, err)
	}
	restored, err := s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: "restore", RestoreRevision: initial.RevisionID, ExpectedHash: &doc.ContentHash, Author: user})
	if err != nil {
		t.Fatal(err)
	}
	if restored.Body != initial.Body || restored.RevisionID == initial.RevisionID {
		t.Fatal("restore reused old version", restored)
	}
	edit.Edits[0].New = "AI two"
	retried, err := s.WriteDocument(ctx, "doc", edit)
	if err != nil || *retried != *doc {
		t.Fatal("retry must return the original write, not a later author's version", retried, err)
	}
	revisions, err := s.ListStudioItemRevisions(ctx, "doc")
	if err != nil || len(revisions) != 5 {
		t.Fatal("history lost", len(revisions), err)
	}
	seenAI, seenDirty := false, false
	for _, r := range revisions {
		full, err := s.GetStudioItemRevision(ctx, "doc", r.Hash)
		if err != nil {
			t.Fatal(err)
		}
		if r.Body != nil {
			t.Fatal("list includes full body")
		}
		seenAI = seenAI || (r.Author.Kind == "session" && r.Author.SessionID == "writer" && r.Author.TurnID == "turn")
		seenDirty = seenDirty || *full.Body == dirty
	}
	if !seenAI || !seenDirty {
		t.Fatal("author or dirty buffer lost")
	}
	if err = s.DeleteSession(ctx, "writer"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	persisted, err := s.GetDocument(ctx, "doc")
	if err != nil || *persisted != *restored {
		t.Fatal("restart lost document", persisted, err)
	}
}

func TestDocumentEditsAreAtomicAndSizeBounded(t *testing.T) {
	s, _ := openTestStore(t)
	ctx := context.Background()
	author := store.ContentAuthor{Kind: "user"}
	doc, err := s.CreateDocument(ctx, &store.StudioItem{ID: "doc", Kind: "doc", Name: "Notes"}, "alpha beta alpha", author)
	if err != nil {
		t.Fatal(err)
	}
	for i, edits := range [][]store.DocumentEdit{
		{{Old: "alpha", New: "ambiguous"}},
		{{Old: "beta", New: "first"}, {Old: "missing", New: "second"}},
		{{Old: "alpha beta", New: "first"}, {Old: "beta alpha", New: "second"}},
		{{Old: "beta", New: strings.Repeat("x", contracts.Studio().MaxDocumentBytes)}},
	} {
		if _, err := s.WriteDocument(ctx, "doc", store.DocumentWrite{ClientRequestID: store.NewID("write"), Edits: edits, Author: author}); err == nil {
			t.Fatal("invalid edits accepted", i)
		}
		after, _ := s.GetDocument(ctx, "doc")
		if *after != *doc {
			t.Fatal("partial write", i)
		}
	}
	if _, err = s.CreateDocument(ctx, &store.StudioItem{ID: "large", Kind: "doc", Name: "Too large"}, strings.Repeat("x", contracts.Studio().MaxDocumentBytes+1), author); err == nil {
		t.Fatal("oversized document accepted")
	}
	if _, err = s.GetStudioItem(ctx, "large"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("failed create left an item")
	}
}

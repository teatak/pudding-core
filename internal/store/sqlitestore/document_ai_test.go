package sqlitestore

import (
	"context"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"path/filepath"
	"testing"
)

func TestDocumentAIPersistenceUndoAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "rich.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := func(text string) store.DocumentNode {
		return store.DocumentNode{Type: "paragraph", Content: []store.DocumentNode{{Type: "text", Text: text}}}
	}
	body, err := store.NewDocumentBody([]store.DocumentNode{p("Original 😀"), p("Untouched")}, "create")
	if err != nil {
		t.Fatal(err)
	}
	author := store.ContentAuthor{Kind: "session", SessionID: "session", TurnID: "turn"}
	item := &store.StudioItem{ID: "rich", Kind: store.StudioItemKindDocument, Name: "AI document"}
	first, err := s.CreateDocument(ctx, item, body, nil, author)
	if err != nil {
		t.Fatal(err)
	}
	text := "Revised 😀"
	blockID := body.Body.Content[0].Attrs["blockId"].(string)
	write := store.DocumentWrite{ClientRequestID: "ai", ExpectedHash: first.ContentHash, Author: author, Operations: []store.DocumentOperation{{Kind: "replace_text", BlockID: blockID, OldText: "Original 😀", Text: &text}}}
	edited, err := s.WriteDocument(ctx, item.ID, write)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.WriteDocument(ctx, item.ID, write)
	if err != nil || retry.RevisionID != edited.RevisionID {
		t.Fatal("edit retry", err)
	}
	createRetry, err := s.CreateDocument(ctx, &store.StudioItem{ID: item.ID, Kind: store.StudioItemKindDocument, Name: item.Name}, body, nil, author)
	if err != nil || createRetry.RevisionID != first.RevisionID {
		t.Fatal("create retry", err)
	}
	latest, _ := s.GetDocument(ctx, item.ID)
	if latest.RevisionID != edited.RevisionID {
		t.Fatal("retry reset edited head")
	}
	revisions, _ := s.ListStudioItemRevisions(ctx, item.ID)
	if len(revisions) != 2 || revisions[0].Author == nil || *revisions[0].Author != author || *revisions[1].Author != author {
		t.Fatal(revisions)
	}
	write.ClientRequestID = "stale"
	if _, err = s.WriteDocument(ctx, item.ID, write); err == nil {
		t.Fatal("stale hash accepted")
	}
	current := edited.Body
	current.Body.Content[1].Content[0].Text = "Later human edit"
	human, err := s.WriteDocument(ctx, item.ID, store.DocumentWrite{ClientRequestID: "human", ExpectedHash: edited.ContentHash, Body: &current})
	if err != nil {
		t.Fatal(err)
	}
	undo := store.DocumentWrite{ClientRequestID: "undo", ExpectedHash: human.ContentHash, UndoRevision: edited.RevisionID}
	undone, err := s.WriteDocument(ctx, item.ID, undo)
	if err != nil {
		t.Fatal(err)
	}
	if undone.Body.Body.Content[0].Content[0].Text != "Original 😀" || undone.Body.Body.Content[1].Content[0].Text != "Later human edit" {
		t.Fatal(undone)
	}
	retry, err = s.WriteDocument(ctx, item.ID, undo)
	if err != nil || retry.RevisionID != undone.RevisionID {
		t.Fatal("undo retry", err)
	}
	undo.ClientRequestID = "forbidden"
	undo.Author = author
	undo.ExpectedHash = undone.ContentHash
	if _, err = s.WriteDocument(ctx, item.ID, undo); err == nil {
		t.Fatal("AI could invoke user undo")
	}
	if _, err = s.GetNote(ctx, item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("rich content exposed to Markdown")
	}
	if _, err = s.WriteNote(ctx, item.ID, store.NoteWrite{ClientRequestID: "wrong", Body: &text}); err == nil {
		t.Fatal("Markdown write changed rich document")
	}
}

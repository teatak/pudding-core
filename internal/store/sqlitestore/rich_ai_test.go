package sqlitestore

import (
	"context"
	"errors"
	"github.com/teatak/pudding-core/internal/store"
	"path/filepath"
	"testing"
)

func TestRichAIPersistenceUndoAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s, err := Open(filepath.Join(t.TempDir(), "rich.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := func(text string) store.RichNode {
		return store.RichNode{Type: "paragraph", Content: []store.RichNode{{Type: "text", Text: text}}}
	}
	body, err := store.NewRichBody([]store.RichNode{p("Original 😀"), p("Untouched")}, "create")
	if err != nil {
		t.Fatal(err)
	}
	author := store.ContentAuthor{Kind: "session", SessionID: "session", TurnID: "turn"}
	item := &store.StudioItem{ID: "rich", Kind: store.StudioItemKindRichDocument, Name: "AI document"}
	first, err := s.CreateRichDocument(ctx, item, body, nil, author)
	if err != nil {
		t.Fatal(err)
	}
	text := "Revised 😀"
	blockID := body.Body.Content[0].Attrs["blockId"].(string)
	write := store.RichDocumentWrite{ClientRequestID: "ai", ExpectedHash: first.ContentHash, Author: author, Operations: []store.RichOperation{{Kind: "replace_text", BlockID: blockID, OldText: "Original 😀", Text: &text}}}
	edited, err := s.WriteRichDocument(ctx, item.ID, write)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := s.WriteRichDocument(ctx, item.ID, write)
	if err != nil || retry.RevisionID != edited.RevisionID {
		t.Fatal("edit retry", err)
	}
	createRetry, err := s.CreateRichDocument(ctx, &store.StudioItem{ID: item.ID, Kind: store.StudioItemKindRichDocument, Name: item.Name}, body, nil, author)
	if err != nil || createRetry.RevisionID != first.RevisionID {
		t.Fatal("create retry", err)
	}
	latest, _ := s.GetRichDocument(ctx, item.ID)
	if latest.RevisionID != edited.RevisionID {
		t.Fatal("retry reset edited head")
	}
	revisions, _ := s.ListStudioItemRevisions(ctx, item.ID)
	if len(revisions) != 2 || revisions[0].Author == nil || *revisions[0].Author != author || *revisions[1].Author != author {
		t.Fatal(revisions)
	}
	write.ClientRequestID = "stale"
	if _, err = s.WriteRichDocument(ctx, item.ID, write); err == nil {
		t.Fatal("stale hash accepted")
	}
	current := edited.Body
	current.Body.Content[1].Content[0].Text = "Later human edit"
	human, err := s.WriteRichDocument(ctx, item.ID, store.RichDocumentWrite{ClientRequestID: "human", ExpectedHash: edited.ContentHash, Body: &current})
	if err != nil {
		t.Fatal(err)
	}
	undo := store.RichDocumentWrite{ClientRequestID: "undo", ExpectedHash: human.ContentHash, UndoRevision: edited.RevisionID}
	undone, err := s.WriteRichDocument(ctx, item.ID, undo)
	if err != nil {
		t.Fatal(err)
	}
	if undone.Body.Body.Content[0].Content[0].Text != "Original 😀" || undone.Body.Body.Content[1].Content[0].Text != "Later human edit" {
		t.Fatal(undone)
	}
	retry, err = s.WriteRichDocument(ctx, item.ID, undo)
	if err != nil || retry.RevisionID != undone.RevisionID {
		t.Fatal("undo retry", err)
	}
	undo.ClientRequestID = "forbidden"
	undo.Author = author
	undo.ExpectedHash = undone.ContentHash
	if _, err = s.WriteRichDocument(ctx, item.ID, undo); err == nil {
		t.Fatal("AI could invoke user undo")
	}
	if _, err = s.GetDocument(ctx, item.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatal("rich content exposed to Markdown")
	}
	if _, err = s.WriteDocument(ctx, item.ID, store.DocumentWrite{ClientRequestID: "wrong", Body: &text}); err == nil {
		t.Fatal("Markdown write changed rich document")
	}
}

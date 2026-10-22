package sqlitestore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestNativeCreationConcurrentRestartAndLaterEdits(t *testing.T) {
	for _, kind := range []string{"page", "sheet"} {
		t.Run(kind, func(t *testing.T) {
			st, path := openTestStore(t)
			ctx := context.Background()
			author := store.ContentAuthor{Kind: "user"}
			create := func() (string, string, error) {
				item := &store.StudioItem{ID: store.NewID(kind), Kind: kind, Name: "Initial"}
				if kind == "page" {
					out, err := st.CreatePage(ctx, item, "Initial", author, "same-intent")
					if err != nil {
						return "", "", err
					}
					return out.ItemID, out.ContentHash, nil
				}
				out, err := st.CreateSheet(ctx, item, sheetFixture(), author, "same-intent")
				if err != nil {
					return "", "", err
				}
				return out.ItemID, out.ContentHash, nil
			}
			const count = 12
			ids, errs := make(chan string, count), make(chan error, count)
			var workers sync.WaitGroup
			for range count {
				workers.Add(1)
				go func() { defer workers.Done(); id, _, err := create(); ids <- id; errs <- err }()
			}
			workers.Wait()
			close(ids)
			close(errs)
			for err := range errs {
				if err != nil {
					t.Fatal(err)
				}
			}
			var id string
			for got := range ids {
				if id != "" && got != id {
					t.Fatalf("duplicate content: %s %s", id, got)
				}
				id = got
			}
			assertHistory := func(want int) {
				t.Helper()
				revisions, err := st.ListStudioItemRevisions(ctx, id)
				if err != nil || len(revisions) != want {
					t.Fatalf("history: %d %v", len(revisions), err)
				}
			}
			assertHistory(1)
			_, hash, err := create()
			if err != nil {
				t.Fatal(err)
			}
			var updatedHash string
			if kind == "page" {
				body := "Later editing must survive"
				out, err := st.WritePage(ctx, id, store.PageWrite{ClientRequestID: "edit", Body: &body, ExpectedHash: &hash, Author: author})
				if err != nil {
					t.Fatal(err)
				}
				updatedHash = out.ContentHash
			} else {
				out, err := st.WriteSheet(ctx, id, store.SheetWrite{ClientRequestID: "edit", Operations: []store.SheetOperation{sheetCell("r", "a", "Later edit")}, Author: author})
				if err != nil {
					t.Fatal(err)
				}
				updatedHash = out.ContentHash
			}
			item, err := st.GetStudioItem(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			item.Name = "Renamed"
			if _, err := st.UpdateStudioItem(ctx, item, item.Revision); err != nil {
				t.Fatal(err)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer st.Close()
			replayID, replayHash, err := create()
			if err != nil || replayID != id || replayHash != updatedHash {
				t.Fatalf("restart replay: %s %s %v", replayID, replayHash, err)
			}
			item, err = st.GetStudioItem(ctx, id)
			if err != nil || item.Name != "Renamed" {
				t.Fatalf("rename lost: %+v %v", item, err)
			}
			assertHistory(2)
			item, err = st.SetStudioItemArchived(ctx, id, item.Revision, true)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := create(); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("revived archive: %v", err)
			}
			if _, err := st.MarkStudioItemDeleted(ctx, id, item.Revision); err != nil {
				t.Fatal(err)
			}
			if _, _, err := create(); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("revived deletion: %v", err)
			}
		})
	}
}

func TestNativeCreationRejectsKeyReuseAndRollsBackFailure(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	author := store.ContentAuthor{Kind: "user"}
	item := func() *store.StudioItem {
		return &store.StudioItem{ID: store.NewID("page"), Kind: "page", Name: "Initial", Icon: "file-text", IconColor: "blue"}
	}
	create := func(i *store.StudioItem, body string, key ...string) (*store.PageContent, error) {
		return st.CreatePage(ctx, i, body, author, key...)
	}
	first, err := create(item(), "Initial", "request")
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*store.StudioItem){
		func(i *store.StudioItem) { i.Name = "Changed" },
		func(i *store.StudioItem) { i.IconColor = "red" },
		func(i *store.StudioItem) { i.NameMode = "manual" },
		func(i *store.StudioItem) { i.SourceSessionID = "other" },
	} {
		i := item()
		change(i)
		var invalid *store.InvalidContent
		if _, err := create(i, "Initial", "request"); !errors.As(err, &invalid) {
			t.Fatalf("parameter reuse: %v", err)
		}
	}
	var invalid *store.InvalidContent
	if _, err := create(item(), "Different body", "request"); !errors.As(err, &invalid) {
		t.Fatalf("body reuse: %v", err)
	}
	if _, err := st.CreateSheet(ctx, &store.StudioItem{Kind: "sheet", Name: "Initial"}, sheetFixture(), author, "request"); !errors.As(err, &invalid) {
		t.Fatalf("kind reuse: %v", err)
	}
	// A failure after the item has been inserted must roll back its identity too.
	if _, err := st.db.Exec(`CREATE TRIGGER fail_native_creation BEFORE INSERT ON studio_item_saves BEGIN SELECT RAISE(ABORT, 'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := create(item(), "Retry", "failed"); err == nil {
		t.Fatal("expected injected failure")
	}
	if _, err := st.db.Exec(`DROP TRIGGER fail_native_creation`); err != nil {
		t.Fatal(err)
	}
	retry, err := create(item(), "Retry", "failed")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := create(item(), "Retry", "failed")
	if err != nil || replay.ItemID != retry.ItemID {
		t.Fatalf("failed request retry: %+v %v", replay, err)
	}
	other, err := create(item(), "Initial", "another")
	if err != nil || other.ItemID == first.ItemID {
		t.Fatalf("independent key: %+v %v", other, err)
	}
	legacy1, err := create(item(), "Initial")
	if err != nil {
		t.Fatal(err)
	}
	legacy2, err := create(item(), "Initial")
	if err != nil || legacy1.ItemID == legacy2.ItemID {
		t.Fatalf("legacy independent creation: %+v %v", legacy2, err)
	}
}

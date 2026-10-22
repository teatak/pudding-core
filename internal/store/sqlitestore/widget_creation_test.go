package sqlitestore

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestWidgetCreationConcurrentRestartAndLaterChanges(t *testing.T) {
	st, path := openTestStore(t)
	ctx := context.Background()
	create := func() (*store.StudioItem, error) {
		return st.CreateStudioItem(ctx, &store.StudioItem{ID: store.NewID("widget"), Kind: "widget", Name: "Initial"}, store.StudioItemCreation{ClientRequestID: "one-intent", InitialDraftHash: "starter"})
	}
	const count = 12
	ids, errs := make(chan string, count), make(chan error, count)
	var workers sync.WaitGroup
	for range count {
		workers.Add(1)
		go func() {
			defer workers.Done()
			item, err := create()
			if err == nil {
				ids <- item.ID
			}
			errs <- err
		}()
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
		if id != "" && id != got {
			t.Fatalf("duplicate: %s %s", id, got)
		}
		id = got
	}
	item, err := st.GetStudioItem(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	item.Name = "Later name"
	item.IconColor = "blue"
	if _, err = st.UpdateStudioItem(ctx, item, item.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = st.SaveStudioItemRevision(ctx, &store.StudioItemRevision{ItemID: id, Hash: "later-source", ClientRequestID: "edit"}, ""); err != nil {
		t.Fatal(err)
	}
	if err = st.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	retry, err := create()
	if err != nil || retry.ID != id || retry.Name != "Later name" || retry.IconColor != "blue" || retry.HeadRevision != "later-source" {
		t.Fatalf("replay lost changes: %+v %v", retry, err)
	}
	revisions, err := st.ListStudioItemRevisions(ctx, id)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("creation added source history: %+v %v", revisions, err)
	}
	item, err = st.SetStudioItemArchived(ctx, id, retry.Revision, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = create(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("archive revived: %v", err)
	}
	if _, err = st.MarkStudioItemDeleted(ctx, id, item.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err = create(); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("deletion revived: %v", err)
	}
}

func TestWidgetCreationIdentityAndTransaction(t *testing.T) {
	st, _ := openTestStore(t)
	ctx := context.Background()
	item := func() *store.StudioItem {
		return &store.StudioItem{ID: store.NewID("widget"), Kind: "widget", Name: "Same name", IconColor: "blue"}
	}
	key := store.StudioItemCreation{ClientRequestID: "request", InitialDraftHash: "template-a"}
	first, err := st.CreateStudioItem(ctx, item(), key)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*store.StudioItem){
		func(w *store.StudioItem) { w.Name = "Different" },
		func(w *store.StudioItem) { w.IconColor = "red" },
		func(w *store.StudioItem) { w.SourceSessionID = "different-session" },
	} {
		w := item()
		change(w)
		var invalid *store.InvalidContent
		if _, err = st.CreateStudioItem(ctx, w, key); !errors.As(err, &invalid) {
			t.Fatalf("changed intent accepted: %v", err)
		}
	}
	changed := key
	changed.InitialDraftHash = "template-b"
	if _, err = st.CreateStudioItem(ctx, item(), changed); err == nil {
		t.Fatal("changed template accepted")
	}
	key.ClientRequestID = "another"
	next, err := st.CreateStudioItem(ctx, item(), key)
	if err != nil || next.ID == first.ID {
		t.Fatalf("explicit new intent deduplicated: %+v %v", next, err)
	}
	for range 2 {
		legacy, err := st.CreateStudioItem(ctx, item())
		if err != nil || legacy.ID == first.ID {
			t.Fatalf("legacy independent creation: %+v %v", legacy, err)
		}
	}
	if _, err = st.db.Exec(`CREATE TRIGGER fail_widget_creation BEFORE INSERT ON studio_item_saves BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	key.ClientRequestID = "failed"
	if _, err = st.CreateStudioItem(ctx, item(), key); err == nil {
		t.Fatal("missing injected failure")
	}
	if _, err = st.db.Exec(`DROP TRIGGER fail_widget_creation`); err != nil {
		t.Fatal(err)
	}
	created, err := st.CreateStudioItem(ctx, item(), key)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := st.CreateStudioItem(ctx, item(), key)
	if err != nil || retry.ID != created.ID {
		t.Fatalf("failed transaction poisoned identity: %+v %v", retry, err)
	}
}

package tool

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

type lazyToolBrowser struct {
	fakeToolBrowser
	recovered []string
}

func (*lazyToolBrowser) SupportsMetadataRecovery() bool { return true }
func (f *lazyToolBrowser) GetTab(_ context.Context, sessionID, tabID string) (browser.TabSnapshot, error) {
	for _, tab := range f.tabs {
		if tab.SessionID == sessionID && tab.ID == tabID {
			return tab, nil
		}
	}
	return browser.TabSnapshot{}, browser.ErrTabNotFound
}
func (f *lazyToolBrowser) Recover(_ context.Context, sessionID string, hint browser.RecoverHint) (browser.TabSnapshot, error) {
	f.recovered = append(f.recovered, hint.TabID)
	tab := browser.TabSnapshot{ID: hint.TabID, SessionID: sessionID, TargetID: "live:" + hint.TabID, URL: hint.URL, Title: hint.Title}
	f.tabs = append(f.tabs, tab)
	return tab, nil
}

func TestBrowserToolsLoadOnlyTheirRequestedSavedTab(t *testing.T) {
	ctx := context.Background()
	st, err := sqlitestore.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err = st.CreateSession(ctx, &store.Session{ID: "session", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "b", "c"} {
		if _, err = st.PutBrowserState(ctx, store.BrowserStateInput{SessionID: "session", TabID: id, URL: "https://example.com/" + id}); err != nil {
			t.Fatal(err)
		}
	}
	fake := &lazyToolBrowser{}
	runner := NewBuiltinRunner(WithBrowser(fake), WithBrowserState(st))
	call := func(name, args string) Result {
		t.Helper()
		res := runner.Call(ctx, Call{SessionID: "session", Name: name, Args: json.RawMessage(args)})
		if !res.Ok {
			t.Fatal(res.Content)
		}
		return res
	}
	call(BrowserStatus, `{}`)
	call(BrowserClose, `{"tabID":"c"}`)
	if len(fake.recovered) != 0 {
		t.Fatal("status/close loaded pages", fake.recovered)
	}
	call(BrowserObserve, `{"tabID":"b"}`)
	call(BrowserObserve, `{"tabID":"b"}`)
	if len(fake.recovered) != 1 || fake.recovered[0] != "b" {
		t.Fatal("wrong recovery targets", fake.recovered)
	}
	if _, err = runner.browserResolveTabID(ctx, "other-session", "a"); err == nil {
		t.Fatal("cross-session restore allowed")
	}
}

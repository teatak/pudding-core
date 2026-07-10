package sqlitestore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestTerminalMetadataPersistsAndRunningStateResets(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.CreateSession(ctx, &store.Session{ID: "sess", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	item := &store.Terminal{
		ID:        "term_1",
		SessionID: "sess",
		Title:     "zsh",
		CWD:       t.TempDir(),
		Shell:     "/bin/zsh",
		Status:    store.TerminalRunning,
	}
	if err := st.CreateTerminal(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.ResetRunningTerminals(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := reopened.ListTerminals(ctx, "sess")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != item.ID || items[0].Status != store.TerminalExited || items[0].ExitCode != nil {
		t.Fatalf("terminals = %+v", items)
	}
}

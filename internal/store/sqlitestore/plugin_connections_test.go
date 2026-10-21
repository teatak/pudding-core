package sqlitestore

import (
	"context"
	"github.com/teatak/pudding-core/internal/store"
	"path/filepath"
	"testing"
)

func TestSessionConnectionMigrationPersistenceAndIsolation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "pudding.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := s.CreateSession(ctx, &store.Session{ID: id, Provider: "mock", Model: "mock"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec(`DROP TABLE session_plugin_connections; PRAGMA user_version=47`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := s.GetSessionPluginConnection(ctx, "one", "github")
	if err != nil || selected != nil {
		t.Fatal("migration selected account", selected, err)
	}
	if err := s.SetSessionPluginConnection(ctx, "missing", "github", "key", "identity"); err == nil {
		t.Fatal("missing session accepted")
	}
	if err := s.SetSessionPluginConnection(ctx, "one", "github", "key", "identity"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	selected, err = s.GetSessionPluginConnection(ctx, "one", "github")
	if err != nil || selected == nil || selected.ConnectionID != "key" || selected.Identity != "identity" {
		t.Fatal(selected, err)
	}
	other, err := s.GetSessionPluginConnection(ctx, "two", "github")
	if err != nil || other != nil {
		t.Fatal("cross-session selection", other, err)
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id='one'`); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, &store.Session{ID: "one", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	selected, err = s.GetSessionPluginConnection(ctx, "one", "github")
	if err != nil || selected != nil {
		t.Fatal("deleted session selection resurrected", selected, err)
	}
}

// Package storetest opens the daemon's real persistence for tests outside the
// store packages, so no test exercises a separate in-memory implementation.
package storetest

import (
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

// Store is the daemon's two persistence sources under one temporary home, wired
// as the daemon wires them: run data in SQLite and settings and provider
// profiles in YAML config.
type Store struct {
	*sqlitestore.Store
	*config.Manager
}

// New opens an empty home in the test's temporary directory and closes the
// database when the test ends.
func New(t testing.TB) *Store {
	t.Helper()
	home := t.TempDir()
	st, err := sqlitestore.OpenWithHome(filepath.Join(home, "pudding.db"), home)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	cfg := config.NewManager(home)
	if err := cfg.Prepare(); err != nil {
		t.Fatal(err)
	}
	return &Store{Store: st, Manager: cfg}
}

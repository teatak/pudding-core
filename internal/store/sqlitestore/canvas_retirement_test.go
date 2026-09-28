package sqlitestore

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/canvasarchive"
)

func assertArchivesContain(t *testing.T, path string, values ...string) {
	t.Helper()
	entries, err := canvasarchive.List(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for _, entry := range entries {
		b, err := os.ReadFile(filepath.Join(entry.Path, "original.json"))
		if err != nil {
			t.Fatal(err)
		}
		body += string(b)
	}
	for _, value := range values {
		if !strings.Contains(body, value) {
			t.Fatalf("archive missing %q", value)
		}
	}
}

func TestFinalCanvasMigrationRetryReusesVerifiedArchives(t *testing.T) {
	path := canvasV24Fixture(t)
	home := filepath.Dir(path)
	db := openMigrationTestDB(t, path)
	err := runSchemaMigration(db, 25, func(tx *sql.Tx) error {
		if err := migrateFinalCanvases(tx, home); err != nil {
			return err
		}
		return errors.New("injected commit failure")
	})
	db.Close()
	if err == nil {
		t.Fatal("expected injected failure")
	}
	entries, err := canvasarchive.List(home)
	if err != nil || len(entries) != 3 {
		t.Fatalf("verified archives missing: %+v %v", entries, err)
	}
	db = openMigrationTestDB(t, path)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "24")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name='canvas_resources'", "0")
	db.Close()
	st, err := OpenWithHome(path, home)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	again, err := canvasarchive.List(home)
	if err != nil || len(again) != len(entries) {
		t.Fatalf("retry changed archives: %+v %v", again, err)
	}
	for i := range entries {
		if entries[i].ID != again[i].ID {
			t.Fatal("retry duplicated an archive")
		}
	}
}

func TestFinalCanvasMigrationArchiveFailureKeepsReleaseData(t *testing.T) {
	path := canvasV24Fixture(t)
	home := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(home, "archives"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if st, err := OpenWithHome(path, home); err == nil {
		st.Close()
		t.Fatal("expected archive failure")
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "24")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM canvas_saved_items", "1")
}

func TestFreshStoreCreatesNoArchiveEntry(t *testing.T) {
	st, path := openTestStore(t)
	defer st.Close()
	entries, err := canvasarchive.List(filepath.Dir(path))
	if err != nil || len(entries) != 0 {
		t.Fatal(entries, err)
	}
	if _, err = os.Stat(canvasarchive.Root(filepath.Dir(path))); !os.IsNotExist(err) {
		t.Fatal("fresh store created archive directory")
	}
}

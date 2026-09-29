package sqlitestore

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/canvas"
)

func assertConvertedCanvasesContain(t *testing.T, path string, values ...string) {
	t.Helper()
	db := openMigrationTestDB(t, path)
	defer db.Close()
	rows, err := db.Query(`SELECT canvas_id,hash,client_request_id FROM canvas_revisions`)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	for rows.Next() {
		var id, hash, request string
		if err = rows.Scan(&id, &hash, &request); err != nil {
			t.Fatal(err)
		}
		p, err := canvas.ReadPackage(filepath.Dir(path), id, hash)
		if err != nil {
			t.Fatal(err)
		}
		body += id + request
		for _, s := range p.Files {
			body += s
		}
	}
	rows.Close()
	rows, err = db.Query(`SELECT session_id,id FROM canvas_mounts`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var session, id string
		if err = rows.Scan(&session, &id); err != nil {
			t.Fatal(err)
		}
		body += session + id
	}
	rows.Close()
	for _, v := range values {
		if !strings.Contains(body, v) {
			t.Fatalf("converted canvas missing %q", v)
		}
	}
}
func TestFinalCanvasMigrationRetryReusesSourcePackages(t *testing.T) {
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
	before, _ := filepath.Glob(filepath.Join(home, "canvases", "*", "revisions", "*"))
	if len(before) != 3 {
		t.Fatal(before, err)
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
	after, _ := filepath.Glob(filepath.Join(home, "canvases", "*", "revisions", "*"))
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatal("retry duplicated packages", before, after)
	}
}
func TestFinalCanvasMigrationWriteFailureKeepsReleaseData(t *testing.T) {
	path := canvasV24Fixture(t)
	home := filepath.Dir(path)
	if err := os.WriteFile(filepath.Join(home, "canvases"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	if st, err := OpenWithHome(path, home); err == nil {
		st.Close()
		t.Fatal("expected package write failure")
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "24")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM canvas_saved_items", "1")
}
func TestFreshStoreCreatesNoLegacyArchive(t *testing.T) {
	st, path := openTestStore(t)
	defer st.Close()
	if _, err := os.Stat(filepath.Join(filepath.Dir(path), "archives")); !os.IsNotExist(err) {
		t.Fatal("fresh store created archive directory")
	}
}

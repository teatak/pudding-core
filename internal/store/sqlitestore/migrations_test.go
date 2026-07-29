package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestSchemaReleaseContract(t *testing.T) {
	// Published fingerprints are immutable. A schema change must bump
	// currentSchemaVersion, add a migration, and append a new fingerprint.
	releasedFingerprints := map[int]string{
		1: "8c5dc7392f4b5bdc77a1edb38c193789a24a1292defa9bf99b1effd96fbaea3d",
		2: "e48dbb97a116c7dd69130b48d3dcc6eae8bc5e628ff27ca586e70f7000e1e0c4",
		3: "c996914be5b56acc17bd448f3e8d498405ce9392d79248f14550fb1bb46829f1",
		4: "e38283316dd223f2f94183fcd94122e36e5d1ec39d5db578744846055391af59",
		5: "76313b2ba7212b51e772206fa1877c4471a084f787b244096108f242e856ca3f",
		6: "ba7c7608f0c8e450cf193174b80fd7701800309bee57a35b25979d0529eaa7e3",
	}
	want, ok := releasedFingerprints[currentSchemaVersion]
	if !ok {
		t.Fatalf("schema v%d has no released fingerprint", currentSchemaVersion)
	}
	got := fmt.Sprintf("%x", sha256.Sum256([]byte(store.SchemaSQL)))
	if got != want {
		t.Fatalf("schema.sql changed without a new schema version: got %s, want %s", got, want)
	}
	for version := baselineSchemaVersion + 1; version <= currentSchemaVersion; version++ {
		if schemaMigrations[version] == nil {
			t.Fatalf("schema v%d has no migration", version)
		}
	}
}

func TestOpenCreatesVersionedSchema(t *testing.T) {
	st, _ := openTestStore(t)
	version, err := schemaVersion(st.db)
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
}

func TestOpenMigratesVersionThreeBrowserHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`
		DROP INDEX browser_history_visited_at;
		DROP TABLE browser_history;
		PRAGMA user_version = 3;
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	version, err := schemaVersion(reopened.db)
	if err != nil || version != currentSchemaVersion {
		t.Fatalf("schema version=%d err=%v", version, err)
	}
	entries, err := reopened.ListBrowserHistory(context.Background(), "", 20)
	if err != nil || len(entries) != 0 {
		t.Fatalf("browser history migration failed: entries=%+v err=%v", entries, err)
	}
}

func TestOpenMigratesVersionFourUsageCalibration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`
		DROP TABLE usage_calibrations;
		PRAGMA user_version = 4;
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	version, err := schemaVersion(reopened.db)
	if err != nil || version != currentSchemaVersion {
		t.Fatalf("schema version = %d err=%v, want %d", version, err, currentSchemaVersion)
	}
	calibration, err := reopened.UsageCalibration(context.Background(), "profile-a", "model-a")
	if err != nil {
		t.Fatal(err)
	}
	if calibration.SampleCount != 0 || calibration.InputRatioEWMA != 1 {
		t.Fatalf("new calibration table returned %+v", calibration)
	}
}

func TestOpenMigratesVersionFiveFileChangeOrigins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`
		ALTER TABLE turn_file_changes DROP COLUMN origin;
		PRAGMA user_version = 5;
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	version, err := schemaVersion(reopened.db)
	if err != nil || version != currentSchemaVersion {
		t.Fatalf("schema version = %d err=%v, want %d", version, err, currentSchemaVersion)
	}
	var defaultOrigin string
	if err := reopened.db.QueryRow(`
		SELECT dflt_value
		FROM pragma_table_info('turn_file_changes')
		WHERE name = 'origin'
	`).Scan(&defaultOrigin); err != nil {
		t.Fatal(err)
	}
	if defaultOrigin != "'structured'" {
		t.Fatalf("origin default = %q, want 'structured'", defaultOrigin)
	}
}

func TestOpenStampsUnversionedCurrentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(context.Background(), &store.Session{
		ID: "sess_baseline", Title: "baseline", Provider: "mock", Model: "mock",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	version, err := schemaVersion(reopened.db)
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version = %d, want %d", version, currentSchemaVersion)
	}
	session, err := reopened.GetSession(context.Background(), "sess_baseline")
	if err != nil {
		t.Fatal(err)
	}
	if session.Title != "baseline" {
		t.Fatalf("session changed while stamping baseline: %+v", session)
	}
}

func TestOpenMigratesVersionOneFileChangesTable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_v1", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`
		DROP TABLE usage_calibrations;
		DROP TABLE turn_file_changes;
		DROP TABLE canvas_items;
		DROP TABLE canvas_closed_items;
		DROP TABLE canvas_saved_items;
		CREATE TABLE canvas_items (
			id TEXT PRIMARY KEY, canvas_id TEXT NOT NULL DEFAULT 'default', source_session_id TEXT NOT NULL DEFAULT '',
			created_by_session_id TEXT NOT NULL DEFAULT '', updated_by_session_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '', item_json TEXT NOT NULL, window_json TEXT NOT NULL DEFAULT '', visible INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
		CREATE INDEX canvas_items_canvas_visible_updated ON canvas_items(canvas_id,visible,updated_at DESC);
		CREATE TABLE canvas_closed_items (
			id TEXT PRIMARY KEY, source_item_id TEXT NOT NULL UNIQUE, actor_session_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '', item_json TEXT NOT NULL, window_json TEXT NOT NULL DEFAULT '', closed_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
		CREATE INDEX canvas_closed_items_closed_at ON canvas_closed_items(closed_at DESC);
		PRAGMA user_version = 1;
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.GetSession(context.Background(), "sess_v1"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := reopened.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='turn_file_changes'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("turn_file_changes table count = %d", count)
	}
}

func TestOpenMigratesLegacyCanvasDataWithoutLosingOrphans(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession(context.Background(), &store.Session{ID: "sess_keep", Provider: "mock", Model: "mock"}); err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}

	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`
		DROP TABLE usage_calibrations;
		DROP TABLE canvas_items;
		DROP TABLE canvas_closed_items;
		DROP TABLE canvas_saved_items;
		CREATE TABLE canvas_items (
			id TEXT PRIMARY KEY, canvas_id TEXT NOT NULL DEFAULT 'default', source_session_id TEXT NOT NULL DEFAULT '',
			created_by_session_id TEXT NOT NULL DEFAULT '', updated_by_session_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '', item_json TEXT NOT NULL, window_json TEXT NOT NULL DEFAULT '', visible INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
		CREATE INDEX canvas_items_canvas_visible_updated ON canvas_items(canvas_id,visible,updated_at DESC);
		CREATE TABLE canvas_closed_items (
			id TEXT PRIMARY KEY, source_item_id TEXT NOT NULL UNIQUE, actor_session_id TEXT NOT NULL DEFAULT '', kind TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '', item_json TEXT NOT NULL, window_json TEXT NOT NULL DEFAULT '', closed_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL
		);
		CREATE INDEX canvas_closed_items_closed_at ON canvas_closed_items(closed_at DESC);
		INSERT INTO canvas_items(id,source_session_id,kind,title,item_json,created_at,updated_at) VALUES
			('active_keep','sess_keep','markdown','Keep','{}',1,2),
			('active_orphan','sess_deleted','markdown','Orphan','{}',3,4);
		INSERT INTO canvas_closed_items(id,source_item_id,actor_session_id,kind,title,item_json,closed_at,created_at,updated_at) VALUES
			('closed_keep','old_keep','sess_keep','markdown','Closed keep','{}',5,5,5),
			('closed_orphan','old_orphan','sess_deleted','markdown','Closed orphan','{}',6,6,6);
		PRAGMA user_version = 2;
	`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	active, err := reopened.ListCanvasItems(context.Background(), "sess_keep")
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "active_keep" {
		t.Fatalf("session canvas after migration = %+v", active)
	}
	closed, err := reopened.ListClosedCanvasItems(context.Background(), "sess_keep", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(closed) != 1 || closed[0].ID != "closed_keep" {
		t.Fatalf("session closed canvas after migration = %+v", closed)
	}
	saved, err := reopened.ListSavedCanvasItems(context.Background(), "sess_keep")
	if err != nil {
		t.Fatal(err)
	}
	if len(saved) != 2 || saved[0].ID != "legacy_closed_closed_orphan" || saved[1].ID != "active_orphan" {
		t.Fatalf("orphan canvas after migration = %+v", saved)
	}
}

func TestOpenRejectsUnsupportedUnversionedSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`CREATE TABLE sessions (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if st != nil {
		_ = st.Close()
	}
	if !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("Open error = %v, want ErrUnsupportedSchema", err)
	}
}

func TestOpenRejectsNewerSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, currentSchemaVersion+1)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	st, err := Open(path)
	if st != nil {
		_ = st.Close()
	}
	if !errors.Is(err, ErrSchemaTooNew) {
		t.Fatalf("Open error = %v, want ErrSchemaTooNew", err)
	}
}

func TestSchemaMigrationRollsBackVersionAndChanges(t *testing.T) {
	st, _ := openTestStore(t)
	wantErr := errors.New("stop migration")
	err := runSchemaMigration(st.db, 2, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`CREATE TABLE migration_probe (id TEXT PRIMARY KEY)`); err != nil {
			return err
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("migration error = %v, want %v", err, wantErr)
	}
	version, err := schemaVersion(st.db)
	if err != nil {
		t.Fatal(err)
	}
	if version != currentSchemaVersion {
		t.Fatalf("schema version after rollback = %d", version)
	}
	var count int
	if err := st.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='migration_probe'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("failed migration left schema changes behind")
	}
}

func TestBackupDatabaseBeforeMigrationPreservesData(t *testing.T) {
	st, path := openTestStore(t)
	createTestSession(t, st, "sess_backup")
	if err := backupDatabaseBeforeMigration(st.db, path, currentSchemaVersion); err != nil {
		t.Fatal(err)
	}
	backups, err := filepath.Glob(fmt.Sprintf("%s.backup-v%d-*", path, currentSchemaVersion))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 {
		t.Fatalf("migration backups = %v, want one", backups)
	}
	db := openMigrationTestDB(t, backups[0])
	defer db.Close()
	var title string
	if err := db.QueryRow(`SELECT title FROM sessions WHERE id='sess_backup'`).Scan(&title); err != nil {
		t.Fatal(err)
	}
	if title != "sess_backup" {
		t.Fatalf("backup session title = %q", title)
	}
}

func openMigrationTestDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		t.Fatal(err)
	}
	return db
}

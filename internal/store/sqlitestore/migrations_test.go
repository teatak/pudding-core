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
	if _, err := db.Exec(`PRAGMA user_version = 2`); err != nil {
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
	backups, err := filepath.Glob(path + ".backup-v1-*")
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

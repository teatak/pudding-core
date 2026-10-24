package sqlitestore

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrationRetryPreservesPublishedRollbackBackup(t *testing.T) {
	// Exact schema.sql from the Core shipped in 0.7.0 (dfffe578).
	schema, err := os.ReadFile("testdata/schema-v48.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(schema)); got != "e37feda284e160a061c98287c7978d610fa7453217b35f22e08089376b5626d4" {
		t.Fatal("published v48 fixture changed")
	}
	file := filepath.Join(t.TempDir(), "pudding.db")
	db := openMigrationTestDB(t, file)
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
 PRAGMA user_version=48;
 INSERT INTO sessions(id,title,provider,model,created_at,updated_at,last_activity_at) VALUES('saved','Keep history','mock','mock',1,2,3);
 CREATE INDEX assistant_memory_terms ON sessions(title);
 `); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	// v49-v53 commit, but the v54 DDL fails and rolls back.
	st, err := Open(file)
	if err == nil {
		st.Close()
		t.Fatal("expected interrupted migration")
	}
	db = openMigrationTestDB(t, file)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "53")
	assertWorkspaceMigrationValue(t, db, "SELECT title FROM sessions WHERE id='saved'", "Keep history")
	if _, err := db.Exec("DROP INDEX assistant_memory_terms"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := filepath.Glob(file + ".backup-v48-*")
	if err != nil || len(before) != 1 {
		t.Fatalf("v48 backup: %v, %v", before, err)
	}
	original, err := os.ReadFile(before[0])
	if err != nil {
		t.Fatal(err)
	}
	// A retry backs up v53. Success must not discard the v48 rollback point.
	st, err = Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
	assertWorkspaceMigrationValue(t, st.db, "SELECT title FROM sessions WHERE id='saved'", "Keep history")
	kept, err := os.ReadFile(before[0])
	if err != nil {
		t.Fatalf("published rollback backup lost after retry: %v", err)
	}
	if sha256.Sum256(kept) != sha256.Sum256(original) {
		t.Fatal("published rollback backup modified")
	}
	backup := openMigrationTestDB(t, before[0])
	defer backup.Close()
	assertWorkspaceMigrationValue(t, backup, "PRAGMA user_version", "48")
	assertWorkspaceMigrationValue(t, backup, "SELECT title FROM sessions WHERE id='saved'", "Keep history")
	backups := migrationBackupFiles(t, file)
	if len(backups) != 2 {
		t.Fatalf("backups = %v, want published v48 and retry v53", backups)
	}
}

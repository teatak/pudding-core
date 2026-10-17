package sqlitestore

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func pageLegacySchemaV42() string {
	return strings.Replace(store.SchemaSQL, "'page','sheet','widget'", "'note','sheet','widget'", 1)
}

func TestPageMigrationFromV42(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(pageLegacySchemaV42()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
VALUES('legacy_note','note','历史笔记',1,'','','{}',1,0,1,1);
PRAGMA user_version=42;`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "43")
	assertWorkspaceMigrationValue(t, s.db, "SELECT kind FROM studio_items WHERE id='legacy_note'", "page")

	// Verify we can insert a new page directly without violating the CHECK constraint.
	if _, err := s.db.Exec(`INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
VALUES('new_page','page','新页面',1,'','','{}',1,0,2,2);`); err != nil {
		t.Fatalf("failed to insert kind=page into migrated studio_items: %v", err)
	}
}

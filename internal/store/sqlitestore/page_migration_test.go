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
INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at)VALUES('s1','mock','m',1,1,1);
INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
VALUES
 ('legacy_note','note','历史笔记',1,'rev_note','','{}',1,0,1,1),
 ('legacy_sheet','sheet','历史表格',2,'rev_sheet','','{}',1,0,1,2),
 ('legacy_widget','widget','历史组件',1,'rev_widget','','{}',1,0,1,3);

INSERT INTO studio_item_content(item_id, body, content_hash) VALUES
 ('legacy_note', '# 笔记正文', 'hash_note'),
 ('legacy_sheet', '{"columns":[],"rows":[]}', 'hash_sheet'),
 ('legacy_widget', '<html>widget</html>', 'hash_widget');

INSERT INTO studio_item_revisions(item_id, hash, parent_revision, client_request_id, created_at, build_receipt, content, content_hash, author_kind) VALUES
 ('legacy_note', 'rev_note', '', 'req1', 1, '', '# 笔记正文', 'hash_note', 'user'),
 ('legacy_sheet', 'rev_sheet', '', 'req2', 2, '', '{"columns":[],"rows":[]}', 'hash_sheet', 'user'),
 ('legacy_widget', 'rev_widget', '', 'req3', 3, '', '<html>widget</html>', 'hash_widget', 'user');

INSERT INTO studio_sheet_ids(item_id, entity_kind, entity_id) VALUES
 ('legacy_sheet', 'row', 'r1'),
 ('legacy_sheet', 'column', 'c1');

INSERT INTO studio_mounts(session_id, id, item_id, visible, created_at) VALUES
 ('s1', 'm_note', 'legacy_note', 1, 1),
 ('s1', 'm_sheet', 'legacy_sheet', 1, 2);

INSERT INTO library_favorites(id, kind, source_session_id, saved_item_id, url, title, created_at) VALUES
 ('fav_note', 'studio', '', 'legacy_note', '', '', 1);

PRAGMA user_version=42;`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// 1. Version and Pragmas
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA user_version", "43")
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA foreign_keys", "1")
	assertWorkspaceMigrationValue(t, s.db, "PRAGMA integrity_check", "ok")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM pragma_foreign_key_check", "0")

	// 2. Kinds: note -> page, sheet -> sheet, widget -> widget
	assertWorkspaceMigrationValue(t, s.db, "SELECT kind FROM studio_items WHERE id='legacy_note'", "page")
	assertWorkspaceMigrationValue(t, s.db, "SELECT kind FROM studio_items WHERE id='legacy_sheet'", "sheet")
	assertWorkspaceMigrationValue(t, s.db, "SELECT kind FROM studio_items WHERE id='legacy_widget'", "widget")

	// 3. Dependent content & revisions preserved 100%
	assertWorkspaceMigrationValue(t, s.db, "SELECT body FROM studio_item_content WHERE item_id='legacy_note'", "# 笔记正文")
	assertWorkspaceMigrationValue(t, s.db, "SELECT body FROM studio_item_content WHERE item_id='legacy_sheet'", `{"columns":[],"rows":[]}`)
	assertWorkspaceMigrationValue(t, s.db, "SELECT body FROM studio_item_content WHERE item_id='legacy_widget'", "<html>widget</html>")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_item_revisions WHERE item_id='legacy_note'", "1")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_item_revisions WHERE item_id='legacy_sheet'", "1")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_item_revisions WHERE item_id='legacy_widget'", "1")

	// 4. Secondary tables preserved (sheet_ids, mounts, favorites)
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_sheet_ids WHERE item_id='legacy_sheet'", "2")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM studio_mounts WHERE item_id='legacy_note'", "1")
	assertWorkspaceMigrationValue(t, s.db, "SELECT count(*) FROM library_favorites WHERE saved_item_id='legacy_note'", "1")

	// 5. Verify inserting new page
	if _, err := s.db.Exec(`INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
VALUES('new_page','page','新页面',1,'','','{}',1,0,2,2);`); err != nil {
		t.Fatalf("failed to insert kind=page into migrated studio_items: %v", err)
	}

}

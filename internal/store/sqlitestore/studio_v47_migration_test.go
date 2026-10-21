package sqlitestore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestStudioMigrationHasOneDestination(t *testing.T) {
	for version := 33; version < 47; version++ {
		if schemaMigrations[version] != nil {
			t.Fatalf("unpublished v%d remains a migration destination", version)
		}
		if !supportedSchemaVersion(version) {
			t.Fatalf("lost development source v%d", version)
		}
	}
	if schemaMigrations[47] == nil {
		t.Fatal("missing consolidated v47 migration")
	}
}

const studioLegacyBody = "# Kept 🙂\r\n\r\n正文与历史。\r\n"
const studioLegacySheet = `{"columns":[{"id":"c","name":"列","type":"text"}],"rows":[{"id":"r","cells":{"c":"保留"}}]}`

func studioLegacyDB(t *testing.T, path string, version int) *sql.DB {
	t.Helper()
	// Frozen published 0.5.0 source: 1571c7bacc4aa7f9e2dd7b9e75329d0ef0f2011e.
	schema, err := os.ReadFile("testdata/schema-v32.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(schema)); got != "e89fd064345411d6b8d6947009bda701329112aa8e0f3a8a1b01a4ed0307de9b" {
		t.Fatal("published v32 schema changed", got)
	}
	db := openMigrationTestDB(t, path)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
 INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('s','mock','m',1,1,1),('child','mock','m',1,1,1);
 INSERT INTO session_children(child_session_id,parent_session_id) VALUES('child','s');
 INSERT INTO turns(id,session_id,client_message_id,status,created_at,updated_at) VALUES('t','s','input','completed',1,2);
 INSERT INTO messages(id,session_id,turn_id,role,text,created_at) VALUES('m','s','t','assistant','canonical history',2);
 INSERT INTO session_usage(session_id,last_estimated_input_tokens,cumulative_input_uncached_tokens,updated_at) VALUES('s',123,4567,1);
 INSERT INTO studio_items(id,kind,name,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES
 ('p','doc','Manual title',1,'rev-p','','{}',1,0,1,1),
 ('sheet','table','Sheet',1,'rev-sheet','','{}',1,0,1,1),
 ('w','widget','Widget',1,'rev-w','rev-w','{}',1,0,1,1);
 INSERT INTO studio_item_content(item_id,body,content_hash) VALUES('p',?,?),('sheet',?,?);
 INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind)
 VALUES('p','rev-p','','create-p',1,'',?,?,'user'),('sheet','rev-sheet','','create-sheet',1,'',?,?,'user'),('w','rev-w','','create-w',1,'','<p>Widget</p>','widget-hash','user');
 INSERT INTO studio_item_saves(item_id,client_request_id,hash,request_hash) VALUES('p','create-p','rev-p','original-request');
 INSERT INTO studio_table_ids(item_id,entity_kind,entity_id) VALUES('sheet','row','r'),('sheet','column','c');
 INSERT INTO studio_mounts(session_id,id,item_id,visible,created_at) VALUES('s','mount','p',1,1);
 PRAGMA user_version=32;`, studioLegacyBody, store.ContentHash(studioLegacyBody), studioLegacySheet, store.ContentHash(studioLegacySheet), studioLegacyBody, store.ContentHash(studioLegacyBody), studioLegacySheet, store.ContentHash(studioLegacySheet)); err != nil {
		t.Fatal(err)
	}
	// Build disposable intermediate sources, never read or change an installed DB.
	if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	for _, step := range studioMigrationSteps {
		if step.version > version {
			break
		}
		if err := runSchemaMigration(db, step.version, step.migrate); err != nil {
			t.Fatal(err)
		}
		switch step.version {
		case 33:
			if _, err := db.Exec(`INSERT INTO widget_data VALUES('w',2,'{"keep":true}')`); err != nil {
				t.Fatal(err)
			}
		case 35:
			if _, err := db.Exec(`INSERT INTO widget_pages(id,item_id,scope,revision_hash,target_id,version,data,interaction)
    VALUES('page-w','w','library','rev-w','old-target',3,'{"round":3}','{"participants":[{"id":"caller","sessionID":"s","roles":["black"]}]}')`); err != nil {
				t.Fatal(err)
			}
		case 39:
			if _, err := db.Exec(`INSERT INTO subtask_archives VALUES('child',99)`); err != nil {
				t.Fatal(err)
			}
		}
	}
	if version >= 38 {
		if _, err := db.Exec(`UPDATE session_usage SET last_estimated_input_tokens=456`); err != nil {
			t.Fatal(err)
		}
	}
	if err := setSchemaVersion(db, version); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestStudioMigrationPreservesEachSourceAndRestart(t *testing.T) {
	for source := 32; source <= 47; source++ {
		t.Run(fmt.Sprintf("v%d", source), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pudding.db")
			db := studioLegacyDB(t, path, source)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			st, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if st != nil {
					st.Close()
				}
			}()
			assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
			assertWorkspaceMigrationValue(t, st.db, "PRAGMA foreign_keys", "1")
			assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM pragma_foreign_key_check", "0")
			assertWorkspaceMigrationValue(t, st.db, "SELECT text FROM messages WHERE id='m'", "canonical history")
			assertWorkspaceMigrationValue(t, st.db, "SELECT content FROM studio_item_revisions WHERE item_id='p'", studioLegacyBody)
			assertWorkspaceMigrationValue(t, st.db, "SELECT request_hash FROM studio_item_saves WHERE item_id='p'", "original-request")
			assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM studio_sheet_ids", "2")
			assertWorkspaceMigrationValue(t, st.db, "SELECT item_id FROM studio_mounts WHERE id='mount'", "p")
			assertWorkspaceMigrationValue(t, st.db, "SELECT cumulative_input_uncached_tokens FROM session_usage", "4567")
			estimate := "0"
			if source >= 38 {
				estimate = "456"
			}
			assertWorkspaceMigrationValue(t, st.db, "SELECT last_estimated_input_tokens FROM session_usage", estimate)
			page, err := st.GetPage(context.Background(), "p")
			if err != nil || page.Body != studioLegacyBody || page.ContentHash != store.ContentHash(studioLegacyBody) || page.RevisionID != "rev-p" {
				t.Fatal("Page changed", page, err)
			}
			sheet, err := st.GetSheet(context.Background(), "sheet")
			if err != nil || sheet.ContentHash != store.ContentHash(studioLegacySheet) || sheet.RevisionID != "rev-sheet" {
				t.Fatal("Sheet changed", sheet, err)
			}
			if source >= 33 {
				assertWorkspaceMigrationValue(t, st.db, "SELECT data FROM widget_data WHERE item_id='w'", `{"keep":true}`)
			}
			if source >= 35 {
				assertWorkspaceMigrationValue(t, st.db, "SELECT data FROM widget_pages WHERE id='page-w'", `{"round":3}`)
				assertWorkspaceMigrationValue(t, st.db, "SELECT revision_hash FROM widget_page_pins WHERE item_id='w'", "rev-w")
				assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM widget_pages,json_each(interaction,'$.participants') WHERE json_type(value,'$.roles') IS NOT NULL", "0")
			}
			if source >= 39 {
				assertWorkspaceMigrationValue(t, st.db, "SELECT archived_at FROM subtask_archives WHERE child_session_id='child'", "99")
			}
			backups := migrationBackupFiles(t, path)
			want := 1
			if source == currentSchemaVersion {
				want = 0
			}
			if len(backups) != want {
				t.Fatalf("backup count=%d want=%d", len(backups), want)
			}
			if err := st.Close(); err != nil {
				t.Fatal(err)
			}
			st, err = Open(path)
			if err != nil {
				t.Fatal("restart", err)
			}
			if got := migrationBackupFiles(t, path); len(got) != want {
				t.Fatal("restart repeated backup", got)
			}
			assertWorkspaceMigrationValue(t, st.db, "SELECT text FROM messages WHERE id='m'", "canonical history")
		})
	}
}

func TestStudioMigrationRollsBackWholeBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := studioLegacyDB(t, path, 32)
	sentinel := errors.New("after final transformation")
	err := runSchemaMigration(db, 47, func(tx *sql.Tx) error {
		if err := migrateStudioV47(tx); err != nil {
			return err
		}
		var version int
		if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
			return err
		}
		if version != 32 {
			t.Fatalf("intermediate version was committed: %d", version)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	assertStudioMigrationRolledBack(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if err != nil {
		t.Fatal("retry after rollback", err)
	}
	defer st.Close()
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
}

func TestStudioMigrationRejectsForeignKeyViolationBeforeCommit(t *testing.T) {
	db := studioLegacyDB(t, filepath.Join(t.TempDir(), "pudding.db"), 32)
	err := runSchemaMigration(db, 47, func(tx *sql.Tx) error {
		if err := migrateStudioV47(tx); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO widget_data VALUES('missing-item',1,'{}')`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "foreign key violation") {
		t.Fatal(err)
	}
	assertStudioMigrationRolledBack(t, db)
}

func assertStudioMigrationRolledBack(t *testing.T, db *sql.DB) {
	t.Helper()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "32")
	assertWorkspaceMigrationValue(t, db, "PRAGMA foreign_keys", "1")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name IN ('widget_data','widget_pages','widget_page_pins','studio_sheet_ids','studio_document_edits')", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT kind FROM studio_items WHERE id='p'", "doc")
	assertWorkspaceMigrationValue(t, db, "SELECT body FROM studio_item_content WHERE item_id='p'", studioLegacyBody)
	assertWorkspaceMigrationValue(t, db, "SELECT last_estimated_input_tokens FROM session_usage", "123")
}

func TestStudioMigrationValidatesSchemaBeforeCommit(t *testing.T) {
	db := studioLegacyDB(t, filepath.Join(t.TempDir(), "pudding.db"), 32)
	last := len(studioMigrationSteps) - 1
	original := studioMigrationSteps[last].migrate
	t.Cleanup(func() { studioMigrationSteps[last].migrate = original })
	studioMigrationSteps[last].migrate = func(tx *sql.Tx) error {
		if err := original(tx); err != nil {
			return err
		}
		_, err := tx.Exec(`DROP INDEX messages_session_created`)
		return err
	}
	err := runSchemaMigration(db, 47, migrateStudioV47)
	if err == nil || !strings.Contains(err.Error(), "missing index messages_session_created") {
		t.Fatal(err)
	}
	assertStudioMigrationRolledBack(t, db)
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name='messages_session_created'", "1")
}

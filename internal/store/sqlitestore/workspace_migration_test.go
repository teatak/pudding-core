package sqlitestore

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fixture is the exact schema.sql from the published v0.2.11 tag.
func openWorkspaceV13Database(t *testing.T) (*sql.DB, string) {
	t.Helper()
	schema, err := os.ReadFile("testdata/schema-v13.sql")
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(schema)); got != "19ae784ea197314f139a5836cd172a6d3c2ca2e3c0f717d69eccf595d25fb99f" {
		t.Fatal("published v13 fixture changed")
	}
	path := filepath.Join(t.TempDir(), "pudding.db")
	db := openMigrationTestDB(t, path)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
INSERT INTO projects(id,name,created_at,updated_at) VALUES('p','Keep project',1,2);
INSERT INTO sessions(id,title,provider,model,project_id,created_at,updated_at,last_activity_at) VALUES('s','Keep session','mock','mock','p',1,2,3);
INSERT INTO turns(id,session_id,client_message_id,status,created_at,updated_at) VALUES('t','s','cm','completed',1,2);
INSERT INTO messages(id,session_id,turn_id,role,text,created_at) VALUES('m','s','t','assistant','Keep message',2);
INSERT INTO computer_app_grants VALUES('s','com.apple.calculator',3);
INSERT INTO browser_history VALUES('h','https://example.com/','Keep history','',3,1,3);
INSERT INTO session_browser_tabs VALUES('s','b','https://example.com/','Keep tab','','',1,3);
INSERT INTO canvas_saved_items(id,source_session_id,source_item_id,kind,title,item_json,revision,created_at,updated_at) VALUES('saved','s','current','markdown','Saved version','{"content":"saved"}',3,1,2);
INSERT INTO canvas_items(session_id,id,kind,title,item_json,source_saved_item_id,base_saved_revision,saved_dirty,created_at,updated_at) VALUES('s','current','markdown','Working version','{"content":"working"}','saved',3,1,1,4);
INSERT INTO canvas_closed_items(session_id,id,source_item_id,actor_session_id,kind,title,item_json,closed_at,created_at,updated_at) VALUES('s','closed','closed-item','s','markdown','Closed version','{"content":"closed"}',5,1,4);
PRAGMA user_version=13;
`); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func assertWorkspaceMigrationValue(t *testing.T, db *sql.DB, query, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(query).Scan(&got); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	if got != want {
		t.Fatalf("%s: got %q, want %q", query, got, want)
	}
}

func TestWorkspaceMigrationFromPublishedV13(t *testing.T) {
	db, path := openWorkspaceV13Database(t)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range [][2]string{
			{"PRAGMA user_version", fmt.Sprint(currentSchemaVersion)},
			{"SELECT name FROM projects WHERE id='p'", "Keep project"},
			{"SELECT title FROM sessions WHERE id='s'", "Keep session"},
			{"SELECT status FROM turns WHERE id='t'", "completed"},
			{"SELECT text FROM messages WHERE id='m'", "Keep message"},
			{"SELECT app_id FROM computer_app_grants WHERE session_id='s'", "com.apple.calculator"},
			{"SELECT title FROM browser_history WHERE id='h'", "Keep history"},
			{"SELECT title FROM session_browser_tabs WHERE tab_id='b'", "Keep tab"},
			{"SELECT item_json||':'||source_saved_item_id||':'||base_saved_revision||':'||saved_dirty FROM canvas_items WHERE id='current'", `{"content":"working"}:saved:3:1`},
			{"SELECT item_json||':'||revision FROM canvas_saved_items WHERE id='saved'", `{"content":"saved"}:3`},
			{"SELECT item_json||':'||visible FROM canvas_items WHERE id='closed-item'", `{"content":"closed"}:0`},
			{"SELECT COUNT(*) FROM library_favorites WHERE saved_item_id='saved'", "1"},
			{"SELECT COUNT(*) FROM sqlite_master WHERE name='canvas_closed_items'", "0"},
			{"SELECT COUNT(*) FROM library_recent_opens", "0"},
			{"SELECT COUNT(*) FROM pragma_table_info('library_favorites') WHERE name IN ('root_path','path')", "0"},
			{"PRAGMA integrity_check", "ok"},
			{"SELECT COUNT(*) FROM pragma_foreign_key_check", "0"},
		} {
			assertWorkspaceMigrationValue(t, st.db, check[0], check[1])
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
	backups := migrationBackupFiles(t, path)
	if len(backups) != 1 || !strings.Contains(backups[0], ".backup-v13-") {
		t.Fatalf("expected one pre-v17 backup: %v", backups)
	}
	backup := openMigrationTestDB(t, backups[0])
	defer backup.Close()
	assertWorkspaceMigrationValue(t, backup, "PRAGMA user_version", "13")
	assertWorkspaceMigrationValue(t, backup, "SELECT item_json FROM canvas_closed_items WHERE id='closed'", `{"content":"closed"}`)
}

func TestWorkspaceMigrationFailureRollsBackWholeUpgrade(t *testing.T) {
	db, path := openWorkspaceV13Database(t)
	// Fail late, after closed-content retention and favorite creation have run.
	if _, err := db.Exec(`CREATE TABLE library_recent_opens (invalid_column TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := Open(path)
	if st != nil {
		st.Close()
	}
	if err == nil {
		t.Fatal("expected a migration failure")
	}
	db = openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "13")
	assertWorkspaceMigrationValue(t, db, "SELECT item_json FROM canvas_closed_items WHERE id='closed'", `{"content":"closed"}`)
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM canvas_items WHERE id='closed-item'", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT COUNT(*) FROM sqlite_master WHERE name='library_favorites'", "0")
	if _, err := db.Exec(`DROP TABLE library_recent_opens`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
	assertWorkspaceMigrationValue(t, st.db, "SELECT COUNT(*) FROM canvas_items WHERE id='closed-item'", "1")
}

func TestWorkspaceMigrationRejectsUnregisteredVersionsWithoutChanges(t *testing.T) {
	for _, version := range []int{14, 15, 16} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			db, path := openWorkspaceV13Database(t)
			if err := setSchemaVersion(db, version); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			st, err := Open(path)
			if st != nil {
				st.Close()
			}
			if !errors.Is(err, ErrUnsupportedSchema) {
				t.Fatalf("Open error = %v, want ErrUnsupportedSchema", err)
			}
			db = openMigrationTestDB(t, path)
			defer db.Close()
			assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", fmt.Sprint(version))
			assertWorkspaceMigrationValue(t, db, "SELECT item_json FROM canvas_closed_items WHERE id='closed'", `{"content":"closed"}`)
			if backups := migrationBackupFiles(t, path); len(backups) != 0 {
				t.Fatalf("unsupported database was migrated: %v", backups)
			}
		})
	}
}

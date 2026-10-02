package sqlitestore

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/widget"
)

const studioFixtureManifest = `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{"primary":{"%s":"github","endpoint":"github_rest"}},"operations":{"items":{"source":"primary","kind":"rest","effectHint":"read","inputSchema":{"type":"object","properties":{},"additionalProperties":false},"request":{"method":"GET","path":"/items"}}}}`

func studioFixtureFiles(version, manifestField, sdk string) map[string]string {
	return map[string]string{
		manifestFile(manifestField): fmt.Sprintf(studioFixtureManifest, manifestField),
		"src/App.tsx":               "import { useQuery } from '" + sdk + "';\nexport default function App() { return <p>" + version + "</p>; }\n",
		"assets/icon.svg":           `<svg xmlns="http://www.w3.org/2000/svg"/>`,
	}
}

func manifestFile(field string) string {
	if field == "appID" {
		return "canvas.json"
	}
	return widget.ManifestFile
}

// studioV26Fixture writes a released v26 database and its <home>/canvases source.
func studioV26Fixture(t *testing.T) (path string, oldA, oldB string) {
	t.Helper()
	home := t.TempDir()
	path = filepath.Join(home, "pudding.db")
	parent := filepath.Join(home, "canvases", "canvas_w", "revisions")
	a := studioFixtureFiles("v1", "appID", "@pudding/canvas")
	b := studioFixtureFiles("v2", "appID", "@pudding/canvas")
	oldA, oldB = widget.PackageHash(a), widget.PackageHash(b)
	for hash, files := range map[string]map[string]string{oldA: a, oldB: b} {
		if err := widget.WriteVersion(parent, hash, files); err != nil {
			t.Fatal(err)
		}
	}
	draft := map[string]string{"canvas.json": `{"sources":{"primary":{"appID":"github"`, "src/App.tsx": "import { pudding } from '@pudding/canvas';\n"}
	if err := widget.InstallDraft(filepath.Join(home, "canvases", "canvas_w", "draft"), oldB, draft); err != nil {
		t.Fatal(err)
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	schema, err := os.ReadFile("testdata/schema-v26.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	spec := `{"revisionHash":"` + oldA + `","resourceRevision":3,"bindingVersion":2,"operationID":"items","operationHash":"op","bindingFingerprint":"fp","appID":"github","connectionID":"conn_1","description":"List","params":{"state":"open"},"request":{"method":"GET"}}`
	if _, err := db.Exec(`
 INSERT INTO sessions(id,provider,model,loaded_app_ids,created_at,updated_at,last_activity_at) VALUES('s1','mock','m','["app-authoring","canvas","github"]',1,1,1);
 INSERT INTO canvas_resources(id,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
  VALUES('canvas_w','Board','chart','blue','s1',3,?,?,'{"primary":"conn_1"}',2,0,1,2);
 INSERT INTO canvas_revisions(canvas_id,hash,parent_revision,client_request_id,created_at,build_receipt) VALUES('canvas_w',?,'','req-a',1,'{"ok":true}'),('canvas_w',?,?,'req-b',2,'');
 INSERT INTO canvas_saves(canvas_id,client_request_id,hash) VALUES('canvas_w','req-a',?),('canvas_w','req-b',?);
 INSERT INTO canvas_mounts(session_id,id,resource_id,visible,created_at) VALUES('s1','mount_1','canvas_w',1,1);
 INSERT INTO library_favorites(id,kind,source_session_id,saved_item_id,url,title,created_at) VALUES('canvas:canvas_w','canvas','','canvas_w','','',3);
 INSERT INTO library_recent_opens(id,kind,source_session_id,canvas_item_id,root_path,path,opened_at) VALUES('recent_1','canvas','s1','mount_1','','',4);
 INSERT INTO canvas_actions(id,canvas_id,client_request_id,request_hash,state,spec,result,created_at) VALUES('action_1','canvas_w','client-1','intent','succeeded',?,'{"ok":true}',5);
 INSERT INTO canvas_links(id,canvas_id,left_entity,right_entity,created_at) VALUES('link_1','canvas_w','{"appID":"github","connectionID":"conn_1","entityType":"issue","entityID":"1"}','{"appID":"gmail","connectionID":"conn_2","entityType":"message","entityID":"m1"}',6);
 PRAGMA user_version=26;`, oldB, oldA, oldA, oldB, oldA, oldA, oldB, spec); err != nil {
		t.Fatal(err)
	}
	return path, oldA, oldB
}

func TestStudioMigrationMovesWidgetsAndRenamesPlugins(t *testing.T) {
	path, oldA, _ := studioV26Fixture(t)
	home := filepath.Dir(path)
	newA := widget.PackageHash(studioFixtureFiles("v1", "pluginID", "@pudding/widget"))
	newB := widget.PackageHash(studioFixtureFiles("v2", "pluginID", "@pudding/widget"))
	for range 2 {
		st, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range [][2]string{
			{"PRAGMA user_version", fmt.Sprint(currentSchemaVersion)},
			{"SELECT kind||'|'||name||'|'||icon||'|'||head_revision||'|'||active_revision||'|'||bindings FROM studio_items WHERE id='canvas_w'", "widget|Board|chart|" + newB + "|" + newA + `|{"primary":"conn_1"}`},
			{"SELECT group_concat(hash||'<'||parent_revision, ',') FROM (SELECT * FROM studio_item_revisions ORDER BY created_at)", newA + "<," + newB + "<" + newA},
			{"SELECT count(*) FROM studio_item_revisions WHERE build_receipt<>''", "0"},
			{"SELECT group_concat(client_request_id||'='||hash, ',') FROM (SELECT * FROM studio_item_saves ORDER BY client_request_id)", "req-a=" + newA + ",req-b=" + newB},
			{"SELECT item_id FROM studio_mounts WHERE session_id='s1' AND id='mount_1'", "canvas_w"},
			{"SELECT id||'|'||kind||'|'||saved_item_id FROM library_favorites", "studio:canvas_w|studio|canvas_w"},
			{"SELECT kind||'|'||studio_mount_id FROM library_recent_opens", "studio|mount_1"},
			{"SELECT loaded_plugin_ids FROM sessions WHERE id='s1'", `["github","plugin-authoring","widget-authoring"]`},
			{"SELECT count(*) FROM sqlite_master WHERE name LIKE 'canvas_%'", "0"},
			{"SELECT count(*) FROM pragma_foreign_key_check", "0"},
			{"PRAGMA integrity_check", "ok"},
		} {
			assertWorkspaceMigrationValue(t, st.db, check[0], check[1])
		}
		var spec, left, right string
		if err := st.db.QueryRow(`SELECT spec FROM widget_actions WHERE id='action_1'`).Scan(&spec); err != nil {
			t.Fatal(err)
		}
		if err := st.db.QueryRow(`SELECT left_entity,right_entity FROM widget_links WHERE id='link_1'`).Scan(&left, &right); err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal([]byte(spec), &decoded); err != nil || decoded["pluginID"] != "github" || decoded["appID"] != nil || decoded["revisionHash"] != oldA {
			t.Fatalf("action spec: %s %v", spec, err)
		}
		if left != `{"pluginID":"github","connectionID":"conn_1","entityType":"issue","entityID":"1"}` || right != `{"pluginID":"gmail","connectionID":"conn_2","entityType":"message","entityID":"m1"}` {
			t.Fatalf("link entities: %s %s", left, right)
		}
		for hash, version := range map[string]string{newA: "v1", newB: "v2"} {
			p, err := widget.ReadPackage(home, "canvas_w", hash)
			if err != nil || !reflect.DeepEqual(p.Files, studioFixtureFiles(version, "pluginID", "@pudding/widget")) {
				t.Fatalf("converted package %s: %+v %v", hash, p.Files, err)
			}
		}
		draft, err := widget.ReadDraft(home, "canvas_w")
		if err != nil || draft.BaseRevisionHash != newB || draft.Files[widget.ManifestFile] != `{"sources":{"primary":{"pluginID":"github"` || draft.Files["src/App.tsx"] != "import { pudding } from '@pudding/widget';\n" {
			t.Fatalf("converted draft: %+v %v", draft, err)
		}
		mounts, err := st.ListStudioMounts(context.Background(), "s1")
		if err != nil || len(mounts) != 1 || mounts[0].ItemID != "canvas_w" || mounts[0].Kind != "widget" {
			t.Fatalf("mounts: %+v %v", mounts, err)
		}
		if err := st.Close(); err != nil {
			t.Fatal(err)
		}
	}
	// The source of the pre-upgrade database backup stays readable.
	if _, err := os.Stat(filepath.Join(home, "canvases", "canvas_w", "revisions", oldA, "canvas.json")); err != nil {
		t.Fatal(err)
	}
}

func TestStudioMigrationFailureRollsBackAndRetries(t *testing.T) {
	path, oldA, _ := studioV26Fixture(t)
	db := openMigrationTestDB(t, path)
	if _, err := db.Exec(`UPDATE canvas_actions SET spec='not json'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("invalid action spec did not fail the upgrade")
	}
	db = openMigrationTestDB(t, path)
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "26")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM canvas_revisions", "2")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM sqlite_master WHERE name='studio_items'", "0")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM pragma_table_info('sessions') WHERE name='loaded_app_ids'", "1")
	if _, err := db.Exec(`UPDATE canvas_actions SET spec='{"appID":"github"}'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	// The packages installed by the failed attempt are reused.
	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	assertWorkspaceMigrationValue(t, st.db, "PRAGMA user_version", fmt.Sprint(currentSchemaVersion))
	assertWorkspaceMigrationValue(t, st.db, "SELECT count(*) FROM studio_item_revisions", "2")
	if strings.Contains(mustQueryString(t, st, `SELECT spec FROM widget_actions`), "appID") {
		t.Fatal("action spec kept appID")
	}
	if mustQueryString(t, st, `SELECT active_revision FROM studio_items`) == oldA {
		t.Fatal("active revision kept its v26 hash")
	}
}

func TestStudioMigrationRejectsChangedSource(t *testing.T) {
	path, oldA, _ := studioV26Fixture(t)
	file := filepath.Join(filepath.Dir(path), "canvases", "canvas_w", "revisions", oldA, "src", "App.tsx")
	if err := os.WriteFile(file, []byte("export default function App() { return null; }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if st, err := Open(path); err == nil {
		st.Close()
		t.Fatal("a changed v26 package did not fail the upgrade")
	}
	db := openMigrationTestDB(t, path)
	defer db.Close()
	assertWorkspaceMigrationValue(t, db, "PRAGMA user_version", "26")
	assertWorkspaceMigrationValue(t, db, "SELECT count(*) FROM canvas_resources", "1")
}

func mustQueryString(t *testing.T, st *Store, query string) string {
	t.Helper()
	var out string
	if err := st.db.QueryRow(query).Scan(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

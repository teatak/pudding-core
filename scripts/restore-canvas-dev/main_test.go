package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/store/sqlitestore"
)

func TestRestoreIsAtomicAndDoesNotOverwriteEditedCanvas(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(home, "data", "pudding.db")
	st, err := sqlitestore.OpenWithHome(dbPath, home)
	if err != nil {
		t.Fatal(err)
	}
	st.Close()
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO sessions(id,provider,model,created_at,updated_at,last_activity_at) VALUES('s','mock','m',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), "snapshots")
	dir := filepath.Join(root, "one")
	os.MkdirAll(dir, 0700)
	snap := snapshot{ID: "saved", Name: "Name", SourceSessionID: "s", HeadRevision: "old", ActiveRevision: "old", Revision: 2, CreatedAt: 1, UpdatedAt: 2,
		Revisions: []revision{{Hash: "old", ClientRequestID: "migration-v25", CreatedAt: 2, Content: store.CanvasContent{Kind: "markdown", Title: "Name", Item: json.RawMessage(`{"content":"Retained text"}`)}}},
		Mounts:    []map[string]any{{"session_id": "missing", "id": "mount", "visible": 1, "created_at": 1}},
	}
	write := func() {
		raw, _ := json.Marshal(snap)
		if err := os.WriteFile(filepath.Join(dir, "original.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		m, _ := json.Marshal(map[string]string{"snapshotHash": fmt.Sprintf("%x", sha256.Sum256(raw))})
		os.WriteFile(filepath.Join(dir, "manifest.json"), m, 0600)
	}
	write()
	if err = restore(home, root, home); err == nil {
		t.Fatal("missing parent session must fail")
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM canvas_resources`).Scan(&count)
	if count != 0 {
		t.Fatal("partial restore persisted")
	}
	snap.Mounts[0]["session_id"] = "s"
	write()
	if err = restore(home, root, home); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE canvas_resources SET name='Edited later' WHERE id='saved'`); err != nil {
		t.Fatal(err)
	}
	if err = restore(home, root, home); err != nil {
		t.Fatal(err)
	}
	var name string
	db.QueryRow(`SELECT name FROM canvas_resources WHERE id='saved'`).Scan(&name)
	if name != "Edited later" {
		t.Fatal("recovery overwrote later edits")
	}
	db.QueryRow(`SELECT count(*) FROM canvas_mounts`).Scan(&count)
	if count != 1 {
		t.Fatal("duplicate mount", count)
	}
}

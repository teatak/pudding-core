package sqlitestore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/teatak/pudding-core/internal/store"
)

const canvasMountSchema = `CREATE TABLE canvas_mounts (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 id TEXT NOT NULL,
 resource_id TEXT NOT NULL REFERENCES canvas_resources(id) ON DELETE CASCADE,
 window_json TEXT NOT NULL DEFAULT '',
 visible INTEGER NOT NULL DEFAULT 1,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(session_id,id),
 UNIQUE(session_id,resource_id)
);`

// v24 is the shipped release layout. Build and retire the old structured canvas
// data in one transaction, leaving only the final source-canvas schema.
func migrateFinalCanvases(tx *sql.Tx, archiveHome string) error {
	if _, err := tx.Exec(`
CREATE TABLE canvas_resources (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 source_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
 revision INTEGER NOT NULL,
 head_revision TEXT NOT NULL,
 active_revision TEXT NOT NULL,
 bindings TEXT NOT NULL,
 grants TEXT NOT NULL,
 binding_version INTEGER NOT NULL,
 deleted INTEGER NOT NULL,
 created_at INTEGER NOT NULL,
 updated_at INTEGER NOT NULL
);
CREATE TABLE canvas_revisions (
 canvas_id TEXT NOT NULL REFERENCES canvas_resources(id) ON DELETE CASCADE,
 hash TEXT NOT NULL,
 parent_revision TEXT NOT NULL,
 client_request_id TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 build_receipt TEXT NOT NULL,
 PRIMARY KEY(canvas_id,hash),
 UNIQUE(canvas_id,client_request_id)
);
CREATE TABLE canvas_saves (
 canvas_id TEXT NOT NULL REFERENCES canvas_resources(id) ON DELETE CASCADE,
 client_request_id TEXT NOT NULL,
 hash TEXT NOT NULL,
 PRIMARY KEY(canvas_id,client_request_id)
);
CREATE TABLE canvas_actions (
 id TEXT PRIMARY KEY,
 canvas_id TEXT NOT NULL REFERENCES canvas_resources(id) ON DELETE CASCADE,
 client_request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('prepared','executing','succeeded','failed','unknown')),
 spec TEXT NOT NULL,
 result TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 UNIQUE(canvas_id,client_request_id)
);
CREATE TABLE canvas_links (
 id TEXT PRIMARY KEY,
 canvas_id TEXT NOT NULL REFERENCES canvas_resources(id) ON DELETE CASCADE,
 left_entity TEXT NOT NULL,
 right_entity TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 UNIQUE(canvas_id,left_entity,right_entity)
);`); err != nil {
		return err
	}
	if err := migrateUnifiedCanvases(tx); err != nil {
		return err
	}
	if err := archiveLegacyCanvases(tx, archiveHome); err != nil {
		return err
	}
	if err := retireLegacyCanvasSchema(tx); err != nil {
		return err
	}
	_, err := tx.Exec(`ALTER TABLE canvas_resources DROP COLUMN grants`)
	return err
}

// One transaction preserves resources, dirty working copies, mounts, favorites and
// recent-open references. Immutable App packages and their hashes stay in place.
func migrateUnifiedCanvases(tx *sql.Tx) error {
	if _, err := tx.Exec(`
 ALTER TABLE canvas_revisions ADD COLUMN content_json TEXT NOT NULL DEFAULT '';
 ` + canvasMountSchema); err != nil {
		return err
	}
	type legacy struct {
		id, session, kind, title, item, window, saved string
		dirty                                         bool
		visible                                       bool
		revision, created, updated                    int64
	}
	read := func(query string, saved bool) ([]legacy, error) {
		rows, err := tx.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := []legacy{}
		for rows.Next() {
			var v legacy
			if saved {
				err = rows.Scan(&v.id, &v.session, &v.kind, &v.title, &v.item, &v.window, &v.revision, &v.created, &v.updated)
			} else {
				err = rows.Scan(&v.id, &v.session, &v.kind, &v.title, &v.item, &v.window, &v.saved, &v.dirty, &v.visible, &v.created, &v.updated)
			}
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, rows.Err()
	}
	saved, err := read(`SELECT id,source_session_id,kind,title,item_json,window_json,revision,created_at,updated_at FROM canvas_saved_items`, true)
	if err != nil {
		return err
	}
	mounts, err := read(`SELECT id,session_id,kind,title,item_json,window_json,source_saved_item_id,saved_dirty,visible,created_at,updated_at FROM canvas_items`, false)
	if err != nil {
		return err
	}
	resources := map[string]bool{}
	insert := func(id string, v legacy) error {
		// Preserve historical payloads exactly; new edits validate supported renderers.
		b, err := json.Marshal(store.CanvasContent{Kind: v.kind, Title: v.title, Item: json.RawMessage(v.item)})
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		hash := hex.EncodeToString(sum[:])
		revision := v.revision
		if revision < 1 {
			revision = 1
		}
		_, err = tx.Exec(`INSERT INTO canvas_resources(id,name,source_session_id,revision,head_revision,active_revision,bindings,grants,binding_version,deleted,created_at,updated_at) VALUES(?,?,(SELECT id FROM sessions WHERE id=?),?,?,?,'{}','{}',1,0,?,?)`, id, v.title, v.session, revision, hash, hash, v.created, v.updated)
		if err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO canvas_revisions(canvas_id,hash,parent_revision,client_request_id,created_at,build_receipt,content_json) VALUES(?,?,'','migration-v26',?,'',?)`, id, hash, v.updated, string(b))
		return err
	}
	for _, v := range saved {
		if err := insert(v.id, v); err != nil {
			return err
		}
		resources[v.id] = true
	}
	for _, v := range mounts {
		id := v.saved
		if !resources[id] || v.dirty {
			id = store.CanvasResourceID(v.session, v.id)
			if v.dirty && resources[v.saved] {
				v.title += " · Recovered changes"
			}
			if err := insert(id, v); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(`INSERT INTO canvas_mounts(session_id,id,resource_id,window_json,visible,created_at) VALUES(?,?,?,?,?,?)`, v.session, v.id, id, v.window, v.visible, v.created); err != nil {
			return err
		}
	}
	// Rebuild foreign keys before removing the old content tables.
	_, err = tx.Exec(`
 ALTER TABLE library_favorites RENAME TO library_favorites_v25;
 DROP INDEX library_favorites_canvas;
 DROP INDEX library_favorites_web;
 CREATE TABLE library_favorites (
 id TEXT PRIMARY KEY,
 kind TEXT NOT NULL CHECK(kind IN ('canvas','web')),
 source_session_id TEXT NOT NULL DEFAULT '',
 saved_item_id TEXT REFERENCES canvas_resources(id) ON DELETE CASCADE,
 url TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', created_at INTEGER NOT NULL,
 CHECK ((kind='canvas' AND saved_item_id IS NOT NULL AND url='') OR (kind='web' AND saved_item_id IS NULL AND url<>''))
 );
 INSERT INTO library_favorites SELECT * FROM library_favorites_v25;
 DROP TABLE library_favorites_v25;
 CREATE UNIQUE INDEX library_favorites_canvas ON library_favorites(saved_item_id) WHERE kind='canvas';
 CREATE UNIQUE INDEX library_favorites_web ON library_favorites(url) WHERE kind='web';
 ALTER TABLE library_recent_opens RENAME TO library_recent_opens_v25;
 DROP INDEX library_recent_canvas;
 DROP INDEX library_recent_file;
 DROP INDEX library_recent_opened;
 CREATE TABLE library_recent_opens (
 id TEXT PRIMARY KEY, kind TEXT NOT NULL CHECK(kind IN ('file','canvas')),
 source_session_id TEXT NOT NULL, canvas_item_id TEXT, root_path TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', opened_at INTEGER NOT NULL,
 FOREIGN KEY(source_session_id,canvas_item_id) REFERENCES canvas_mounts(session_id,id) ON DELETE CASCADE,
 CHECK ((kind='canvas' AND canvas_item_id IS NOT NULL AND root_path='' AND path='') OR (kind='file' AND canvas_item_id IS NULL AND root_path<>'' AND path<>''))
 );
 INSERT INTO library_recent_opens SELECT * FROM library_recent_opens_v25;
 DROP TABLE library_recent_opens_v25;
 CREATE UNIQUE INDEX library_recent_canvas ON library_recent_opens(source_session_id,canvas_item_id) WHERE kind='canvas';
 CREATE UNIQUE INDEX library_recent_file ON library_recent_opens(root_path,path) WHERE kind='file';
 CREATE INDEX library_recent_opened ON library_recent_opens(opened_at DESC,id DESC);
 DROP TABLE canvas_items;
 DROP TABLE canvas_saved_items;
 `)
	return err
}

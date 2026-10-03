package sqlitestore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

// Schema v30 upgrades v26 canvases and Apps to Studio and plugins, including
// native documents, tables and archives in the same database transaction.
//
// Widget source moves from <home>/canvases to <home>/studio with the widget
// manifest name, plugin source fields and SDK import, so every content hash
// changes and stored references are remapped. New packages are installed before
// the SQL commit and are reused on retry. <home>/canvases is left in place: it is
// the source of the database backup taken before this upgrade.
const studioSchemaV30 = `
CREATE TABLE studio_items (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('doc','table','widget')),
    name TEXT NOT NULL,
    icon TEXT NOT NULL DEFAULT '',
    icon_color TEXT NOT NULL DEFAULT '',
    source_session_id TEXT REFERENCES sessions(id) ON DELETE SET NULL,
    revision INTEGER NOT NULL,
    head_revision TEXT NOT NULL,
    active_revision TEXT NOT NULL,
    bindings TEXT NOT NULL,
    binding_version INTEGER NOT NULL,
    deleted INTEGER NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    archived_at INTEGER NOT NULL DEFAULT 0
);
INSERT INTO studio_items(id,kind,name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at)
 SELECT id,'widget',name,icon,icon_color,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at FROM canvas_resources;
CREATE TABLE studio_item_revisions (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    hash TEXT NOT NULL,
    parent_revision TEXT NOT NULL,
    client_request_id TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    build_receipt TEXT NOT NULL,
    content TEXT,
    content_hash TEXT NOT NULL DEFAULT '',
    author_kind TEXT NOT NULL DEFAULT '',
    author_session_id TEXT NOT NULL DEFAULT '',
    author_turn_id TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(item_id, hash),
    UNIQUE(item_id, client_request_id)
);
INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt)
 SELECT canvas_id,hash,parent_revision,client_request_id,created_at,'' FROM canvas_revisions;
CREATE TABLE studio_item_saves (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    client_request_id TEXT NOT NULL,
    hash TEXT NOT NULL,
    request_hash TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(item_id, client_request_id)
);
INSERT INTO studio_item_saves(item_id,client_request_id,hash) SELECT canvas_id,client_request_id,hash FROM canvas_saves;
CREATE TABLE studio_item_content (
    item_id TEXT PRIMARY KEY REFERENCES studio_items(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    content_hash TEXT NOT NULL
);
CREATE TABLE studio_table_ids (
    item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
    entity_kind TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    PRIMARY KEY (item_id, entity_kind, entity_id)
);
CREATE TABLE widget_actions (
 id TEXT PRIMARY KEY,
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 client_request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('prepared','executing','succeeded','failed','unknown')),
 spec TEXT NOT NULL,
 result TEXT NOT NULL DEFAULT '',
 created_at INTEGER NOT NULL,
 UNIQUE(item_id,client_request_id)
);
INSERT INTO widget_actions(id,item_id,client_request_id,request_hash,state,spec,result,created_at)
 SELECT id,canvas_id,client_request_id,request_hash,state,spec,result,created_at FROM canvas_actions;
CREATE TABLE widget_links (
 id TEXT PRIMARY KEY,
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 left_entity TEXT NOT NULL,
 right_entity TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 UNIQUE(item_id,left_entity,right_entity)
);
INSERT INTO widget_links(id,item_id,left_entity,right_entity,created_at) SELECT id,canvas_id,left_entity,right_entity,created_at FROM canvas_links;
CREATE TABLE studio_mounts (
 session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 id TEXT NOT NULL,
 item_id TEXT NOT NULL REFERENCES studio_items(id) ON DELETE CASCADE,
 visible INTEGER NOT NULL DEFAULT 1,
 created_at INTEGER NOT NULL,
 PRIMARY KEY(session_id,id),
 UNIQUE(session_id,item_id)
);
INSERT INTO studio_mounts(session_id,id,item_id,visible,created_at) SELECT session_id,id,resource_id,visible,created_at FROM canvas_mounts;
ALTER TABLE library_recent_opens RENAME TO library_recent_opens_v26;
CREATE TABLE library_recent_opens (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('file','studio')),
    source_session_id TEXT NOT NULL,
    studio_mount_id TEXT,
    root_path TEXT NOT NULL DEFAULT '',
    path TEXT NOT NULL DEFAULT '',
    opened_at INTEGER NOT NULL,
    FOREIGN KEY(source_session_id,studio_mount_id) REFERENCES studio_mounts(session_id,id) ON DELETE CASCADE,
    CHECK ((kind='studio' AND studio_mount_id IS NOT NULL AND root_path='' AND path='')
        OR (kind='file' AND studio_mount_id IS NULL AND root_path<>'' AND path<>''))
);
INSERT INTO library_recent_opens(id,kind,source_session_id,studio_mount_id,root_path,path,opened_at)
 SELECT id,CASE kind WHEN 'canvas' THEN 'studio' ELSE kind END,source_session_id,canvas_item_id,root_path,path,opened_at FROM library_recent_opens_v26;
DROP TABLE library_recent_opens_v26;
ALTER TABLE library_favorites RENAME TO library_favorites_v26;
CREATE TABLE library_favorites (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL CHECK(kind IN ('studio','web')),
    source_session_id TEXT NOT NULL DEFAULT '',
    saved_item_id TEXT REFERENCES studio_items(id) ON DELETE CASCADE,
    url TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    CHECK ((kind='studio' AND saved_item_id IS NOT NULL AND url='')
        OR (kind='web' AND saved_item_id IS NULL AND url<>''))
);
INSERT INTO library_favorites(id,kind,source_session_id,saved_item_id,url,title,created_at)
 SELECT CASE kind WHEN 'canvas' THEN 'studio:'||saved_item_id ELSE id END,CASE kind WHEN 'canvas' THEN 'studio' ELSE kind END,source_session_id,saved_item_id,url,title,created_at FROM library_favorites_v26;
DROP TABLE library_favorites_v26;
DROP TABLE canvas_links;
DROP TABLE canvas_actions;
DROP TABLE canvas_saves;
DROP TABLE canvas_revisions;
DROP TABLE canvas_mounts;
DROP TABLE canvas_resources;
CREATE UNIQUE INDEX library_favorites_studio ON library_favorites(saved_item_id) WHERE kind='studio';
CREATE UNIQUE INDEX library_favorites_web ON library_favorites(url) WHERE kind='web';
CREATE UNIQUE INDEX library_recent_studio ON library_recent_opens(source_session_id,studio_mount_id) WHERE kind='studio';
CREATE UNIQUE INDEX library_recent_file ON library_recent_opens(root_path,path) WHERE kind='file';
CREATE INDEX library_recent_opened ON library_recent_opens(opened_at DESC,id DESC);
ALTER TABLE sessions RENAME COLUMN loaded_app_ids TO loaded_plugin_ids;
CREATE INDEX studio_items_archived_at ON studio_items(archived_at);
`

// Built-in plugin IDs renamed with the App concept.
var renamedPluginIDs = map[string]string{"app-authoring": "plugin-authoring", "canvas": "widget-authoring"}

func migrateStudioAndPlugins(tx *sql.Tx, home string) error {
	moved, err := moveWidgetSources(tx, home)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(studioSchemaV30); err != nil {
		return err
	}
	if err := remapWidgetHashes(tx, moved); err != nil {
		return err
	}
	if err := renameWidgetPluginFields(tx); err != nil {
		return err
	}
	return renameLoadedPluginIDs(tx)
}

// v26SourcePath accepts the files of a v26 package: canvas.json instead of the
// widget manifest, with the same source, asset and legacy fixture paths.
func v26SourcePath(name string) bool {
	return name == "canvas.json" || (name != widget.ManifestFile && widget.ValidFilePath(name))
}

// convertV26Source renames the manifest and its plugin source fields and points
// source files at the renamed SDK. Drafts may be incomplete, so nothing is parsed.
func convertV26Source(files map[string]string) map[string]string {
	out := make(map[string]string, len(files))
	for name, content := range files {
		switch {
		case name == "canvas.json":
			name = widget.ManifestFile
			content = strings.ReplaceAll(content, `"appID"`, `"pluginID"`)
		case strings.HasPrefix(name, "src/"):
			content = strings.ReplaceAll(content, "@pudding/canvas", "@pudding/widget")
		}
		out[name] = content
	}
	return out
}

// moveWidgetSources installs every revision and draft in the studio layout and
// returns, per item, the hash each v26 revision now has.
func moveWidgetSources(tx *sql.Tx, home string) (map[string]map[string]string, error) {
	rows, err := tx.Query(`SELECT canvas_id,hash FROM canvas_revisions ORDER BY canvas_id,created_at,hash`)
	if err != nil {
		return nil, err
	}
	type revision struct{ item, hash string }
	var revisions []revision
	for rows.Next() {
		var r revision
		if err := rows.Scan(&r.item, &r.hash); err != nil {
			rows.Close()
			return nil, err
		}
		revisions = append(revisions, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	items, err := tx.Query(`SELECT id FROM canvas_resources ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var ids []string
	for items.Next() {
		var id string
		if err := items.Scan(&id); err != nil {
			items.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := items.Err(); err != nil {
		items.Close()
		return nil, err
	}
	items.Close()
	if home == "" {
		if len(ids) > 0 {
			return nil, errors.New("studio migration requires the Pudding home directory")
		}
		return nil, nil
	}
	moved := map[string]map[string]string{}
	for _, r := range revisions {
		files, err := widget.ReadVersion(filepath.Join(home, "canvases", r.item, "revisions", r.hash), v26SourcePath)
		if err != nil {
			return nil, fmt.Errorf("read widget %s revision %s: %w", r.item, r.hash, err)
		}
		if widget.PackageHash(files) != r.hash {
			return nil, fmt.Errorf("widget %s revision %s does not match its content", r.item, r.hash)
		}
		hash, err := widget.WritePackage(home, r.item, widget.Package{Files: convertV26Source(files)})
		if err != nil {
			return nil, fmt.Errorf("convert widget %s revision %s: %w", r.item, r.hash, err)
		}
		if moved[r.item] == nil {
			moved[r.item] = map[string]string{}
		}
		moved[r.item][r.hash] = hash
	}
	for _, id := range ids {
		old := filepath.Join(home, "canvases", id, "draft")
		if _, err := os.Stat(old); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			return nil, err
		}
		root, err := widget.DraftRoot(home, id)
		if err != nil {
			return nil, err
		}
		if _, err := os.Stat(root); err == nil {
			continue // Installed by an earlier attempt of this upgrade.
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		draft, err := widget.ReadDraftDir(old, v26SourcePath)
		if err != nil {
			return nil, fmt.Errorf("read widget %s draft: %w", id, err)
		}
		base := draft.BaseRevisionHash
		if base != "" {
			mapped, ok := moved[id][base]
			if !ok {
				return nil, fmt.Errorf("widget %s draft base %s has no revision", id, base)
			}
			base = mapped
		}
		if err := widget.InstallDraft(root, base, convertV26Source(draft.Files)); err != nil {
			return nil, fmt.Errorf("install widget %s draft: %w", id, err)
		}
	}
	return moved, nil
}

// remapWidgetHashes points revisions, parents, saves and the head and active
// selections at the converted packages. Build receipts were cleared on copy:
// opening a widget performs a real build of the converted source.
func remapWidgetHashes(tx *sql.Tx, moved map[string]map[string]string) error {
	for item, hashes := range moved {
		for old, hash := range hashes {
			if _, err := tx.Exec(`UPDATE studio_item_revisions SET hash=? WHERE item_id=? AND hash=?;
UPDATE studio_item_revisions SET parent_revision=? WHERE item_id=? AND parent_revision=?;
UPDATE studio_item_saves SET hash=? WHERE item_id=? AND hash=?;
UPDATE studio_items SET head_revision=CASE WHEN head_revision=? THEN ? ELSE head_revision END,
 active_revision=CASE WHEN active_revision=? THEN ? ELSE active_revision END WHERE id=?`,
				hash, item, old, hash, item, old, hash, item, old, old, hash, old, hash, item); err != nil {
				return fmt.Errorf("remap widget %s revision %s: %w", item, old, err)
			}
		}
	}
	return nil
}

// Stored action specs and link entities keep their meaning; only the field that
// names the plugin is renamed, through the current structs so later lookups of
// the same entity produce identical JSON.
func renameWidgetPluginFields(tx *sql.Tx) error {
	type v26Spec struct {
		RevisionHash       string          `json:"revisionHash"`
		ResourceRevision   int64           `json:"resourceRevision"`
		BindingVersion     int64           `json:"bindingVersion"`
		OperationID        string          `json:"operationID"`
		OperationHash      string          `json:"operationHash"`
		BindingFingerprint string          `json:"bindingFingerprint"`
		AppID              string          `json:"appID"`
		ConnectionID       string          `json:"connectionID"`
		Description        string          `json:"description"`
		Params             map[string]any  `json:"params"`
		Request            json.RawMessage `json:"request"`
	}
	type v26Entity struct {
		AppID        string `json:"appID"`
		ConnectionID string `json:"connectionID"`
		EntityType   string `json:"entityType"`
		EntityID     string `json:"entityID"`
	}
	entity := func(raw string) (string, error) {
		var old v26Entity
		if err := json.Unmarshal([]byte(raw), &old); err != nil {
			return "", err
		}
		data, err := json.Marshal(store.WidgetEntity{PluginID: old.AppID, ConnectionID: old.ConnectionID, EntityType: old.EntityType, EntityID: old.EntityID})
		return string(data), err
	}
	type row struct{ id, a, b string }
	collect := func(query string, columns int) ([]row, error) {
		rows, err := tx.Query(query)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []row
		for rows.Next() {
			var r row
			dest := []any{&r.id, &r.a}
			if columns == 3 {
				dest = append(dest, &r.b)
			}
			if err := rows.Scan(dest...); err != nil {
				return nil, err
			}
			out = append(out, r)
		}
		return out, rows.Err()
	}
	actions, err := collect(`SELECT id,spec FROM widget_actions`, 2)
	if err != nil {
		return err
	}
	for _, a := range actions {
		var old v26Spec
		if err := json.Unmarshal([]byte(a.a), &old); err != nil {
			return fmt.Errorf("widget action %s: %w", a.id, err)
		}
		spec, err := json.Marshal(store.WidgetActionSpec{RevisionHash: old.RevisionHash, ResourceRevision: old.ResourceRevision, BindingVersion: old.BindingVersion, OperationID: old.OperationID, OperationHash: old.OperationHash, BindingFingerprint: old.BindingFingerprint, PluginID: old.AppID, ConnectionID: old.ConnectionID, Description: old.Description, Params: old.Params, Request: old.Request})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE widget_actions SET spec=? WHERE id=?`, string(spec), a.id); err != nil {
			return err
		}
	}
	links, err := collect(`SELECT id,left_entity,right_entity FROM widget_links`, 3)
	if err != nil {
		return err
	}
	for _, l := range links {
		left, err := entity(l.a)
		if err != nil {
			return fmt.Errorf("widget link %s: %w", l.id, err)
		}
		right, err := entity(l.b)
		if err != nil {
			return fmt.Errorf("widget link %s: %w", l.id, err)
		}
		if _, err := tx.Exec(`UPDATE widget_links SET left_entity=?,right_entity=? WHERE id=?`, left, right, l.id); err != nil {
			return err
		}
	}
	return nil
}

func renameLoadedPluginIDs(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT id,loaded_plugin_ids FROM sessions`)
	if err != nil {
		return err
	}
	type update struct{ id, ids string }
	var updates []update
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var ids []string
		if err := json.Unmarshal([]byte(raw), &ids); err != nil {
			rows.Close()
			return err
		}
		changed := false
		for i, pluginID := range ids {
			if renamed, ok := renamedPluginIDs[strings.TrimSpace(pluginID)]; ok {
				ids[i], changed = renamed, true
			}
		}
		if changed {
			data, err := json.Marshal(store.NormalizePluginIDs(ids))
			if err != nil {
				rows.Close()
				return err
			}
			updates = append(updates, update{id, string(data)})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, u := range updates {
		if _, err := tx.Exec(`UPDATE sessions SET loaded_plugin_ids=? WHERE id=?`, u.ids, u.id); err != nil {
			return err
		}
	}
	return nil
}

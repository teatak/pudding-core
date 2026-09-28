package sqlitestore

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/canvasarchive"
)

func archiveLegacyCanvases(tx *sql.Tx, home string) error {
	rows, err := tx.Query(`SELECT w.id,w.name,coalesce(w.source_session_id,''),coalesce(s.title,''),w.active_revision,w.head_revision,w.revision,w.deleted,w.created_at,w.updated_at
 FROM canvas_resources w LEFT JOIN sessions s ON s.id=w.source_session_id WHERE EXISTS(SELECT 1 FROM canvas_revisions r WHERE r.canvas_id=w.id AND r.content_json<>'') ORDER BY w.id`)
	if err != nil {
		return err
	}
	snapshots := []canvasarchive.Snapshot{}
	for rows.Next() {
		var s canvasarchive.Snapshot
		if err = rows.Scan(&s.ID, &s.Name, &s.SourceSessionID, &s.SourceSessionTitle, &s.ActiveRevision, &s.HeadRevision, &s.Revision, &s.Deleted, &s.CreatedAt, &s.UpdatedAt); err != nil {
			rows.Close()
			return err
		}
		snapshots = append(snapshots, s)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, s := range snapshots {
		versions, err := tx.Query(`SELECT hash,parent_revision,client_request_id,created_at,content_json FROM canvas_revisions WHERE canvas_id=? AND content_json<>'' ORDER BY created_at,hash`, s.ID)
		if err != nil {
			return err
		}
		for versions.Next() {
			var r canvasarchive.Revision
			var content string
			if err = versions.Scan(&r.Hash, &r.ParentRevision, &r.ClientRequestID, &r.CreatedAt, &content); err != nil {
				versions.Close()
				return err
			}
			if !json.Valid([]byte(content)) {
				versions.Close()
				return errors.New("invalid legacy canvas content")
			}
			r.Content = json.RawMessage(content)
			s.Revisions = append(s.Revisions, r)
		}
		err = versions.Err()
		versions.Close()
		if err != nil {
			return err
		}
		if s.Mounts, err = archiveRows(tx, `SELECT * FROM canvas_mounts WHERE resource_id=? ORDER BY session_id,id`, s.ID); err != nil {
			return err
		}
		if s.Favorites, err = archiveRows(tx, `SELECT * FROM library_favorites WHERE saved_item_id=? ORDER BY id`, s.ID); err != nil {
			return err
		}
		if s.Recent, err = archiveRows(tx, `SELECT r.* FROM library_recent_opens r JOIN canvas_mounts m ON m.session_id=r.source_session_id AND m.id=r.canvas_item_id WHERE m.resource_id=? ORDER BY r.id`, s.ID); err != nil {
			return err
		}
		if _, err = canvasarchive.Write(home, s); err != nil {
			return err
		}
	}
	return nil
}
func archiveRows(tx *sql.Tx, query, id string) ([]map[string]any, error) {
	rows, err := tx.Query(query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	out := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		record := map[string]any{}
		for i, c := range cols {
			v := values[i]
			if b, ok := v.([]byte); ok {
				v = string(b)
			}
			record[c] = v
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
func retireLegacyCanvasSchema(tx *sql.Tx) error {
	_, err := tx.Exec(`
 DELETE FROM canvas_resources WHERE EXISTS(SELECT 1 FROM canvas_revisions r WHERE r.canvas_id=canvas_resources.id AND r.content_json<>'') AND NOT EXISTS(SELECT 1 FROM canvas_revisions r WHERE r.canvas_id=canvas_resources.id AND r.content_json='');
 UPDATE canvas_resources SET
 head_revision=coalesce((SELECT hash FROM canvas_revisions r WHERE r.canvas_id=canvas_resources.id AND r.content_json='' ORDER BY created_at DESC,hash DESC LIMIT 1),''),
 active_revision=CASE WHEN active_revision IN(SELECT hash FROM canvas_revisions r WHERE r.canvas_id=canvas_resources.id AND r.content_json<>'') THEN coalesce((SELECT hash FROM canvas_revisions r WHERE r.canvas_id=canvas_resources.id AND r.content_json='' AND r.build_receipt<>'' ORDER BY created_at DESC,hash DESC LIMIT 1),'') ELSE active_revision END,
 revision=revision+1
 WHERE EXISTS(SELECT 1 FROM canvas_revisions r WHERE r.canvas_id=canvas_resources.id AND r.content_json<>'');
 DELETE FROM canvas_saves WHERE EXISTS(SELECT 1 FROM canvas_revisions r WHERE r.canvas_id=canvas_saves.canvas_id AND r.hash=canvas_saves.hash AND r.content_json<>'');
 DELETE FROM canvas_revisions WHERE content_json<>'';
 UPDATE canvas_revisions SET parent_revision='' WHERE parent_revision<>'' AND NOT EXISTS(SELECT 1 FROM canvas_revisions p WHERE p.canvas_id=canvas_revisions.canvas_id AND p.hash=canvas_revisions.parent_revision);
 ALTER TABLE canvas_revisions DROP COLUMN content_json;
 ALTER TABLE canvas_mounts DROP COLUMN window_json;
 `)
	return err
}

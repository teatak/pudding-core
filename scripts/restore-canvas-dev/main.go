// One-time recovery for pre-release development archives. This command is not
// part of daemon startup or the shipped migration path. Run against a copy first.
package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/canvaslegacy"
	"github.com/teatak/pudding-core/internal/store"
)

type revision struct {
	Hash            string
	ParentRevision  string
	ClientRequestID string
	CreatedAt       int64
	Content         store.CanvasContent
}
type snapshot struct {
	ID, Name, SourceSessionID, ActiveRevision, HeadRevision string
	Revision, CreatedAt, UpdatedAt                          int64
	Deleted                                                 bool
	Revisions                                               []revision
	Mounts, Favorites, Recent                               []map[string]any
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	home := flag.String("home", "", "target home (requires an existing schema 26 database)")
	archives := flag.String("archives", "", "legacy archive directory")
	assets := flag.String("asset-home", "", "home containing referenced attachments")
	flag.Parse()
	if *home == "" || *archives == "" || *assets == "" {
		return fmt.Errorf("home, archives and asset-home are required")
	}
	return restore(*home, *archives, *assets)
}

func restore(home, archives, assets string) error {
	db, err := sql.Open("sqlite3", "file:"+filepath.ToSlash(filepath.Join(home, "data", "pudding.db"))+"?mode=rw&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return err
	}
	defer db.Close()
	var version int
	if err = db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version != 26 {
		return fmt.Errorf("expected schema 26, got %d", version)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	paths, err := filepath.Glob(filepath.Join(archives, "*", "original.json"))
	if err != nil {
		return err
	}
	resolve := canvaslegacy.Images(assets)
	restored := 0
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		manifest, err := os.ReadFile(filepath.Join(filepath.Dir(path), "manifest.json"))
		if err != nil {
			return err
		}
		var m struct{ SnapshotHash string }
		if err = json.Unmarshal(manifest, &m); err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(raw)) != m.SnapshotHash {
			return fmt.Errorf("snapshot checksum mismatch: %s", path)
		}
		var s snapshot
		if err = json.Unmarshal(raw, &s); err != nil {
			return err
		}
		hashes := map[string]string{}
		for _, r := range s.Revisions {
			p, err := canvaslegacy.Convert(r.Content, resolve)
			if err != nil {
				return fmt.Errorf("%s: %w", s.ID, err)
			}
			hash, err := canvas.WritePackage(home, s.ID, p)
			if err != nil {
				return err
			}
			hashes[r.Hash] = hash
		}
		for _, hash := range []string{s.HeadRevision, s.ActiveRevision} {
			if hash != "" && hashes[hash] == "" {
				return fmt.Errorf("missing revision in %s", s.ID)
			}
		}
		var exists int
		if err = tx.QueryRow(`SELECT count(*) FROM canvas_resources WHERE id=?`, s.ID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			for _, hash := range hashes {
				var found int
				if err = tx.QueryRow(`SELECT count(*) FROM canvas_revisions WHERE canvas_id=? AND hash=?`, s.ID, hash).Scan(&found); err != nil {
					return err
				}
				if found != 1 {
					return fmt.Errorf("refusing to overwrite existing canvas %s", s.ID)
				}
			}
			continue
		}
		if _, err = tx.Exec(`INSERT INTO canvas_resources(id,name,source_session_id,revision,head_revision,active_revision,bindings,binding_version,deleted,created_at,updated_at) VALUES(?,?,(SELECT id FROM sessions WHERE id=?),?,?,?,'{}',1,?,?,?)`, s.ID, s.Name, s.SourceSessionID, s.Revision, hashes[s.HeadRevision], hashes[s.ActiveRevision], s.Deleted, s.CreatedAt, s.UpdatedAt); err != nil {
			return err
		}
		for _, r := range s.Revisions {
			if r.ParentRevision != "" && hashes[r.ParentRevision] == "" {
				return fmt.Errorf("missing parent revision in %s", s.ID)
			}
			if _, err = tx.Exec(`INSERT INTO canvas_revisions(canvas_id,hash,parent_revision,client_request_id,created_at,build_receipt) VALUES(?,?,?,?,?,'')`, s.ID, hashes[r.Hash], hashes[r.ParentRevision], r.ClientRequestID, r.CreatedAt); err != nil {
				return err
			}
		}
		for _, row := range s.Mounts {
			if _, err = tx.Exec(`INSERT INTO canvas_mounts(session_id,id,resource_id,visible,created_at) VALUES(?,?,?,?,?)`, row["session_id"], row["id"], s.ID, row["visible"], row["created_at"]); err != nil {
				return fmt.Errorf("restore mount %s: %w", s.ID, err)
			}
		}
		for _, row := range s.Favorites {
			if _, err = tx.Exec(`INSERT INTO library_favorites(id,kind,source_session_id,saved_item_id,url,title,created_at) VALUES(?,?,?,?,?,?,?)`, row["id"], row["kind"], row["source_session_id"], s.ID, row["url"], row["title"], row["created_at"]); err != nil {
				return err
			}
		}
		for _, row := range s.Recent {
			if _, err = tx.Exec(`INSERT INTO library_recent_opens(id,kind,source_session_id,canvas_item_id,root_path,path,opened_at) VALUES(?,?,?,?,?,?,?)`, row["id"], row["kind"], row["source_session_id"], row["canvas_item_id"], row["root_path"], row["path"], row["opened_at"]); err != nil {
				return err
			}
		}
		restored++
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	fmt.Printf("Restored %d canvases; scanned %d snapshots.\n", restored, len(paths))
	return nil
}

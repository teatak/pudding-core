package sqlitestore

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

// nativeContentSave carries persistence metadata only; editor requests keep their
// original field layout because their serialized bytes are request fingerprints.
type nativeContentSave struct {
	ClientRequestID string
	Author          store.ContentAuthor
	PreserveOnly    bool
}

func scanContentSnapshot(row messageScanner) (*store.ContentSnapshot, error) {
	d := &store.ContentSnapshot{}
	err := row.Scan(&d.ItemID, &d.Body, &d.ContentHash, &d.RevisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return d, err
}

func validateContentAuthor(ctx context.Context, tx *sql.Tx, author store.ContentAuthor) error {
	if author.Kind == "user" && author.SessionID == "" && author.TurnID == "" {
		return nil
	}
	if author.Kind == "session" && author.SessionID != "" && author.TurnID != "" {
		turn, err := getTurnTx(ctx, tx, author.TurnID)
		if err != nil {
			return err
		}
		if turn.SessionID == author.SessionID {
			return nil
		}
	}
	return &store.InvalidContent{Message: "invalid content author"}
}

// Each debounced human save seals that editing pause. AI writes and restores
// always append a version, even when restoring a previously seen body.
func saveNativeContentTx(ctx context.Context, tx *sql.Tx, d *store.ContentSnapshot, in nativeContentSave, requestHash string) error {
	parent := d.RevisionID
	revision := store.ContentHash(d.ItemID + "\x00" + parent + "\x00" + in.ClientRequestID + "\x00" + d.ContentHash)
	now := unixMS(time.Now().UTC())
	_, err := tx.ExecContext(ctx, `INSERT INTO studio_item_revisions(item_id,hash,parent_revision,client_request_id,created_at,build_receipt,content,content_hash,author_kind,author_session_id,author_turn_id) VALUES(?,?,?,?,?,'',?,?,?,?,?)`, d.ItemID, revision, parent, in.ClientRequestID, now, d.Body, d.ContentHash, in.Author.Kind, in.Author.SessionID, in.Author.TurnID)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_saves(item_id,client_request_id,hash,request_hash) VALUES(?,?,?,?)`, d.ItemID, in.ClientRequestID, revision, requestHash); err != nil {
		return err
	}
	if in.PreserveOnly {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE studio_item_content SET body=?,content_hash=? WHERE item_id=?`, d.Body, d.ContentHash, d.ItemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE studio_items SET head_revision=?,revision=revision+1,updated_at=? WHERE id=?`, revision, now, d.ItemID); err != nil {
		return err
	}
	d.RevisionID = revision
	return nil
}

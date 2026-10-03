package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

const documentSelect = `SELECT w.id,c.body,c.content_hash,w.head_revision FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=? AND w.kind='doc' AND w.deleted=0`

func scanDocument(row messageScanner) (*store.DocumentContent, error) {
	d := &store.DocumentContent{}
	err := row.Scan(&d.ItemID, &d.Body, &d.ContentHash, &d.RevisionID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return d, err
}

func (s *Store) GetDocument(ctx context.Context, id string) (*store.DocumentContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return scanDocument(s.db.QueryRowContext(ctx, documentSelect, id))
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
	return &store.InvalidDocument{Message: "invalid content author"}
}

func (s *Store) CreateDocument(ctx context.Context, item *store.StudioItem, body string, author store.ContentAuthor) (*store.DocumentContent, error) {
	if err := store.ValidateDocument(body); err != nil {
		return nil, err
	}
	if item.Kind != store.StudioItemKindDoc {
		return nil, &store.InvalidDocument{Message: "document kind required"}
	}
	var out *store.DocumentContent
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, author); err != nil {
			return err
		}
		if err := createStudioItemTx(ctx, tx, item); err != nil {
			return err
		}
		hash := store.DocumentHash(body)
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_content(item_id,body,content_hash) VALUES(?,?,?)`, item.ID, body, hash); err != nil {
			return err
		}
		out = &store.DocumentContent{ItemID: item.ID, Body: body, ContentHash: hash}
		return saveNativeContentTx(ctx, tx, out, store.DocumentWrite{ClientRequestID: "create", Author: author}, "")
	})
	return out, err
}

func (s *Store) WriteDocument(ctx context.Context, id string, in store.DocumentWrite) (*store.DocumentContent, error) {
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 {
		return nil, &store.InvalidDocument{Message: "clientRequestID is required (max 200 bytes)"}
	}
	modes := 0
	if in.Body != nil {
		modes++
	}
	if len(in.Edits) > 0 {
		modes++
	}
	if in.RestoreRevision != "" {
		modes++
	}
	if in.UndoRevision != "" {
		modes++
	}
	if in.UndoRevision != "" && in.Author.Kind != "user" {
		return nil, &store.InvalidDocument{Message: "undo is a user operation"}
	}
	if modes != 1 || (in.PreserveOnly && (in.Body == nil || in.Author.Kind != "user")) {
		return nil, &store.InvalidDocument{Message: "provide exactly one of body, edits, restoreRevision or undoRevision"}
	}
	if (in.Body != nil || in.RestoreRevision != "") && in.ExpectedHash == nil {
		return nil, &store.InvalidDocument{Message: "expectedHash is required for replacement or restore"}
	}
	payload, _ := json.Marshal(struct {
		Write  store.DocumentWrite
		Author store.ContentAuthor
	}{in, in.Author})
	requestHash := store.DocumentHash(string(payload))
	var out *store.DocumentContent
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, in.Author); err != nil {
			return err
		}
		current, err := scanDocument(tx.QueryRowContext(ctx, documentSelect, id))
		if err != nil {
			return err
		}
		var savedHash, savedRevision string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,hash FROM studio_item_saves WHERE item_id=? AND client_request_id=?`, id, in.ClientRequestID).Scan(&savedHash, &savedRevision)
		if err == nil {
			if savedHash != requestHash {
				return &store.InvalidDocument{Message: "clientRequestID reused with different content"}
			}
			if in.PreserveOnly {
				out = current
				return nil
			}
			out, err = scanDocument(tx.QueryRowContext(ctx, `SELECT item_id,content,content_hash,hash FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, savedRevision))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !in.PreserveOnly && in.ExpectedHash != nil && *in.ExpectedHash != current.ContentHash {
			return &store.ContentConflict{CurrentHash: current.ContentHash}
		}
		body := current.Body
		if in.Body != nil {
			body = *in.Body
		}
		if len(in.Edits) > 0 {
			body, err = store.ApplyDocumentEdits(body, in.Edits)
			if err != nil {
				return err
			}
		}
		if in.RestoreRevision != "" {
			var content sql.NullString
			if err := tx.QueryRowContext(ctx, `SELECT content FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, in.RestoreRevision).Scan(&content); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return store.ErrNotFound
				}
				return err
			}
			if !content.Valid {
				return &store.InvalidDocument{Message: "revision has no document content"}
			}
			body = content.String
		}

		if in.UndoRevision != "" {
			before, after, err := nativeUndoSourceTx(ctx, tx, id, current.RevisionID, in.UndoRevision)
			if err == nil {
				body, err = store.UndoDocument(before, after, current.Body)
			}
			if errors.Is(err, store.ErrUndoConflict) {
				return &store.ContentConflict{CurrentHash: current.ContentHash}
			}
			if err != nil {
				return err
			}
		}
		if err := store.ValidateDocument(body); err != nil {
			return err
		}
		out = &store.DocumentContent{ItemID: id, Body: body, ContentHash: store.DocumentHash(body), RevisionID: current.RevisionID}
		if err := saveNativeContentTx(ctx, tx, out, in, requestHash); err != nil {
			return err
		}
		if in.PreserveOnly {
			out = current
		}
		return nil
	})
	return out, err
}

// Each debounced human save seals that editing pause. AI writes and restores
// always append a version, even when restoring a previously seen body.
func saveNativeContentTx(ctx context.Context, tx *sql.Tx, d *store.DocumentContent, in store.DocumentWrite, requestHash string) error {
	parent := d.RevisionID
	revision := store.DocumentHash(d.ItemID + "\x00" + parent + "\x00" + in.ClientRequestID + "\x00" + d.ContentHash)
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

package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

const documentSelect = `SELECT w.id,c.body,c.content_hash,w.head_revision FROM studio_items w JOIN studio_item_content c ON c.item_id=w.id WHERE w.id=? AND w.kind='rich-document' AND w.deleted=0 AND w.archived_at=0`

func documentFromContent(d *store.ContentSnapshot) (*store.DocumentContent, error) {
	var body store.DocumentBody
	if err := json.Unmarshal([]byte(d.Body), &body); err != nil {
		return nil, err
	}
	return &store.DocumentContent{ItemID: d.ItemID, Body: body, ContentHash: d.ContentHash, RevisionID: d.RevisionID}, nil
}
func (s *Store) GetDocument(ctx context.Context, id string) (*store.DocumentContent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := scanContentSnapshot(s.db.QueryRowContext(ctx, documentSelect, id))
	if err != nil {
		return nil, err
	}
	return documentFromContent(d)
}
func documentAssetsTx(ctx context.Context, tx *sql.Tx, id string, body store.DocumentBody, blobs []store.DocumentBlob) error {
	if len(blobs) > 101 {
		return &store.InvalidContent{Message: "too many uploaded assets"}
	}
	refs := map[string]string{}
	for _, a := range body.Assets {
		if mime, ok := refs[a.Hash]; ok && mime != a.Type {
			return &store.InvalidContent{Message: "asset type mismatch"}
		}
		refs[a.Hash] = a.Type
	}
	if body.Original != nil {
		if _, ok := refs[body.Original.Hash]; ok {
			return &store.InvalidContent{Message: "original file cannot be an image"}
		}
		refs[body.Original.Hash] = store.DOCXMIME
	}
	total := 0
	for _, blob := range blobs {
		total += len(blob.Data)
		if total > store.MaxDocumentAssetsBytes {
			return &store.InvalidContent{Message: "asset upload too large"}
		}
		if refs[blob.Hash] != blob.Type {
			return &store.InvalidContent{Message: "unreferenced asset upload"}
		}
		if err := store.ValidateDocumentBlob(blob); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO studio_rich_blobs(item_id,hash,mime,data) VALUES(?,?,?,?)`, id, blob.Hash, blob.Type, blob.Data); err != nil {
			return err
		}
	}
	total = 0
	for hash, mime := range refs {
		var size int
		var actual string
		err := tx.QueryRowContext(ctx, `SELECT mime,length(data) FROM studio_rich_blobs WHERE item_id=? AND hash=?`, id, hash).Scan(&actual, &size)
		if errors.Is(err, sql.ErrNoRows) || err == nil && actual != mime {
			return &store.InvalidContent{Message: "asset reference missing from this document"}
		}
		if err != nil {
			return err
		}
		total += size
		if total > store.MaxDocumentAssetsBytes {
			return &store.InvalidContent{Message: "document assets too large"}
		}
	}
	// Keep unreferenced historical assets: old revisions and undo still own them.
	return nil
}
func (s *Store) CreateDocument(ctx context.Context, item *store.StudioItem, body store.DocumentBody, blobs []store.DocumentBlob, authors ...store.ContentAuthor) (*store.DocumentContent, error) {
	if item.Kind != store.StudioItemKindDocument {
		return nil, &store.InvalidContent{Message: "rich-document kind required"}
	}
	if err := store.ValidateDocument(body); err != nil {
		return nil, err
	}
	author := store.ContentAuthor{Kind: "user"}
	if len(authors) > 0 {
		author = authors[0]
	}
	payload, _ := json.Marshal(struct {
		Name   string
		Body   store.DocumentBody
		Author store.ContentAuthor
	}{item.Name, body, author})
	requestHash := store.ContentHash(string(payload))
	var d *store.ContentSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var savedHash, savedRevision string
		err := tx.QueryRowContext(ctx, `SELECT request_hash,hash FROM studio_item_saves WHERE item_id=? AND client_request_id='create'`, item.ID).Scan(&savedHash, &savedRevision)
		if err == nil {
			if savedHash != requestHash {
				return &store.InvalidContent{Message: "creation request reused with different content"}
			}
			// An archived/deleted item must not be resurrected by a retry.
			if _, err = scanContentSnapshot(tx.QueryRowContext(ctx, documentSelect, item.ID)); err != nil {
				return err
			}
			d, err = scanContentSnapshot(tx.QueryRowContext(ctx, `SELECT item_id,content,content_hash,hash FROM studio_item_revisions WHERE item_id=? AND hash=?`, item.ID, savedRevision))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := createStudioItemTx(ctx, tx, item); err != nil {
			return err
		}
		if err := documentAssetsTx(ctx, tx, item.ID, body, blobs); err != nil {
			return err
		}
		raw, _ := json.Marshal(body)
		d = &store.ContentSnapshot{ItemID: item.ID, Body: string(raw), ContentHash: store.ContentHash(string(raw))}
		if _, err := tx.ExecContext(ctx, `INSERT INTO studio_item_content(item_id,body,content_hash) VALUES(?,?,?)`, item.ID, d.Body, d.ContentHash); err != nil {
			return err
		}
		return saveNativeContentTx(ctx, tx, d, nativeContentSave{ClientRequestID: "create", Author: author}, requestHash)
	})
	if err != nil {
		return nil, err
	}
	return documentFromContent(d)
}
func (s *Store) WriteDocument(ctx context.Context, id string, in store.DocumentWrite) (*store.DocumentContent, error) {
	if in.Author.Kind == "" {
		in.Author = store.ContentAuthor{Kind: "user"}
	}
	choices := 0
	if in.Body != nil {
		choices++
	}
	if in.RestoreRevision != "" {
		choices++
	}
	if in.UndoRevision != "" {
		choices++
	}
	if len(in.Operations) > 0 {
		choices++
	}
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 || !store.ValidContentHash(in.ExpectedHash) || choices != 1 || in.Body == nil && len(in.Blobs) > 0 {
		return nil, &store.InvalidContent{Message: "clientRequestID, expectedHash and exactly one of body/operations/restoreRevision/undoRevision are required"}
	}
	if in.UndoRevision != "" && in.Author.Kind != "user" {
		return nil, &store.InvalidContent{Message: "AI undo is user-only"}
	}
	if in.Body != nil {
		if err := store.ValidateDocument(*in.Body); err != nil {
			return nil, err
		}
	}
	// Hash the request metadata and content addresses, not a second base64 copy.
	for _, blob := range in.Blobs {
		if err := store.ValidateDocumentBlob(blob); err != nil {
			return nil, err
		}
	}
	payload, _ := json.Marshal(struct {
		ClientRequestID, ExpectedHash, RestoreRevision, UndoRevision string
		Body                                                         *store.DocumentBody
		Operations                                                   []store.DocumentOperation
		Author                                                       store.ContentAuthor
	}{in.ClientRequestID, in.ExpectedHash, in.RestoreRevision, in.UndoRevision, in.Body, in.Operations, in.Author})
	// Preserve retries of user body/restoration saves made by older clients.
	if len(in.Operations) == 0 && in.UndoRevision == "" && in.Author.Kind == "user" {
		payload, _ = json.Marshal(struct {
			ClientRequestID, ExpectedHash, RestoreRevision string
			Body                                           *store.DocumentBody
		}{in.ClientRequestID, in.ExpectedHash, in.RestoreRevision, in.Body})
	}
	requestHash := store.ContentHash(string(payload))
	var out *store.ContentSnapshot
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		current, err := scanContentSnapshot(tx.QueryRowContext(ctx, documentSelect, id))
		if err != nil {
			return err
		}
		var savedHash, savedRevision string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,hash FROM studio_item_saves WHERE item_id=? AND client_request_id=?`, id, in.ClientRequestID).Scan(&savedHash, &savedRevision)
		if err == nil {
			if savedHash != requestHash {
				return &store.InvalidContent{Message: "clientRequestID reused with different content"}
			}
			out, err = scanContentSnapshot(tx.QueryRowContext(ctx, `SELECT item_id,content,content_hash,hash FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, savedRevision))
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current.ContentHash != in.ExpectedHash {
			return &store.ContentConflict{CurrentHash: current.ContentHash}
		}
		body := in.Body
		if in.RestoreRevision != "" {
			var raw string
			err = tx.QueryRowContext(ctx, `SELECT content FROM studio_item_revisions WHERE item_id=? AND hash=?`, id, in.RestoreRevision).Scan(&raw)
			if errors.Is(err, sql.ErrNoRows) {
				return store.ErrNotFound
			}
			if err != nil {
				return err
			}
			body = &store.DocumentBody{}
			if err = json.Unmarshal([]byte(raw), body); err != nil {
				return err
			}
		}
		if len(in.Operations) > 0 || in.UndoRevision != "" {
			var base store.DocumentBody
			if err = json.Unmarshal([]byte(current.Body), &base); err != nil {
				return err
			}
			next := base
			if len(in.Operations) > 0 {
				next, err = store.ApplyDocumentOperations(base, in.Operations, id+"/"+in.ClientRequestID)
			} else {
				var before, after string
				before, after, err = nativeUndoSourceTx(ctx, tx, id, current.RevisionID, in.UndoRevision)
				if err == nil {
					next, err = store.UndoDocument(before, after, base)
				}
			}
			if errors.Is(err, store.ErrUndoConflict) {
				return &store.ContentConflict{CurrentHash: current.ContentHash}
			}
			if err != nil {
				return err
			}
			body = &next
		}
		if err = store.ValidateDocument(*body); err != nil {
			return err
		}
		if err = documentAssetsTx(ctx, tx, id, *body, in.Blobs); err != nil {
			return err
		}
		raw, _ := json.Marshal(body)
		out = &store.ContentSnapshot{ItemID: id, Body: string(raw), ContentHash: store.ContentHash(string(raw)), RevisionID: current.RevisionID}
		return saveNativeContentTx(ctx, tx, out, nativeContentSave{ClientRequestID: in.ClientRequestID, Author: in.Author}, requestHash)
	})
	if err != nil {
		return nil, err
	}
	return documentFromContent(out)
}
func (s *Store) GetDocumentBlob(ctx context.Context, id, hash string) (*store.DocumentBlob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	blob := &store.DocumentBlob{Hash: hash}
	err := s.db.QueryRowContext(ctx, `SELECT b.mime,b.data FROM studio_rich_blobs b JOIN studio_items w ON w.id=b.item_id WHERE b.item_id=? AND b.hash=? AND w.kind='rich-document' AND w.deleted=0 AND w.archived_at=0`, id, hash).Scan(&blob.Type, &blob.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, store.ErrNotFound
	}
	return blob, err
}

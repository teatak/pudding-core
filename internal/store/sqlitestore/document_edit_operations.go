package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/store"
)

const documentEditLease = 2 * time.Minute
const documentEditMaxLifetime = 15 * time.Minute

func loadDocumentEditTx(ctx context.Context, tx *sql.Tx, itemID, id string) (*store.DocumentEditOperation, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM studio_document_edits WHERE item_id=? AND id=?`, itemID, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	var op store.DocumentEditOperation
	err := json.Unmarshal([]byte(raw), &op)
	return &op, err
}

func saveDocumentEditTx(ctx context.Context, tx *sql.Tx, op *store.DocumentEditOperation) error {
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO studio_document_edits(id,item_id,state,updated_at,payload) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET state=excluded.state,updated_at=excluded.updated_at,payload=excluded.payload`, op.ID, op.ItemID, op.State, op.UpdatedAt, string(raw))
	return err
}

func activeDocumentEditsTx(ctx context.Context, tx *sql.Tx, itemID string, now int64) ([]store.DocumentEditOperation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM studio_document_edits WHERE item_id=? AND state='active'`, itemID)
	if err != nil {
		return nil, err
	}
	all := []store.DocumentEditOperation{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var op store.DocumentEditOperation
		if err = json.Unmarshal([]byte(raw), &op); err != nil {
			rows.Close()
			return nil, err
		}
		all = append(all, op)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	active := []store.DocumentEditOperation{}
	for _, op := range all {
		if op.ExpiresAt <= now {
			op.State = "expired"
			op.UpdatedAt = now
			op.Sequence++
			if err = saveDocumentEditTx(ctx, tx, &op); err != nil {
				return nil, err
			}
		} else {
			active = append(active, op)
		}
	}
	return active, nil
}

func (s *Store) ChangeDocumentEdit(ctx context.Context, itemID string, in store.DocumentEditRequest) (*store.DocumentEditOperation, error) {
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 {
		return nil, &store.InvalidDocument{Message: "clientRequestID is required (max 200 bytes)"}
	}
	if in.Action != "begin" && in.Action != "renew" && in.Action != "cancel" {
		return nil, &store.InvalidDocument{Message: "invalid editing operation action"}
	}
	payload, _ := json.Marshal(struct {
		Input  store.DocumentEditRequest
		Author store.ContentAuthor
	}{in, in.Author})
	requestHash := store.DocumentHash(string(payload))
	var out *store.DocumentEditOperation
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, in.Author); err != nil {
			return err
		}
		doc, err := scanDocument(tx.QueryRowContext(ctx, documentSelect, itemID))
		if err != nil {
			return err
		}
		var hash, result string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,result FROM studio_document_edit_requests WHERE item_id=? AND client_request_id=?`, itemID, in.ClientRequestID).Scan(&hash, &result)
		if err == nil {
			if hash != requestHash {
				return &store.InvalidDocument{Message: "clientRequestID reused with different content"}
			}
			return json.Unmarshal([]byte(result), &out)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UnixMilli()
		active, err := activeDocumentEditsTx(ctx, tx, itemID, now)
		if err != nil {
			return err
		}
		if in.Action == "begin" {
			if in.ExpectedHash == "" || in.ExpectedHash != doc.ContentHash {
				return &store.ContentConflict{CurrentHash: doc.ContentHash}
			}
			from, to := in.From, in.To
			if to == 0 && in.Target != "" {
				from = strings.Index(doc.Body, in.Target)
				if from < 0 || strings.Contains(doc.Body[from+1:], in.Target) {
					return &store.InvalidDocument{Message: "target must occur exactly once, or supply an exact UTF-8 byte range"}
				}
				to = from + len(in.Target)
			}
			if !store.ValidDocumentRange(doc.Body, from, to) || to-from > contracts.Studio().MaxReadBytes {
				return &store.InvalidDocument{Message: "invalid UTF-8 editing range (maximum 64 KiB)"}
			}
			if in.Target != "" && doc.Body[from:to] != in.Target {
				return &store.ContentConflict{CurrentHash: doc.ContentHash}
			}
			if len(active) >= 8 {
				return &store.InvalidDocument{Message: "too many active document edits"}
			}
			for _, op := range active {
				if from <= op.To && to >= op.From {
					return &store.DocumentLocked{OperationID: op.ID}
				}
			}
			out = &store.DocumentEditOperation{ID: store.NewID("edit"), ItemID: itemID, State: "active", From: from, To: to, Target: doc.Body[from:to], ContentHash: doc.ContentHash, Author: in.Author, CreatedAt: now, UpdatedAt: now, ExpiresAt: now + documentEditLease.Milliseconds(), Sequence: 1, LeaseToken: store.NewID("lease")}
		} else {
			out, err = loadDocumentEditTx(ctx, tx, itemID, in.OperationID)
			if err != nil {
				return err
			}
			userCancel := in.Action == "cancel" && in.Author.Kind == "user"
			if !userCancel && (in.Author != out.Author || in.LeaseToken == "" || in.LeaseToken != out.LeaseToken) {
				return &store.InvalidDocument{Message: "editing operation owner or lease does not match"}
			}
			if out.State == "active" {
				out.UpdatedAt = now
				out.Sequence++
				if in.Action == "cancel" {
					out.State = "cancelled"
				} else {
					out.ExpiresAt = min(now+documentEditLease.Milliseconds(), out.CreatedAt+documentEditMaxLifetime.Milliseconds())
					if out.ExpiresAt <= now {
						return &store.InvalidDocument{Message: "editing operation reached its lifetime limit"}
					}
				}
			} else if in.Action == "renew" {
				return &store.InvalidDocument{Message: "editing operation is no longer active"}
			}
		}
		if err = saveDocumentEditTx(ctx, tx, out); err != nil {
			return err
		}
		resultBytes, _ := json.Marshal(out)
		_, err = tx.ExecContext(ctx, `INSERT INTO studio_document_edit_requests(item_id,client_request_id,request_hash,result) VALUES(?,?,?,?)`, itemID, in.ClientRequestID, requestHash, string(resultBytes))
		return err
	})
	return out, err
}

func (s *Store) GetDocumentEditState(ctx context.Context, itemID string) (*store.DocumentEditState, error) {
	out := &store.DocumentEditState{Operations: []store.DocumentEditOperation{}}
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		doc, err := scanDocument(tx.QueryRowContext(ctx, documentSelect, itemID))
		if err != nil {
			return err
		}
		out.ContentHash = doc.ContentHash
		now := time.Now().UnixMilli()
		if _, err = activeDocumentEditsTx(ctx, tx, itemID, now); err != nil {
			return err
		}
		rows, err := tx.QueryContext(ctx, `SELECT payload FROM studio_document_edits WHERE item_id=? AND (state='active' OR updated_at>?) ORDER BY updated_at DESC LIMIT 32`, itemID, now-int64(time.Minute/time.Millisecond))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			var op store.DocumentEditOperation
			if err = json.Unmarshal([]byte(raw), &op); err != nil {
				return err
			}
			op.LeaseToken = ""
			op.Target = ""
			out.Operations = append(out.Operations, op)
		}
		return rows.Err()
	})
	return out, err
}

func checkDocumentEditCommit(op *store.DocumentEditOperation, in store.DocumentWrite, body string, now int64) error {
	if op.State != "active" || op.ExpiresAt <= now {
		return &store.InvalidDocument{Message: "editing lease expired or operation already ended"}
	}
	if op.Author != in.Author || in.LeaseToken == "" || op.LeaseToken != in.LeaseToken {
		return &store.InvalidDocument{Message: "editing operation owner or lease does not match"}
	}
	if !store.ValidDocumentRange(body, op.From, op.To) || body[op.From:op.To] != op.Target {
		return &store.ContentConflict{CurrentHash: store.DocumentHash(body)}
	}
	return nil
}

func guardDocumentEditsTx(ctx context.Context, tx *sql.Tx, itemID, before, after, ownID string, now int64) error {
	active, err := activeDocumentEditsTx(ctx, tx, itemID, now)
	if err != nil {
		return err
	}
	for _, op := range active {
		if op.ID == ownID {
			continue
		}
		from, to, ok := store.MapDocumentRange(before, after, op.From, op.To)
		if !ok {
			return &store.DocumentLocked{OperationID: op.ID}
		}
		op.From = from
		op.To = to
		op.ContentHash = store.DocumentHash(after)
		op.UpdatedAt = now
		op.Sequence++
		if err = saveDocumentEditTx(ctx, tx, &op); err != nil {
			return err
		}
	}
	return nil
}

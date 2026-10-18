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

const pageEditLease = 2 * time.Minute
const pageEditMaxLifetime = 15 * time.Minute

func loadPageEditTx(ctx context.Context, tx *sql.Tx, itemID, id string) (*store.PageEditOperation, error) {
	var raw string
	if err := tx.QueryRowContext(ctx, `SELECT payload FROM studio_document_edits WHERE item_id=? AND id=?`, itemID, id).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	var op store.PageEditOperation
	err := json.Unmarshal([]byte(raw), &op)
	return &op, err
}

func savePageEditTx(ctx context.Context, tx *sql.Tx, op *store.PageEditOperation) error {
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO studio_document_edits(id,item_id,state,updated_at,payload) VALUES(?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET state=excluded.state,updated_at=excluded.updated_at,payload=excluded.payload`, op.ID, op.ItemID, op.State, op.UpdatedAt, string(raw))
	return err
}

func activePageEditsTx(ctx context.Context, tx *sql.Tx, itemID string, now int64) ([]store.PageEditOperation, error) {
	rows, err := tx.QueryContext(ctx, `SELECT payload FROM studio_document_edits WHERE item_id=? AND state='active'`, itemID)
	if err != nil {
		return nil, err
	}
	all := []store.PageEditOperation{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return nil, err
		}
		var op store.PageEditOperation
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
	active := []store.PageEditOperation{}
	for _, op := range all {
		ended := false
		if op.Author.Kind == "session" {
			turn, err := getTurnTx(ctx, tx, op.Author.TurnID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return nil, err
			}
			ended = err != nil || turn.Status != store.TurnRunning
		}
		if op.ExpiresAt <= now || ended {
			op.State = "expired"
			if ended {
				op.State = "cancelled"
			}
			op.UpdatedAt = now
			op.Sequence++
			if err = savePageEditTx(ctx, tx, &op); err != nil {
				return nil, err
			}
		} else {
			active = append(active, op)
		}
	}
	return active, nil
}

func (s *Store) ChangePageEdit(ctx context.Context, itemID string, in store.PageEditRequest) (*store.PageEditOperation, error) {
	if in.ClientRequestID == "" || len(in.ClientRequestID) > 200 {
		return nil, &store.InvalidContent{Message: "clientRequestID is required (max 200 bytes)"}
	}
	if in.Action != "begin" && in.Action != "renew" && in.Action != "cancel" {
		return nil, &store.InvalidContent{Message: "invalid editing operation action"}
	}
	payload, _ := json.Marshal(struct {
		Input  store.PageEditRequest
		Author store.ContentAuthor
	}{in, in.Author})
	requestHash := store.ContentHash(string(payload))
	var out *store.PageEditOperation
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := validateContentAuthor(ctx, tx, in.Author); err != nil {
			return err
		}
		doc, err := scanContentSnapshot(tx.QueryRowContext(ctx, pageSelect, itemID))
		if err != nil {
			return err
		}
		var hash, result string
		err = tx.QueryRowContext(ctx, `SELECT request_hash,result FROM studio_document_edit_requests WHERE item_id=? AND client_request_id=?`, itemID, in.ClientRequestID).Scan(&hash, &result)
		if err == nil {
			if hash != requestHash {
				return &store.InvalidContent{Message: "clientRequestID reused with different content"}
			}
			return json.Unmarshal([]byte(result), &out)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		now := time.Now().UnixMilli()
		active, err := activePageEditsTx(ctx, tx, itemID, now)
		if err != nil {
			return err
		}
		if in.Action == "begin" {
			if in.Author.Kind == "session" {
				turn, err := getTurnTx(ctx, tx, in.Author.TurnID)
				if err != nil {
					return err
				}
				if turn.Status != store.TurnRunning {
					return &store.InvalidContent{Message: "editing turn has ended"}
				}
			}
			if in.ExpectedHash == "" || in.ExpectedHash != doc.ContentHash {
				return &store.ContentConflict{CurrentHash: doc.ContentHash}
			}
			ranges := append([]store.PageEditRange(nil), in.Ranges...)
			if len(ranges) == 0 {
				ranges = []store.PageEditRange{{From: in.From, To: in.To, Target: in.Target}}
			} else if in.Target != "" || in.From != 0 || in.To != 0 {
				return &store.InvalidContent{Message: "provide ranges or a single target, not both"}
			}
			if len(ranges) > 8 || len(active) >= 8 {
				return &store.InvalidContent{Message: "too many active editing ranges"}
			}
			total := 0
			for i := range ranges {
				r := &ranges[i]
				if r.To == 0 && r.Target != "" {
					r.From = strings.Index(doc.Body, r.Target)
					if r.From < 0 || strings.Contains(doc.Body[r.From+1:], r.Target) {
						return &store.InvalidContent{Message: "target must occur exactly once, or supply an exact UTF-8 byte range"}
					}
					r.To = r.From + len(r.Target)
				}
				total += r.To - r.From
				if !store.ValidPageRange(doc.Body, r.From, r.To) || total > contracts.Studio().MaxReadBytes {
					return &store.InvalidContent{Message: "invalid UTF-8 editing ranges (maximum total 64 KiB)"}
				}
				if i > 0 && ranges[i-1].To >= r.From {
					return &store.InvalidContent{Message: "ranges must be disjoint and in document order"}
				}
				if r.Target != "" && doc.Body[r.From:r.To] != r.Target {
					return &store.ContentConflict{CurrentHash: doc.ContentHash}
				}
				r.Target = doc.Body[r.From:r.To]
				for _, op := range active {
					for _, other := range op.EditRanges() {
						if r.From <= other.To && r.To >= other.From {
							return &store.PageLocked{OperationID: op.ID}
						}
					}
				}
			}
			out = &store.PageEditOperation{ID: store.NewID("edit"), ItemID: itemID, State: "active", ContentHash: doc.ContentHash, Author: in.Author, CreatedAt: now, UpdatedAt: now, ExpiresAt: now + pageEditLease.Milliseconds(), Sequence: 1, LeaseToken: store.NewID("lease")}
			if len(in.Ranges) > 0 {
				out.Ranges = ranges
			}
			out.SetRanges(ranges)
		} else {
			out, err = loadPageEditTx(ctx, tx, itemID, in.OperationID)
			if err != nil {
				return err
			}
			userCancel := in.Action == "cancel" && in.Author.Kind == "user"
			if !userCancel && (in.Author != out.Author || in.LeaseToken == "" || in.LeaseToken != out.LeaseToken) {
				return &store.InvalidContent{Message: "editing operation owner or lease does not match"}
			}
			if out.State == "active" {
				out.UpdatedAt = now
				out.Sequence++
				if in.Action == "cancel" {
					out.State = "cancelled"
				} else {
					out.ExpiresAt = min(now+pageEditLease.Milliseconds(), out.CreatedAt+pageEditMaxLifetime.Milliseconds())
					if out.ExpiresAt <= now {
						return &store.InvalidContent{Message: "editing operation reached its lifetime limit"}
					}
				}
			} else if in.Action == "renew" {
				return &store.InvalidContent{Message: "editing operation is no longer active"}
			}
		}
		if err = savePageEditTx(ctx, tx, out); err != nil {
			return err
		}
		resultBytes, _ := json.Marshal(out)
		_, err = tx.ExecContext(ctx, `INSERT INTO studio_document_edit_requests(item_id,client_request_id,request_hash,result) VALUES(?,?,?,?)`, itemID, in.ClientRequestID, requestHash, string(resultBytes))
		return err
	})
	return out, err
}

func (s *Store) GetPageEditState(ctx context.Context, itemID string) (*store.PageEditState, error) {
	out := &store.PageEditState{Operations: []store.PageEditOperation{}}
	err := s.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		doc, err := scanContentSnapshot(tx.QueryRowContext(ctx, pageSelect, itemID))
		if err != nil {
			return err
		}
		out.ContentHash = doc.ContentHash
		now := time.Now().UnixMilli()
		if _, err = activePageEditsTx(ctx, tx, itemID, now); err != nil {
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
			var op store.PageEditOperation
			if err = json.Unmarshal([]byte(raw), &op); err != nil {
				return err
			}
			op.LeaseToken = ""
			op.Target = ""
			for i := range op.Ranges {
				op.Ranges[i].Target = ""
			}
			out.Operations = append(out.Operations, op)
		}
		return rows.Err()
	})
	return out, err
}

func checkPageEditCommit(op *store.PageEditOperation, in store.PageWrite, body string, now int64) error {
	if op.State == "cancelled" {
		return &store.InvalidContent{Message: "editing operation was cancelled; do not retry this edit without a new user request"}
	}
	if op.State != "active" || op.ExpiresAt <= now {
		return &store.InvalidContent{Message: "editing lease expired or operation already ended"}
	}
	if op.Author != in.Author || in.LeaseToken == "" || op.LeaseToken != in.LeaseToken {
		return &store.InvalidContent{Message: "editing operation owner or lease does not match"}
	}
	for _, r := range op.EditRanges() {
		if !store.ValidPageRange(body, r.From, r.To) || body[r.From:r.To] != r.Target {
			return &store.ContentConflict{CurrentHash: store.ContentHash(body)}
		}
	}
	return nil
}

func guardPageEditsTx(ctx context.Context, tx *sql.Tx, itemID, before, after, ownID string, now int64) error {
	active, err := activePageEditsTx(ctx, tx, itemID, now)
	if err != nil {
		return err
	}
	var all []store.PageEditRange
	var owners []string
	for _, op := range active {
		if op.ID == ownID {
			continue
		}
		for _, r := range op.EditRanges() {
			all = append(all, r)
			owners = append(owners, op.ID)
		}
	}
	mapped, conflict := store.MapPageRanges(before, after, all)
	if conflict >= 0 {
		return &store.PageLocked{OperationID: owners[conflict]}
	}
	offset := 0
	for _, op := range active {
		if op.ID == ownID {
			continue
		}
		count := len(op.EditRanges())
		op.SetRanges(mapped[offset : offset+count])
		offset += count
		op.ContentHash = store.ContentHash(after)
		op.UpdatedAt = now
		op.Sequence++
		if err = savePageEditTx(ctx, tx, &op); err != nil {
			return err
		}
	}
	return nil
}

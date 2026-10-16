package store

import (
	"context"
	"unicode/utf8"
)

// Ranges use zero-based UTF-8 byte offsets, with an exclusive end.
type NoteEditOperation struct {
	ID          string        `json:"operationID"`
	ItemID      string        `json:"itemID"`
	State       string        `json:"state"`
	From        int           `json:"from"`
	To          int           `json:"to"`
	ContentHash string        `json:"contentHash"`
	RevisionID  string        `json:"revisionID,omitempty"`
	Author      ContentAuthor `json:"author"`
	CreatedAt   int64         `json:"createdAt"`
	UpdatedAt   int64         `json:"updatedAt"`
	ExpiresAt   int64         `json:"expiresAt"`
	Sequence    int64         `json:"sequence"`
	LeaseToken  string        `json:"leaseToken,omitempty"`
	Target      string        `json:"target,omitempty"`
}

type NoteEditRequest struct {
	Action          string        `json:"action"`
	ClientRequestID string        `json:"clientRequestID"`
	OperationID     string        `json:"operationID,omitempty"`
	LeaseToken      string        `json:"leaseToken,omitempty"`
	ExpectedHash    string        `json:"expectedHash,omitempty"`
	From            int           `json:"from,omitempty"`
	To              int           `json:"to,omitempty"`
	Target          string        `json:"target,omitempty"`
	Author          ContentAuthor `json:"-"`
}

type NoteEditState struct {
	ContentHash string              `json:"contentHash"`
	Operations  []NoteEditOperation `json:"operations"`
}

type NoteEditOperationStore interface {
	ChangeNoteEdit(context.Context, string, NoteEditRequest) (*NoteEditOperation, error)
	GetNoteEditState(context.Context, string) (*NoteEditState, error)
}

type NoteLocked struct{ OperationID string }

func (e *NoteLocked) Error() string {
	return "document range is locked by another editing operation"
}

func ValidNoteRange(body string, from, to int) bool {
	return from >= 0 && to > from && to <= len(body) &&
		(from == 0 || utf8.RuneStart(body[from])) && (to == len(body) || utf8.RuneStart(body[to]))
}

// Map only disjoint changes. Boundary edits are conservative: deleting a newline
// or inserting at the edge can change the protected paragraph's structure.
func MapNoteRange(before, after string, from, to int) (int, int, bool) {
	if !ValidNoteRange(before, from, to) {
		return 0, 0, false
	}
	boundaries := []int{0}
	for at, r := range before {
		boundaries = append(boundaries, at+utf8.RuneLen(r))
	}
	shift := 0
	for _, c := range textChanges(before, after) {
		start, end := boundaries[c.start], boundaries[c.end]
		if start <= to && end >= from {
			return 0, 0, false
		}
		if end < from {
			shift += len(string(c.text)) - (end - start)
		}
	}
	return from + shift, to + shift, true
}

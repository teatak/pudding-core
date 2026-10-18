package store

import (
	"context"
	"unicode/utf8"
)

type NoteEditRange struct {
	From   int    `json:"from,omitempty"`
	To     int    `json:"to,omitempty"`
	Target string `json:"target,omitempty"`
}

// Ranges use zero-based UTF-8 byte offsets, with an exclusive end.
type NoteEditOperation struct {
	Ranges      []NoteEditRange `json:"ranges,omitempty"`
	ID          string          `json:"operationID"`
	ItemID      string          `json:"itemID"`
	State       string          `json:"state"`
	From        int             `json:"from"`
	To          int             `json:"to"`
	ContentHash string          `json:"contentHash"`
	RevisionID  string          `json:"revisionID,omitempty"`
	Author      ContentAuthor   `json:"author"`
	CreatedAt   int64           `json:"createdAt"`
	UpdatedAt   int64           `json:"updatedAt"`
	ExpiresAt   int64           `json:"expiresAt"`
	Sequence    int64           `json:"sequence"`
	LeaseToken  string          `json:"leaseToken,omitempty"`
	Target      string          `json:"target,omitempty"`
}

type NoteEditRequest struct {
	Ranges          []NoteEditRange `json:"ranges,omitempty"`
	Action          string          `json:"action"`
	ClientRequestID string          `json:"clientRequestID"`
	OperationID     string          `json:"operationID,omitempty"`
	LeaseToken      string          `json:"leaseToken,omitempty"`
	ExpectedHash    string          `json:"expectedHash,omitempty"`
	From            int             `json:"from,omitempty"`
	To              int             `json:"to,omitempty"`
	Target          string          `json:"target,omitempty"`
	Author          ContentAuthor   `json:"-"`
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
	ranges, conflict := MapNoteRanges(before, after, []NoteEditRange{{From: from, To: to}})
	if conflict >= 0 {
		return 0, 0, false
	}
	return ranges[0].From, ranges[0].To, true
}

// One diff for the entire batch, including unrelated concurrent reservations.
// The conflict index identifies the owning operation without repeating the diff.
func MapNoteRanges(before, after string, ranges []NoteEditRange) ([]NoteEditRange, int) {
	if len(ranges) == 0 {
		return ranges, -1
	}
	boundaries := []int{0}
	for at, r := range before {
		boundaries = append(boundaries, at+utf8.RuneLen(r))
	}
	changes := textChanges(before, after)
	out := append([]NoteEditRange(nil), ranges...)
	for i, r := range ranges {
		if !ValidNoteRange(before, r.From, r.To) {
			return nil, i
		}
		shift := 0
		for _, c := range changes {
			start, end := boundaries[c.start], boundaries[c.end]
			if start <= r.To && end >= r.From {
				return nil, i
			}
			if end < r.From {
				shift += len(string(c.text)) - (end - start)
			}
		}
		out[i].From += shift
		out[i].To += shift
	}
	return out, -1
}

// Legacy single-range operations remain readable without a migration. From/To
// cover the batch for older clients; current clients protect only its ranges.
func (op *NoteEditOperation) EditRanges() []NoteEditRange {
	if len(op.Ranges) > 0 {
		return op.Ranges
	}
	return []NoteEditRange{{From: op.From, To: op.To, Target: op.Target}}
}
func (op *NoteEditOperation) SetRanges(ranges []NoteEditRange) {
	op.From, op.To = ranges[0].From, ranges[len(ranges)-1].To
	if len(op.Ranges) > 0 || len(ranges) > 1 {
		op.Ranges = ranges
		op.Target = ""
	} else {
		op.Target = ranges[0].Target
	}
}

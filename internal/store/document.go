package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

const StudioItemKindDoc = "doc"

type ContentAuthor struct {
	Kind      string `json:"kind"`
	SessionID string `json:"sessionID,omitempty"`
	TurnID    string `json:"turnID,omitempty"`
}

type DocumentContent struct {
	ItemID      string `json:"itemID"`
	Body        string `json:"body"`
	ContentHash string `json:"contentHash"`
	RevisionID  string `json:"revisionID"`
}

type DocumentEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type DocumentWrite struct {
	ClientRequestID string         `json:"clientRequestID"`
	ExpectedHash    *string        `json:"expectedHash,omitempty"`
	Body            *string        `json:"body,omitempty"`
	Edits           []DocumentEdit `json:"edits,omitempty"`
	RestoreRevision string         `json:"restoreRevision,omitempty"`
	// PreserveOnly archives a dirty editor buffer before loading the latest body.
	// It never replaces the working content or changes its head.
	PreserveOnly bool          `json:"preserveOnly,omitempty"`
	Author       ContentAuthor `json:"-"`
}

type DocumentStore interface {
	CreateDocument(context.Context, *StudioItem, string, ContentAuthor) (*DocumentContent, error)
	GetDocument(context.Context, string) (*DocumentContent, error)
	WriteDocument(context.Context, string, DocumentWrite) (*DocumentContent, error)
}

type ContentConflict struct{ CurrentHash string }

func (e *ContentConflict) Error() string {
	return "document content changed; read the latest body before retrying"
}

type InvalidDocument struct{ Message string }

func (e *InvalidDocument) Error() string { return e.Message }

func DocumentHash(body string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(body))) }
func ValidateDocument(body string) error {
	if !utf8.ValidString(body) {
		return &InvalidDocument{"document must be UTF-8"}
	}
	if len(body) > contracts.Studio().MaxDocumentBytes {
		return &InvalidDocument{"document exceeds maximum size"}
	}
	return nil
}

// Resolve all anchors against the same body before applying any change. A failed
// or overlapping anchor rejects the whole operation, including earlier edits.
func ApplyDocumentEdits(body string, edits []DocumentEdit) (string, error) {
	if len(edits) == 0 || len(edits) > contracts.Studio().MaxDocumentEdits {
		return "", &InvalidDocument{"invalid edit count"}
	}
	type replacement struct {
		start, end int
		text       string
	}
	ranges := make([]replacement, 0, len(edits))
	for _, edit := range edits {
		index := strings.Index(body, edit.Old)
		if edit.Old == "" || index < 0 || strings.Contains(body[index+1:], edit.Old) {
			return "", &InvalidDocument{"each old text must match exactly once in the latest document"}
		}
		ranges = append(ranges, replacement{index, index + len(edit.Old), edit.New})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	var out strings.Builder
	cursor := 0
	for _, r := range ranges {
		if r.start < cursor {
			return "", &InvalidDocument{"document edits overlap"}
		}
		out.WriteString(body[cursor:r.start])
		out.WriteString(r.text)
		cursor = r.end
	}
	out.WriteString(body[cursor:])
	result := out.String()
	return result, ValidateDocument(result)
}

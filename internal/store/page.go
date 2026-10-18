package store

import (
	"context"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/contracts"
)

const (
	StudioItemKindPage = "page"
)

// PageContent is the UTF-8 Markdown view of a native content snapshot.
type PageContent = ContentSnapshot

type PageEdit struct {
	Old string `json:"old"`
	New string `json:"new"`
}

type PageWrite struct {
	Replacements    []string   `json:"replacements,omitempty"`
	OperationID     string     `json:"operationID,omitempty"`
	LeaseToken      string     `json:"leaseToken,omitempty"`
	Replacement     *string    `json:"replacement,omitempty"`
	ClientRequestID string     `json:"clientRequestID"`
	ExpectedHash    *string    `json:"expectedHash,omitempty"`
	Body            *string    `json:"body,omitempty"`
	Edits           []PageEdit `json:"edits,omitempty"`
	RestoreRevision string     `json:"restoreRevision,omitempty"`
	UndoRevision    string     `json:"undoRevision,omitempty"`
	// PreserveOnly archives a dirty editor buffer before loading the latest body.
	// It never replaces the working content or changes its head.
	PreserveOnly bool          `json:"preserveOnly,omitempty"`
	Author       ContentAuthor `json:"-"`
}

type PageStore interface {
	PageEditOperationStore
	CreatePage(context.Context, *StudioItem, string, ContentAuthor) (*PageContent, error)
	GetPage(context.Context, string) (*PageContent, error)
	WritePage(context.Context, string, PageWrite) (*PageContent, error)
}

func ValidatePage(body string) error {
	if !utf8.ValidString(body) {
		return &InvalidContent{"document must be UTF-8"}
	}
	if len(body) > contracts.Studio().MaxDocumentBytes {
		return &InvalidContent{"document exceeds maximum size"}
	}
	return nil
}

// Resolve all anchors against the same body before applying any change. A failed
// or overlapping anchor rejects the whole operation, including earlier edits.
func ApplyPageEdits(body string, edits []PageEdit) (string, error) {
	if len(edits) == 0 || len(edits) > contracts.Studio().MaxDocumentEdits {
		return "", &InvalidContent{"invalid edit count"}
	}
	type replacement struct {
		start, end int
		text       string
	}
	ranges := make([]replacement, 0, len(edits))
	for _, edit := range edits {
		index := strings.Index(body, edit.Old)
		if edit.Old == "" || index < 0 || strings.Contains(body[index+1:], edit.Old) {
			return "", &InvalidContent{"each old text must match exactly once in the latest document"}
		}
		ranges = append(ranges, replacement{index, index + len(edit.Old), edit.New})
	}
	sort.Slice(ranges, func(i, j int) bool { return ranges[i].start < ranges[j].start })
	var out strings.Builder
	cursor := 0
	for _, r := range ranges {
		if r.start < cursor {
			return "", &InvalidContent{"document edits overlap"}
		}
		out.WriteString(body[cursor:r.start])
		out.WriteString(r.text)
		cursor = r.end
	}
	out.WriteString(body[cursor:])
	result := out.String()
	return result, ValidatePage(result)
}

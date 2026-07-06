// Package tool owns tool declarations and execution.
package tool

import (
	"context"
	"encoding/json"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

type Call struct {
	SessionID     string
	TurnID        string
	CallID        string
	Name          string
	Args          json.RawMessage
	WorkspaceDirs []string
}

type Result struct {
	CallID      string
	Name        string
	Ok          bool
	Content     string
	Attachments []store.Attachment
	// ContextAttachments are appended to canonical turn output and replayed to the model.
	ContextAttachments []store.Attachment
	SummaryKind        string
	SummaryCount       int
}

const (
	SummaryReturnedFields = "returned_fields"
	SummaryReturnedItems  = "returned_items"
	SummaryReadChars      = "read_chars"
	SummaryReadFiles      = "read_files"
	SummaryChangedLines   = "changed_lines"
)

type Runner interface {
	Definitions(ctx context.Context, sessionID string) ([]provider.ToolDef, error)
	Call(ctx context.Context, call Call) Result
}

// Package tool owns tool declarations and execution.
package tool

import (
	"context"
	"encoding/json"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

type Call struct {
	SessionID       string
	TurnID          string
	CallID          string
	Name            string
	Args            json.RawMessage
	Mode            store.AgentMode
	ProjectDirs     []string
	CommandSandbox  CommandSandboxMode
	CommandStateKey string
}

type CommandSandboxMode string

const (
	// The zero value requests the project sandbox so production runners fail closed.
	CommandSandboxEnforce CommandSandboxMode = ""
	CommandSandboxBypass  CommandSandboxMode = "bypass"
)

type Result struct {
	CallID  string
	Name    string
	Ok      bool
	Content string
	// Attachments are user-visible tool artifacts persisted with the tool result.
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

type AppScopedDefinitionRunner interface {
	DefinitionsForApps(ctx context.Context, sessionID string, appIDs []string) ([]provider.ToolDef, error)
}

type ApprovalDetailsProvider interface {
	ApprovalDetails(ctx context.Context, call Call) (map[string]any, error)
}

type SessionResourceCleaner interface {
	CloseSession(sessionID string)
}

type ResourceCloser interface {
	Close() error
}

type BackgroundProcessController interface {
	ListBackgroundProcesses(sessionID string) []BackgroundProcessSnapshot
	BackgroundProcessCount(sessionID string) int
	ReadBackgroundProcess(sessionID, processID string, offset int64, maxBytes, tailBytes int) (BackgroundProcessLogSnapshot, error)
	StopBackgroundProcess(sessionID, processID string) (BackgroundProcessSnapshot, error)
}

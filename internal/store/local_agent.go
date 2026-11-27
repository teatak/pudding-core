package store

import "context"

// LocalAgentState is the durable link to the CLI-owned conversation. Pudding
// keeps display history; the agent owns its inference context and compaction.
type LocalAgentState struct {
	SessionID      string `json:"sessionID"`
	AgentSessionID string `json:"agentSessionID"`
	Executable     string `json:"executable"`
	CWD            string `json:"cwd"`
	// Versions of instructions delivered to this ACP session.
	GuideDigests    map[string]string `json:"guideDigests,omitempty"`
	LastTurnID      string            `json:"lastTurnID"`
	ContextUsed     int               `json:"contextUsed"`
	ContextSize     int               `json:"contextSize"`
	Image           bool              `json:"image"`
	Audio           bool              `json:"audio"`
	EmbeddedContext bool              `json:"embeddedContext"`
}

// LocalAgentContext records why this turn uses a newly created ACP context.
type LocalAgentContext struct {
	Reason string `json:"reason"` // first_use, directory_changed, session_unavailable
}

type LocalAgentStore interface {
	RecordLocalAgentContext(context.Context, string, string, LocalAgentContext) error
	GetLocalAgentState(context.Context, string) (*LocalAgentState, error)
	PutLocalAgentState(context.Context, *LocalAgentState) error
	LookupSubmission(context.Context, string, string) (*Turn, *QueuedInput, error)
}

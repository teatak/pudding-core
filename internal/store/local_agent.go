package store

import "context"

// LocalAgentState is the durable link to the CLI-owned conversation. Pudding
// keeps display history; the agent owns its inference context and compaction.
type LocalAgentState struct {
	SessionID      string `json:"sessionID"`
	AgentSessionID string `json:"agentSessionID"`
	Executable     string `json:"executable"`
	CWD            string `json:"cwd"`
	// Last acknowledged prompt, or the history boundary of a reviewed reset.
	LastTurnID string `json:"lastTurnID"`
	// A failed restore must not silently replace the CLI-owned context.
	NeedsHandoff    bool `json:"needsHandoff,omitempty"`
	ContextUsed     int  `json:"contextUsed"`
	ContextSize     int  `json:"contextSize"`
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}
type LocalAgentStore interface {
	GetLocalAgentState(context.Context, string) (*LocalAgentState, error)
	PutLocalAgentState(context.Context, *LocalAgentState) error
	LookupSubmission(context.Context, string, string) (*Turn, *QueuedInput, error)
}

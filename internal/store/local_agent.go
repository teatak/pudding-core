package store

import "context"

// LocalAgentState is the durable link to the CLI-owned conversation. Pudding
// keeps display history; the agent owns its inference context and compaction.
type LocalAgentState struct {
	SessionID       string `json:"sessionID"`
	AgentSessionID  string `json:"agentSessionID"`
	Executable      string `json:"executable"`
	CWD             string `json:"cwd"`
	LastTurnID      string `json:"lastTurnID"`
	ContextUsed     int    `json:"contextUsed"`
	ContextSize     int    `json:"contextSize"`
	Image           bool   `json:"image"`
	Audio           bool   `json:"audio"`
	EmbeddedContext bool   `json:"embeddedContext"`
}
type LocalAgentStore interface {
	GetLocalAgentState(context.Context, string) (*LocalAgentState, error)
	PutLocalAgentState(context.Context, *LocalAgentState) error
	LookupSubmission(context.Context, string, string) (*Turn, *QueuedInput, error)
}

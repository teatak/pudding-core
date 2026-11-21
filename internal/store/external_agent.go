package store

import "context"

type ExternalAgentBinding struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	RuntimeID string `json:"-"`
	Name      string `json:"name"`
}

type ExternalAgentStore interface {
	CreateAgentInvite(context.Context, ExternalAgentBinding, string, int64) error
	RedeemAgentInvite(ctx context.Context, inviteHash, credentialHash, name string, now int64) (*ExternalAgentBinding, error)
	AgentBinding(ctx context.Context, credentialHash string) (*ExternalAgentBinding, error)
	ListAgentBindings(ctx context.Context, sessionID string) ([]ExternalAgentBinding, error)
	RemoveAgentBinding(ctx context.Context, sessionID, id string) error
}

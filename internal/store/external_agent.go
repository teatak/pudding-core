package store

import "context"

type ExternalAgentBinding struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	RuntimeID string `json:"-"`
	ExternalAgentIdentity
}

type ExternalAgentIdentity struct {
	Name          string `json:"name"`
	Title         string `json:"title,omitempty"`
	IconPNGBase64 string `json:"iconPNGBase64,omitempty"`
}

type ExternalAgentStore interface {
	CreateAgentInvite(context.Context, ExternalAgentBinding, string, int64) error
	RedeemAgentInvite(ctx context.Context, inviteHash, credentialHash string, identity ExternalAgentIdentity, now int64) (*ExternalAgentBinding, error)
	AgentBinding(ctx context.Context, credentialHash string) (*ExternalAgentBinding, error)
	ListAgentBindings(ctx context.Context, sessionID string) ([]ExternalAgentBinding, error)
	RemoveAgentBinding(ctx context.Context, sessionID, id string) error
}

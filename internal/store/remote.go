package store

import (
	"context"
	"errors"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

var ErrInvalidRemote = errors.New("store: invalid remote access input")
var ErrRemoteUnauthorized = errors.New("store: remote credential unavailable or expired")
var ErrRemoteConflict = errors.New("store: remote pairing state conflict")

const RemotePairingLifetime = 5 * time.Minute
const RemoteDeviceLifetime = 30 * 24 * time.Hour

// RemoteScope identifies one browser host registration. Gateway injects these
// fields; a credential issued for LAN can never authorize a relay origin.
type RemoteScope struct {
	Mode   string `json:"mode"`
	Origin string `json:"origin"`
}

func (s RemoteScope) Validate() error {
	u, err := url.Parse(s.Origin)
	if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.String() != s.Origin || strings.ContainsAny(s.Origin, "\r\n") {
		return ErrInvalidRemote
	}
	switch s.Mode {
	case "lan":
		address, err := netip.ParseAddr(u.Hostname())
		if u.Scheme != "http" || err != nil || !address.Is4() {
			return ErrInvalidRemote
		}
	case "relay":
		if u.Scheme != "https" {
			return ErrInvalidRemote
		}
	default:
		return ErrInvalidRemote
	}
	return nil
}

type RemoteDevice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	RemoteScope
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type RemotePairing struct {
	ID string `json:"id"`
	RemoteScope
	Status     string    `json:"status"`
	DeviceName string    `json:"deviceName,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
}
type RemoteAccess struct {
	DesktopID string          `json:"desktopID"`
	Devices   []RemoteDevice  `json:"devices"`
	Pairings  []RemotePairing `json:"pairings"`
}
type RemotePairingCode struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type RemotePairingRequest struct {
	RemoteScope
	Code       string `json:"code"`
	DeviceName string `json:"deviceName"`
}
type RemotePairingRequested struct {
	ID        string `json:"id"`
	PollToken string `json:"pollToken"`
	Status    string `json:"status"`
}
type RemotePollInput struct {
	RemoteScope
	PollToken string `json:"pollToken"`
}
type RemotePollResult struct {
	Status string        `json:"status"`
	Token  string        `json:"token,omitempty"`
	Device *RemoteDevice `json:"device,omitempty"`
}
type RemoteAuthorizeInput struct {
	RemoteScope
	Token string `json:"token"`
}
type RemoteAuthorization struct {
	DesktopID string       `json:"desktopID"`
	Device    RemoteDevice `json:"device"`
}

// RemoteStore keeps canonical identity and credentials in SQLite. Live
// notifications only cause callers to re-read this authority.
type RemoteStore interface {
	RemoteAccess(context.Context) (*RemoteAccess, error)
	CreateRemotePairing(context.Context, RemoteScope) (*RemotePairingCode, error)
	RequestRemotePairing(context.Context, RemotePairingRequest) (*RemotePairingRequested, error)
	PollRemotePairing(context.Context, string, RemotePollInput) (*RemotePollResult, error)
	ApproveRemotePairing(context.Context, string) error
	DeleteRemotePairing(context.Context, string) error
	DeleteRemoteDevice(context.Context, string) error
	AuthorizeRemote(context.Context, RemoteAuthorizeInput) (*RemoteAuthorization, error)
}

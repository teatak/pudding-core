package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) registerRemote(app *cart.Engine) {
	app.Route("/remote/access").GET(s.remoteAccess)
	app.Route("/remote/models").GET(s.remoteModels)
	app.Route("/remote/pairings").POST(s.remoteCreatePairing)
	app.Route("/remote/pairings/request").POST(s.remoteRequestPairing)
	app.Route("/remote/pairings/:pairingID").DELETE(s.remoteDeletePairing)
	app.Route("/remote/pairings/:pairingID/poll").POST(s.remotePollPairing)
	app.Route("/remote/pairings/:pairingID/approve").POST(s.remoteApprovePairing)
	app.Route("/remote/pairings/:pairingID/deny").POST(s.remoteDeletePairing)
	app.Route("/remote/devices/:deviceID").DELETE(s.remoteDeleteDevice)
	app.Route("/remote/authorize").POST(s.remoteAuthorize)
	app.Route("/remote/events").GET(s.remoteEvents)
}
func (s *Server) remoteFail(c *cart.Context, err error) error {
	switch {
	case errors.Is(err, store.ErrInvalidRemote):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_remote_input"})
	case errors.Is(err, store.ErrRemoteUnauthorized):
		c.JSON(http.StatusUnauthorized, map[string]string{"error": "remote_unauthorized"})
	case errors.Is(err, store.ErrRemoteConflict):
		c.JSON(http.StatusConflict, map[string]string{"error": "remote_pairing_conflict"})
	default:
		return s.fail(c, err)
	}
	return nil
}
func (s *Server) remoteAccess(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	out, err := s.store.RemoteAccess(c.Request.Context())
	if err != nil {
		return s.remoteFail(c, err)
	}
	c.JSON(http.StatusOK, out)
	return nil
}
func (s *Server) remoteCreatePairing(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	var in store.RemoteScope
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	out, err := s.store.CreateRemotePairing(c.Request.Context(), in)
	if err != nil {
		return s.remoteFail(c, err)
	}
	s.notifyRemote(remoteChange{PairingID: out.ID})
	c.JSON(http.StatusCreated, out)
	return nil
}
func (s *Server) remoteRequestPairing(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	var in store.RemotePairingRequest
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	out, err := s.store.RequestRemotePairing(c.Request.Context(), in)
	if err != nil {
		return s.remoteFail(c, err)
	}
	s.notifyRemote(remoteChange{PairingID: out.ID})
	c.JSON(http.StatusOK, out)
	return nil
}
func (s *Server) remotePollPairing(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	id, _ := c.Param("pairingID")
	var in store.RemotePollInput
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	out, err := s.store.PollRemotePairing(c.Request.Context(), id, in)
	if err != nil {
		return s.remoteFail(c, err)
	}
	if out.Device != nil {
		s.notifyRemote(remoteChange{DeviceID: out.Device.ID, PairingID: id})
	}
	c.JSON(http.StatusOK, out)
	return nil
}
func (s *Server) remoteApprovePairing(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	id, _ := c.Param("pairingID")
	if err := s.store.ApproveRemotePairing(c.Request.Context(), id); err != nil {
		return s.remoteFail(c, err)
	}
	s.notifyRemote(remoteChange{PairingID: id})
	c.JSON(http.StatusOK, map[string]string{"status": "approved"})
	return nil
}
func (s *Server) remoteDeletePairing(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	id, _ := c.Param("pairingID")
	if err := s.store.DeleteRemotePairing(c.Request.Context(), id); err != nil {
		return s.remoteFail(c, err)
	}
	s.notifyRemote(remoteChange{PairingID: id})
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}
func (s *Server) remoteDeleteDevice(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	id, _ := c.Param("deviceID")
	if err := s.store.DeleteRemoteDevice(c.Request.Context(), id); err != nil {
		return s.remoteFail(c, err)
	}
	s.notifyRemote(remoteChange{DeviceID: id})
	c.Response.WriteHeader(http.StatusNoContent)
	return nil
}
func (s *Server) remoteAuthorize(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	var in store.RemoteAuthorizeInput
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid_json")
	}
	out, err := s.store.AuthorizeRemote(c.Request.Context(), in)
	if err != nil {
		return s.remoteFail(c, err)
	}
	c.JSON(http.StatusOK, out)
	return nil
}

type remoteModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	Label    string `json:"label,omitempty"`
}

func (s *Server) remoteModels(c *cart.Context) error {
	c.Response.Header().Set("Cache-Control", "no-store")
	profiles, err := s.config.ListProviderProfiles(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	models := []remoteModel{}
	for _, p := range profiles {
		for _, m := range p.Models {
			if !m.Unavailable {
				models = append(models, remoteModel{Provider: p.ProfileID(), Model: m.ID, Label: m.DisplayName})
			}
		}
	}
	c.JSON(http.StatusOK, map[string]any{"models": models})
	return nil
}

// These resource-scoped auth notifications are invalidations, not business
// history or an alternate credential authority. Reconnect requires a snapshot
// and reauthorization; session event sequencing is unchanged (constraints 3/12).
type remoteChange struct {
	DeviceID  string `json:"deviceID,omitempty"`
	PairingID string `json:"pairingID,omitempty"`
}

func (s *Server) notifyRemote(change remoteChange) {
	s.remoteMu.Lock()
	defer s.remoteMu.Unlock()
	for ch := range s.remoteSubscribers {
		select {
		case ch <- change:
		default:
			close(ch)
			delete(s.remoteSubscribers, ch)
		}
	}
}
func (s *Server) remoteEvents(c *cart.Context) error {
	ch := make(chan remoteChange, 16)
	s.remoteMu.Lock()
	if s.remoteSubscribers == nil {
		s.remoteSubscribers = map[chan remoteChange]struct{}{}
	}
	s.remoteSubscribers[ch] = struct{}{}
	s.remoteMu.Unlock()
	defer func() { s.remoteMu.Lock(); delete(s.remoteSubscribers, ch); s.remoteMu.Unlock() }()
	w := c.Response
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "event: remote.ready\ndata: {}\n\n")
	w.Flush()
	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	for {
		select {
		case <-c.Request.Context().Done():
			return nil
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			w.Flush()
		case change, ok := <-ch:
			if !ok {
				return nil
			}
			data, _ := json.Marshal(change)
			fmt.Fprintf(w, "event: remote.changed\ndata: %s\n\n", data)
			w.Flush()
		}
	}
}

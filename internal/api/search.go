package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	defaultSessionSearchLimit = 40
	maxSessionSearchLimit     = 100
	maxSessionSearchScope     = 200
)

type sessionMessageSearchRequest struct {
	SessionIDs []string `json:"sessionIDs"`
	Query      string   `json:"query"`
	Limit      int      `json:"limit"`
}

func (s *Server) searchSessionMessages(c *cart.Context) error {
	var req sessionMessageSearchRequest
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	query := strings.TrimSpace(req.Query)
	if query == "" {
		return badRequest(c, "query is required")
	}
	if len(req.SessionIDs) == 0 {
		return badRequest(c, "sessionIDs are required")
	}
	if len(req.SessionIDs) > maxSessionSearchScope {
		return badRequest(c, "too many sessionIDs")
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultSessionSearchLimit
	}
	if limit > maxSessionSearchLimit {
		limit = maxSessionSearchLimit
	}

	sessionIDs := make([]string, 0, len(req.SessionIDs))
	seen := make(map[string]struct{}, len(req.SessionIDs))
	for _, rawID := range req.SessionIDs {
		sessionID := strings.TrimSpace(rawID)
		if sessionID == "" {
			return badRequest(c, "invalid sessionID")
		}
		if _, ok := seen[sessionID]; ok {
			continue
		}
		seen[sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, sessionID)
	}

	messages := make([]*store.Message, 0, limit)
	for _, sessionID := range sessionIDs {
		if len(messages) >= limit {
			break
		}
		hits, err := s.store.SearchMessages(c.Request.Context(), store.MessageSearchInput{
			SessionID: sessionID,
			Query:     query,
			Limit:     limit,
			Literal:   true,
		})
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if errors.Is(err, store.ErrHistorySearchUnavailable) {
			c.JSON(http.StatusServiceUnavailable, map[string]string{"error": "history_search_unavailable"})
			return nil
		}
		if err != nil {
			return s.fail(c, err)
		}
		for _, message := range hits {
			if message.Role != store.RoleUser && message.Role != store.RoleAssistant {
				continue
			}
			if message.Kind != store.MessageKindText && message.Kind != store.MessageKindSummary {
				continue
			}
			messages = append(messages, message)
			// The dialog renders one best excerpt per session. SearchMessages already
			// returns relevance order, so do not replace it with the newest hit.
			break
		}
	}
	c.JSON(http.StatusOK, map[string]any{"messages": messages})
	return nil
}

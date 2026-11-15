package api

import (
	"context"
	"sync"

	"github.com/teatak/pudding-core/internal/browser"
)

type browserOpenContextKey struct{}
type browserOpenVersion struct {
	sessionID string
	version   uint64
}

func (s *Server) BeginBrowserOpen(ctx context.Context, sessionID string) (context.Context, func()) {
	s.browserMu.Lock()
	if s.browserToolOpens == nil {
		s.browserToolOpens = map[string]map[chan struct{}]struct{}{}
	}
	if s.browserToolOpens[sessionID] == nil {
		s.browserToolOpens[sessionID] = map[chan struct{}]struct{}{}
	}
	done := make(chan struct{})
	s.browserToolOpens[sessionID][done] = struct{}{}
	version := s.browserSessionVersions[sessionID]
	s.browserMu.Unlock()
	var once sync.Once
	return context.WithValue(ctx, browserOpenContextKey{}, browserOpenVersion{sessionID, version}), func() {
		once.Do(func() {
			s.browserMu.Lock()
			defer s.browserMu.Unlock()
			delete(s.browserToolOpens[sessionID], done)
			if len(s.browserToolOpens[sessionID]) == 0 {
				delete(s.browserToolOpens, sessionID)
			}
			close(done)
		})
	}
}

// An IPC update may arrive before the tool's Open RPC returns. Wait for its
// commit, without holding the state mutex or depending on a list/state GET.
func (s *Server) awaitBrowserToolOpen(ctx context.Context, sessionID, tabID string) error {
	for {
		s.browserMu.Lock()
		var pending chan struct{}
		if !s.browserTabAllowedLocked(sessionID, tabID) {
			for done := range s.browserToolOpens[sessionID] {
				pending = done
				break
			}
		}
		s.browserMu.Unlock()
		if pending == nil {
			return nil
		}
		select {
		case <-pending:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *Server) BrowserTabOpened(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	version, ok := ctx.Value(browserOpenContextKey{}).(browserOpenVersion)
	if !ok || version.sessionID != sessionID || version.version != s.browserSessionVersions[sessionID] {
		return browser.ErrTabNotFound
	}
	s.allowBrowserTabInGateLocked(sessionID, tab.ID)
	return s.syncBrowserStateLocked(ctx, sessionID, tab)
}

func (s *Server) BrowserTabUpdated(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	return s.syncBrowserState(ctx, sessionID, tab)
}

func (s *Server) BrowserTabClosed(ctx context.Context, sessionID, tabID string) error {
	return s.deleteClosedBrowserTabState(ctx, sessionID, tabID)
}

func (s *Server) BrowserSessionClosed(ctx context.Context, sessionID string, tabs []browser.TabSnapshot) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	for _, tab := range tabs {
		s.rememberClosedBrowserTabLocked(sessionID, tab.ID)
	}
	s.closeBrowserTabGateLocked(sessionID)
	return s.store.ClearBrowserState(ctx, sessionID)
}

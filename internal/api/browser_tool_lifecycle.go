package api

import (
	"context"
	"maps"
	"sync"

	"github.com/teatak/pudding-core/internal/browser"
)

type browserOpenContextKey struct{}
type browserToolOpenWait struct {
	count   int
	changed chan struct{}
}
type browserOpenVersion struct {
	sessionID string
	version   uint64
	tabs      map[string]uint64
}

func (s *Server) BeginBrowserOpen(ctx context.Context, sessionID string) (context.Context, func()) {
	s.browserMu.Lock()
	if s.browserToolOpens == nil {
		s.browserToolOpens = map[string]*browserToolOpenWait{}
	}
	if s.browserToolOpens[sessionID] == nil {
		s.browserToolOpens[sessionID] = &browserToolOpenWait{changed: make(chan struct{})}
	}
	wait := s.browserToolOpens[sessionID]
	wait.count++
	version := s.browserSessionVersions[sessionID]
	tabs := maps.Clone(s.browserTabVersions[sessionID])
	s.browserMu.Unlock()
	var once sync.Once
	return context.WithValue(ctx, browserOpenContextKey{}, browserOpenVersion{sessionID, version, tabs}), func() {
		once.Do(func() {
			s.browserMu.Lock()
			defer s.browserMu.Unlock()
			wait.count--
			close(wait.changed)
			if wait.count == 0 {
				delete(s.browserToolOpens, sessionID)
			} else {
				wait.changed = make(chan struct{})
			}
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
			if wait := s.browserToolOpens[sessionID]; wait != nil {
				pending = wait.changed
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

func (s *Server) checkBrowserTabAccess(ctx context.Context, sessionID, tabID string) error {
	if err := s.awaitBrowserToolOpen(ctx, sessionID, tabID); err != nil {
		return err
	}
	if !s.browserTabAllowed(sessionID, tabID) {
		return browser.ErrTabNotFound
	}
	return nil
}

func (s *Server) BrowserTabOpened(ctx context.Context, sessionID string, tab browser.TabSnapshot) error {
	s.browserMu.Lock()
	defer s.browserMu.Unlock()
	version, ok := ctx.Value(browserOpenContextKey{}).(browserOpenVersion)
	if !ok || version.sessionID != sessionID || version.version != s.browserSessionVersions[sessionID] ||
		version.tabs[tab.ID] != s.browserTabVersions[sessionID][tab.ID] {
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

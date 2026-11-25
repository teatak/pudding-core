package engine

import (
	"context"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/acp"
)

type localAgentQuotaCache struct {
	mu        sync.Mutex
	snapshot  acp.QuotaSnapshot
	expiresAt time.Time
}

func (e *Engine) LocalAgentQuota(ctx context.Context, refresh bool) (acp.QuotaSnapshot, error) {
	if !IsDesktopRequest(ctx) {
		return acp.QuotaSnapshot{}, ErrLocalAgentDesktop
	}
	cache := &e.localQuota
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return acp.QuotaSnapshot{}, err
	}
	if !refresh && time.Now().Before(cache.expiresAt) {
		return cache.snapshot, nil
	}
	paths := e.localAgentPaths
	if paths == nil {
		paths = acp.Candidates()
	}
	snapshot, err := acp.ReadQuota(ctx, paths)
	if err != nil {
		return acp.QuotaSnapshot{}, err
	}
	cache.snapshot = snapshot
	cache.expiresAt = time.Now().Add(time.Minute)
	return snapshot, nil
}

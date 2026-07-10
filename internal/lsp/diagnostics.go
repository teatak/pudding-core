package lsp

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type diagnosticsCache struct {
	mu         sync.RWMutex
	generation uint64
	byURI      map[string]DiagnosticSnapshot
	changed    chan struct{}
}

func newDiagnosticsCache() *diagnosticsCache {
	return &diagnosticsCache{byURI: map[string]DiagnosticSnapshot{}, changed: make(chan struct{})}
}

func (c *diagnosticsCache) update(raw json.RawMessage) {
	var params struct {
		URI         string       `json:"uri"`
		Version     *int         `json:"version,omitempty"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if err := json.Unmarshal(raw, &params); err != nil || params.URI == "" {
		return
	}
	c.mu.Lock()
	c.generation++
	c.byURI[params.URI] = DiagnosticSnapshot{
		URI:         params.URI,
		Version:     params.Version,
		Diagnostics: append([]Diagnostic(nil), params.Diagnostics...),
		Generation:  c.generation,
		UpdatedAt:   time.Now(),
	}
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

func (c *diagnosticsCache) get(uri string) (DiagnosticSnapshot, bool) {
	c.mu.RLock()
	snapshot, ok := c.byURI[uri]
	c.mu.RUnlock()
	if !ok {
		return DiagnosticSnapshot{}, false
	}
	snapshot.Diagnostics = append([]Diagnostic(nil), snapshot.Diagnostics...)
	return snapshot, true
}

func (c *diagnosticsCache) generationForURI(uri string) uint64 {
	c.mu.RLock()
	snapshot := c.byURI[uri]
	c.mu.RUnlock()
	return snapshot.Generation
}

func (c *diagnosticsCache) wait(ctx context.Context, uri string, afterGeneration uint64) (DiagnosticSnapshot, bool, error) {
	for {
		c.mu.RLock()
		snapshot, ok := c.byURI[uri]
		changed := c.changed
		if ok && snapshot.Generation > afterGeneration {
			snapshot.Diagnostics = append([]Diagnostic(nil), snapshot.Diagnostics...)
			c.mu.RUnlock()
			return snapshot, true, nil
		}
		c.mu.RUnlock()
		select {
		case <-ctx.Done():
			return DiagnosticSnapshot{}, false, ctx.Err()
		case <-changed:
		}
	}
}

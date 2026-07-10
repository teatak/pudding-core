package lsp

import (
	"encoding/json"
	"sync"
	"time"
)

type diagnosticsCache struct {
	mu         sync.RWMutex
	generation uint64
	byURI      map[string]DiagnosticSnapshot
}

func newDiagnosticsCache() *diagnosticsCache {
	return &diagnosticsCache{byURI: map[string]DiagnosticSnapshot{}}
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

package lsp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// OperationService scopes a multi-request tool call to one process generation.
// The lease serializes document overlays and prevents idle/capacity eviction.
// If the server exits, the caller must restart the whole operation, including
// document synchronization; retrying only its last RPC would lose didOpen state.
type OperationService interface {
	BeginOperation(ctx context.Context, spec ServerSpec) (Operation, error)
}

type Operation interface {
	Service
	Release()
	Abort()
}

type operation struct {
	manager  *Manager
	process  *Process
	once     sync.Once
	released atomic.Bool
}

func (m *Manager) BeginOperation(ctx context.Context, spec ServerSpec) (Operation, error) {
	for {
		process, err := m.Acquire(ctx, spec)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		entry := m.entries[process.spec.Key]
		pinned := entry != nil && entry.process == process && !entry.invalid.Load()
		if pinned {
			process.leases.Add(1)
		}
		m.mu.Unlock()
		if !pinned {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			continue
		}
		if err := lockContext(ctx, process.operationMu); err != nil {
			process.leases.Add(-1)
			return nil, err
		}
		op := &operation{manager: m, process: process}
		if err := op.check(ctx, spec); err != nil {
			op.Release()
			return nil, err
		}
		return op, nil
	}
}

func (o *operation) check(ctx context.Context, spec ServerSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if o.released.Load() || !o.process.Alive() {
		return fmt.Errorf("%w: language-server operation ended; retry the whole code query", ErrProcessClosed)
	}
	normalized, err := normalizeSpec(spec)
	if err != nil {
		return err
	}
	if !sameSpec(normalized, o.process.spec) {
		return ErrSpecConflict
	}
	o.manager.mu.Lock()
	entry := o.manager.entries[o.process.spec.Key]
	current := entry != nil && entry.process == o.process && !entry.invalid.Load()
	o.manager.mu.Unlock()
	if !current {
		return fmt.Errorf("%w: language workspace changed; retry the whole code query", ErrProcessClosed)
	}
	return nil
}

func (o *operation) Release() {
	o.once.Do(func() {
		o.released.Store(true)
		o.manager.touch(o.process.spec.Key, o.process)
		<-o.process.operationMu
		o.process.leases.Add(-1)
	})
}

// Abort discards speculative state if restoring an overlay fails. A later tool
// starts a clean server instead of querying text that was never saved to disk.
func (o *operation) Abort() {
	o.process.finish(ErrProcessClosed)
	o.process.forceTerminate()
	o.manager.discard(o.process.spec.Key, o.process)
}

func (o *operation) Request(ctx context.Context, spec ServerSpec, method string, params, result any) error {
	timeout := o.manager.requestTimeout
	if method == "textDocument/diagnostic" {
		timeout = o.manager.diagnosticTimeout
	}
	ctx, cancel := withTimeout(ctx, timeout)
	defer cancel()
	if err := o.check(ctx, spec); err != nil {
		return err
	}
	if err := o.manager.flushFileChanges(ctx, o.process); err != nil {
		return err
	}
	return o.process.Request(ctx, method, params, result)
}

func (o *operation) SyncDocument(ctx context.Context, spec ServerSpec, document Document) (DocumentState, error) {
	ctx, cancel := withTimeout(ctx, o.manager.requestTimeout)
	defer cancel()
	if err := o.check(ctx, spec); err != nil {
		return DocumentState{}, err
	}
	return o.process.SyncDocument(ctx, document)
}

func (o *operation) RefreshDocuments(ctx context.Context, spec ServerSpec, load DocumentLoader) (map[string]DocumentState, error) {
	ctx, cancel := withTimeout(ctx, o.manager.requestTimeout)
	defer cancel()
	if err := o.check(ctx, spec); err != nil {
		return nil, err
	}
	if err := o.manager.flushFileChanges(ctx, o.process); err != nil {
		return nil, err
	}
	return o.process.RefreshDocuments(ctx, load)
}

func (o *operation) PublishedDiagnostics(ctx context.Context, spec ServerSpec, uri string, afterGeneration uint64) (DiagnosticSnapshot, bool, error) {
	if err := o.check(ctx, spec); err != nil {
		return DiagnosticSnapshot{}, false, err
	}
	return o.process.diagnostics.wait(ctx, uri, afterGeneration)
}

func (o *operation) PositionEncoding(ctx context.Context, spec ServerSpec) (string, error) {
	if err := o.check(ctx, spec); err != nil {
		return "", err
	}
	return o.process.PositionEncoding(), nil
}

package tool

import (
	"context"

	"github.com/teatak/pudding-core/internal/lsp"
)

// Acquire lazily so input/path authorization still runs before starting a
// server. Every RPC in one tool call then uses the same exclusive process lease.
type codeOperationService struct {
	base lsp.Service
	op   lsp.Operation
}

func (s *codeOperationService) service(ctx context.Context, spec lsp.ServerSpec) (lsp.Service, error) {
	if s.op != nil {
		return s.op, nil
	}
	if manager, ok := s.base.(lsp.OperationService); ok {
		op, err := manager.BeginOperation(ctx, spec)
		if err != nil {
			return nil, err
		}
		s.op = op
		return op, nil
	}
	return s.base, nil
}

func (s *codeOperationService) Release() {
	if s.op != nil {
		s.op.Release()
	}
}

func (s *codeOperationService) Abort() {
	if s.op != nil {
		s.op.Abort()
	}
}

func (s *codeOperationService) RecordFileChanges(changes []lsp.FileChange) {
	if recorder, ok := s.base.(lsp.FileChangeRecorder); ok {
		recorder.RecordFileChanges(changes)
	}
}

func (s *codeOperationService) Request(ctx context.Context, spec lsp.ServerSpec, method string, params, result any) error {
	service, err := s.service(ctx, spec)
	if err != nil {
		return err
	}
	return service.Request(ctx, spec, method, params, result)
}

func (s *codeOperationService) SyncDocument(ctx context.Context, spec lsp.ServerSpec, document lsp.Document) (lsp.DocumentState, error) {
	service, err := s.service(ctx, spec)
	if err != nil {
		return lsp.DocumentState{}, err
	}
	return service.SyncDocument(ctx, spec, document)
}

func (s *codeOperationService) RefreshDocuments(ctx context.Context, spec lsp.ServerSpec, load lsp.DocumentLoader) (map[string]lsp.DocumentState, error) {
	service, err := s.service(ctx, spec)
	if err != nil {
		return nil, err
	}
	return service.RefreshDocuments(ctx, spec, load)
}

func (s *codeOperationService) PublishedDiagnostics(ctx context.Context, spec lsp.ServerSpec, uri string, generation uint64) (lsp.DiagnosticSnapshot, bool, error) {
	service, err := s.service(ctx, spec)
	if err != nil {
		return lsp.DiagnosticSnapshot{}, false, err
	}
	return service.PublishedDiagnostics(ctx, spec, uri, generation)
}

func (s *codeOperationService) PositionEncoding(ctx context.Context, spec lsp.ServerSpec) (string, error) {
	service, err := s.service(ctx, spec)
	if err != nil {
		return "", err
	}
	return service.PositionEncoding(ctx, spec)
}

type codeOperationContextKey struct{}

func (r *BuiltinRunner) codeLanguageService(ctx context.Context) lsp.Service {
	if service, ok := ctx.Value(codeOperationContextKey{}).(*codeOperationService); ok {
		return service
	}
	return r.languageService
}

func (r *BuiltinRunner) codeOperation(ctx context.Context, call Call) Result {
	if r.languageService != nil {
		service := &codeOperationService{base: r.languageService}
		defer service.Release()
		ctx = context.WithValue(ctx, codeOperationContextKey{}, service)
	}
	switch call.Name {
	case CodeSymbols:
		return r.codeSymbols(ctx, call)
	case CodeDefinition:
		return r.codeDefinition(ctx, call)
	case CodeReferences:
		return r.codeReferences(ctx, call)
	case CodeDiagnostics:
		return r.codeDiagnostics(ctx, call)
	default:
		return r.codeRename(ctx, call)
	}
}

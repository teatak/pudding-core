package lsp

import (
	"context"
	"crypto/sha256"
	"errors"
	"sort"
	"strings"
	"unicode/utf8"
)

const maxOpenDocuments = 128

type documentState struct {
	hash       [sha256.Size]byte
	languageID string
	version    int
	lastUsed   uint64
}

func (p *Process) SyncDocument(ctx context.Context, document Document) (DocumentState, error) {
	if err := lockContext(ctx, p.documentsMu); err != nil {
		return DocumentState{}, err
	}
	defer func() { <-p.documentsMu }()
	return p.syncDocumentLocked(ctx, document, true)
}

// RefreshDocuments only reloads the bounded set of documents already held by
// this process. Refreshing does not make a document more recently used: the next
// explicit SyncDocument still determines which documents are kept at capacity.
// A change or close invalidates diagnostics for every open document in the
// workspace, even if a dependent document's own text and version stay unchanged.
func (p *Process) RefreshDocuments(ctx context.Context, load DocumentLoader) (map[string]DocumentState, error) {
	if err := lockContext(ctx, p.documentsMu); err != nil {
		return nil, err
	}
	defer func() { <-p.documentsMu }()
	URIs := make([]string, 0, len(p.documents))
	generations := make(map[string]uint64, len(p.documents))
	for uri := range p.documents {
		URIs = append(URIs, uri)
		generations[uri] = p.diagnostics.generationForURI(uri)
	}
	sort.Strings(URIs)
	states := make(map[string]DocumentState, len(URIs))
	workspaceChanged := false
	for _, uri := range URIs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		text, keep, err := load(uri)
		if err != nil {
			return nil, err
		}
		if !keep {
			if err := p.closeDocumentLocked(ctx, uri); err != nil {
				return nil, err
			}
			workspaceChanged = true
			continue
		}
		state, err := p.syncDocumentLocked(ctx, Document{URI: uri, LanguageID: p.documents[uri].languageID, Text: text}, false)
		if err != nil {
			return nil, err
		}
		workspaceChanged = workspaceChanged || state.Changed
		states[uri] = state
	}
	for uri, state := range states {
		state.Changed = workspaceChanged
		state.PreviousDiagnosticGeneration = generations[uri]
		states[uri] = state
	}
	return states, nil
}

func (p *Process) closeDocumentLocked(ctx context.Context, uri string) error {
	p.diagnostics.invalidate()
	if err := p.NotifyContext(ctx, "textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": uri}}); err != nil {
		return err
	}
	delete(p.documents, uri)
	return nil
}

func (p *Process) syncDocumentLocked(ctx context.Context, document Document, touch bool) (DocumentState, error) {
	document.URI = strings.TrimSpace(document.URI)
	document.LanguageID = strings.TrimSpace(document.LanguageID)
	if document.URI == "" || document.LanguageID == "" {
		return DocumentState{}, errors.New("document URI and language ID are required")
	}
	if !utf8.ValidString(document.Text) {
		return DocumentState{}, errors.New("document text must be valid UTF-8")
	}
	hash := sha256.Sum256([]byte(document.Text))
	previousDiagnosticGeneration := p.diagnostics.generationForURI(document.URI)

	current, exists := p.documents[document.URI]
	lastUsed := current.lastUsed
	if touch || !exists {
		// Clock readings can tie, especially on Windows. A sequence under
		// documentsMu preserves actual access order without map-order ties.
		p.documentUse++
		lastUsed = p.documentUse
	}
	if exists && current.hash == hash && current.languageID == document.LanguageID {
		current.lastUsed = lastUsed
		p.documents[document.URI] = current
		return DocumentState{
			URI:                          document.URI,
			Version:                      current.version,
			Changed:                      false,
			PositionEncoding:             p.positionEncoding,
			PreviousDiagnosticGeneration: previousDiagnosticGeneration,
		}, nil
	}
	if exists && current.languageID != document.LanguageID {
		if err := p.closeDocumentLocked(ctx, document.URI); err != nil {
			return DocumentState{}, err
		}
		exists = false
	}
	if !exists && len(p.documents) >= maxOpenDocuments {
		oldestURI := ""
		var oldest uint64
		for uri, state := range p.documents {
			if oldestURI == "" || state.lastUsed < oldest {
				oldestURI = uri
				oldest = state.lastUsed
			}
		}
		if oldestURI != "" {
			if err := p.closeDocumentLocked(ctx, oldestURI); err != nil {
				return DocumentState{}, err
			}
		}
	}
	version := 1
	method := "textDocument/didOpen"
	params := any(map[string]any{
		"textDocument": map[string]any{
			"uri":        document.URI,
			"languageId": document.LanguageID,
			"version":    version,
			"text":       document.Text,
		},
	})
	if exists {
		version = current.version + 1
		method = "textDocument/didChange"
		params = map[string]any{
			"textDocument":   map[string]any{"uri": document.URI, "version": version},
			"contentChanges": []map[string]any{{"text": document.Text}},
		}
	}
	p.diagnostics.invalidate()
	if err := p.NotifyContext(ctx, method, params); err != nil {
		return DocumentState{}, err
	}
	p.documents[document.URI] = documentState{
		hash:       hash,
		languageID: document.LanguageID,
		version:    version,
		lastUsed:   lastUsed,
	}
	return DocumentState{
		URI:                          document.URI,
		Version:                      version,
		Changed:                      true,
		PositionEncoding:             p.positionEncoding,
		PreviousDiagnosticGeneration: previousDiagnosticGeneration,
	}, nil
}

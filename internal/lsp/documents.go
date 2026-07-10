package lsp

import (
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const maxOpenDocuments = 128

type documentState struct {
	hash       [sha256.Size]byte
	languageID string
	version    int
	lastUsed   time.Time
}

func (p *Process) SyncDocument(document Document) (DocumentState, error) {
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

	p.documentsMu.Lock()
	defer p.documentsMu.Unlock()
	current, exists := p.documents[document.URI]
	if exists && current.hash == hash && current.languageID == document.LanguageID {
		current.lastUsed = time.Now()
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
		if err := p.Notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": document.URI}}); err != nil {
			return DocumentState{}, err
		}
		delete(p.documents, document.URI)
		exists = false
	}
	if !exists && len(p.documents) >= maxOpenDocuments {
		oldestURI := ""
		var oldest time.Time
		for uri, state := range p.documents {
			if oldestURI == "" || state.lastUsed.Before(oldest) {
				oldestURI = uri
				oldest = state.lastUsed
			}
		}
		if oldestURI != "" {
			if err := p.Notify("textDocument/didClose", map[string]any{"textDocument": map[string]string{"uri": oldestURI}}); err != nil {
				return DocumentState{}, err
			}
			delete(p.documents, oldestURI)
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
	if err := p.Notify(method, params); err != nil {
		return DocumentState{}, err
	}
	p.documents[document.URI] = documentState{
		hash:       hash,
		languageID: document.LanguageID,
		version:    version,
		lastUsed:   time.Now(),
	}
	return DocumentState{
		URI:                          document.URI,
		Version:                      version,
		Changed:                      true,
		PositionEncoding:             p.positionEncoding,
		PreviousDiagnosticGeneration: previousDiagnosticGeneration,
	}, nil
}

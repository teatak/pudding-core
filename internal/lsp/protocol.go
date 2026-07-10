// Package lsp manages daemon-owned Language Server Protocol processes.
package lsp

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	DefaultMaxMessageBytes = 8 << 20
	DefaultMaxHeaderBytes  = 16 << 10
)

var (
	ErrClosed         = errors.New("lsp manager closed")
	ErrProcessClosed  = errors.New("lsp process closed")
	ErrRequestNotSent = errors.New("lsp request was not sent")
	ErrCapacity       = errors.New("lsp process capacity reached")
	ErrSpecConflict   = errors.New("lsp server spec conflicts with running process")
)

// ProcessKey identifies one daemon-shared language server.
type ProcessKey struct {
	LanguageRoot string
	ServerKind   string
}

// ServerSpec is resolved by trusted backend code, never from model input.
type ServerSpec struct {
	Key     ProcessKey
	Command string
	Args    []string
	Dir     string
	Env     []string
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Diagnostic struct {
	Range    Range           `json:"range"`
	Severity int             `json:"severity,omitempty"`
	Code     json.RawMessage `json:"code,omitempty"`
	Source   string          `json:"source,omitempty"`
	Message  string          `json:"message"`
}

type DiagnosticSnapshot struct {
	URI         string
	Version     *int
	Diagnostics []Diagnostic
	Generation  uint64
	UpdatedAt   time.Time
}

type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *ResponseError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("lsp response error %d: %s", e.Code, e.Message)
}

type wireMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ResponseError  `json:"error,omitempty"`
}

type Document struct {
	URI        string
	LanguageID string
	Text       string
}

type DocumentState struct {
	URI                          string
	Version                      int
	Changed                      bool
	PositionEncoding             string
	PreviousDiagnosticGeneration uint64
}

type workspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

type initializeParams struct {
	ProcessID        int                `json:"processId"`
	RootURI          string             `json:"rootUri"`
	ClientInfo       clientInfo         `json:"clientInfo"`
	Capabilities     clientCapabilities `json:"capabilities"`
	WorkspaceFolders []workspaceFolder  `json:"workspaceFolders"`
}

type clientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

type clientCapabilities struct {
	General struct {
		PositionEncodings []string `json:"positionEncodings"`
	} `json:"general"`
	Workspace struct {
		Symbol        struct{} `json:"symbol"`
		Configuration bool     `json:"configuration"`
		WorkspaceEdit struct {
			DocumentChanges bool `json:"documentChanges"`
		} `json:"workspaceEdit"`
	} `json:"workspace"`
	TextDocument struct {
		Definition struct{} `json:"definition"`
		References struct{} `json:"references"`
		Rename     struct {
			PrepareSupport bool `json:"prepareSupport"`
		} `json:"rename"`
		Diagnostic         struct{} `json:"diagnostic"`
		PublishDiagnostics struct {
			RelatedInformation bool `json:"relatedInformation"`
		} `json:"publishDiagnostics"`
	} `json:"textDocument"`
}

type initializeResult struct {
	Capabilities struct {
		PositionEncoding string `json:"positionEncoding"`
	} `json:"capabilities"`
}

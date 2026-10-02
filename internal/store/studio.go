package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrCanvasConflict = errors.New("canvas revision conflict")

type Canvas struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Icon            string            `json:"icon,omitempty"`
	IconColor       string            `json:"iconColor,omitempty"`
	SourceSessionID string            `json:"sourceSessionID,omitempty"`
	Revision        int64             `json:"revision"`
	HeadRevision    string            `json:"headRevision"`
	ActiveRevision  string            `json:"activeRevision"`
	Bindings        map[string]string `json:"bindings"`
	BindingVersion  int64             `json:"bindingVersion"`
	Deleted         bool              `json:"deleted"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type CanvasRevision struct {
	CanvasID        string          `json:"canvasID"`
	Hash            string          `json:"hash"`
	ParentRevision  string          `json:"parentRevision"`
	ClientRequestID string          `json:"clientRequestID"`
	CreatedAt       time.Time       `json:"createdAt"`
	BuildReceipt    json.RawMessage `json:"buildReceipt,omitempty"`
}
type CanvasStore interface {
	ListCanvasLinks(context.Context, string) ([]*CanvasLink, error)
	PutCanvasLink(context.Context, *CanvasLink, int64) error
	DeleteCanvasLink(context.Context, string, string, int64) error

	CreateCanvasAction(context.Context, *CanvasAction) (*CanvasAction, error)
	GetCanvasAction(context.Context, string, string) (*CanvasAction, error)
	ListCanvasActions(context.Context, string) ([]*CanvasAction, error)
	ClaimCanvasAction(context.Context, string, string) error
	FinishCanvasAction(context.Context, string, string, string, json.RawMessage) error

	// ListCanvases orders by UpdatedAt, the time of the latest saved revision.
	ListCanvases(context.Context) ([]*Canvas, error)
	GetCanvas(context.Context, string) (*Canvas, error)
	CreateCanvas(context.Context, *Canvas) (*Canvas, error)
	// UpdateCanvas changes metadata (appearance, bindings, active revision, deletion) and keeps
	// UpdatedAt: only a new revision is new work, so metadata changes do not reorder the list.
	UpdateCanvas(context.Context, *Canvas, int64) (*Canvas, error)
	SaveCanvasRevision(context.Context, *CanvasRevision, string) (*Canvas, error)
	ListCanvasRevisions(context.Context, string) ([]*CanvasRevision, error)
	GetCanvasRevision(context.Context, string, string) (*CanvasRevision, error)
	PutCanvasBuildReceipt(context.Context, string, string, json.RawMessage) error
}

// CanvasActionSpec is frozen before the trusted host asks for confirmation.
// It contains no connection credentials.
type CanvasActionSpec struct {
	RevisionHash       string          `json:"revisionHash"`
	ResourceRevision   int64           `json:"resourceRevision"`
	BindingVersion     int64           `json:"bindingVersion"`
	OperationID        string          `json:"operationID"`
	OperationHash      string          `json:"operationHash"`
	BindingFingerprint string          `json:"bindingFingerprint"`
	AppID              string          `json:"appID"`
	ConnectionID       string          `json:"connectionID"`
	Description        string          `json:"description"`
	Params             map[string]any  `json:"params"`
	Request            json.RawMessage `json:"request"`
}
type CanvasAction struct {
	ID              string           `json:"id"`
	CanvasID        string           `json:"canvasID"`
	ClientRequestID string           `json:"clientRequestID"`
	RequestHash     string           `json:"requestHash"`
	State           string           `json:"state"`
	Spec            CanvasActionSpec `json:"spec"`
	Result          json.RawMessage  `json:"result,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
}

type CanvasEntity struct {
	AppID        string `json:"appID"`
	ConnectionID string `json:"connectionID"`
	EntityType   string `json:"entityType"`
	EntityID     string `json:"entityID"`
}
type CanvasLink struct {
	ID        string       `json:"id"`
	CanvasID  string       `json:"canvasID"`
	Left      CanvasEntity `json:"left"`
	Right     CanvasEntity `json:"right"`
	CreatedAt time.Time    `json:"createdAt"`
}

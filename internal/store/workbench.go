package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrWorkbenchConflict = errors.New("workbench revision conflict")

type Workbench struct {
	ID              string                    `json:"id"`
	Name            string                    `json:"name"`
	SourceSessionID string                    `json:"sourceSessionID,omitempty"`
	Revision        int64                     `json:"revision"`
	HeadRevision    string                    `json:"headRevision"`
	ActiveRevision  string                    `json:"activeRevision"`
	Bindings        map[string]string         `json:"bindings"`
	Grants          map[string]WorkbenchGrant `json:"grants"`
	BindingVersion  int64                     `json:"bindingVersion"`
	Deleted         bool                      `json:"deleted"`
	CreatedAt       time.Time                 `json:"createdAt"`
	UpdatedAt       time.Time                 `json:"updatedAt"`
}

type WorkbenchGrant struct {
	OperationHash      string    `json:"operationHash"`
	BindingFingerprint string    `json:"bindingFingerprint"`
	ConnectionID       string    `json:"connectionID"`
	GrantedAt          time.Time `json:"grantedAt"`
}
type WorkbenchRevision struct {
	WorkbenchID     string          `json:"workbenchID"`
	Hash            string          `json:"hash"`
	ParentRevision  string          `json:"parentRevision"`
	ClientRequestID string          `json:"clientRequestID"`
	CreatedAt       time.Time       `json:"createdAt"`
	BuildReceipt    json.RawMessage `json:"buildReceipt,omitempty"`
}
type WorkbenchStore interface {
	ListWorkbenchLinks(context.Context, string) ([]*WorkbenchLink, error)
	PutWorkbenchLink(context.Context, *WorkbenchLink, int64) error
	DeleteWorkbenchLink(context.Context, string, string, int64) error

	CreateWorkbenchAction(context.Context, *WorkbenchAction) (*WorkbenchAction, error)
	GetWorkbenchAction(context.Context, string, string) (*WorkbenchAction, error)
	ListWorkbenchActions(context.Context, string) ([]*WorkbenchAction, error)
	ClaimWorkbenchAction(context.Context, string, string) error
	FinishWorkbenchAction(context.Context, string, string, string, json.RawMessage) error

	ListWorkbenches(context.Context) ([]*Workbench, error)
	GetWorkbench(context.Context, string) (*Workbench, error)
	CreateWorkbench(context.Context, *Workbench) (*Workbench, error)
	UpdateWorkbench(context.Context, *Workbench, int64) (*Workbench, error)
	SaveWorkbenchRevision(context.Context, *WorkbenchRevision, int64) (*Workbench, error)
	ListWorkbenchRevisions(context.Context, string) ([]*WorkbenchRevision, error)
	GetWorkbenchRevision(context.Context, string, string) (*WorkbenchRevision, error)
	PutWorkbenchBuildReceipt(context.Context, string, string, json.RawMessage) error
}

// WorkbenchActionSpec is frozen before the trusted host asks for confirmation.
// It contains no connection credentials.
type WorkbenchActionSpec struct {
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
type WorkbenchAction struct {
	ID              string              `json:"id"`
	WorkbenchID     string              `json:"workbenchID"`
	ClientRequestID string              `json:"clientRequestID"`
	RequestHash     string              `json:"requestHash"`
	State           string              `json:"state"`
	Spec            WorkbenchActionSpec `json:"spec"`
	Result          json.RawMessage     `json:"result,omitempty"`
	CreatedAt       time.Time           `json:"createdAt"`
}

type WorkbenchEntity struct {
	AppID        string `json:"appID"`
	ConnectionID string `json:"connectionID"`
	EntityType   string `json:"entityType"`
	EntityID     string `json:"entityID"`
}
type WorkbenchLink struct {
	ID          string          `json:"id"`
	WorkbenchID string          `json:"workbenchID"`
	Left        WorkbenchEntity `json:"left"`
	Right       WorkbenchEntity `json:"right"`
	CreatedAt   time.Time       `json:"createdAt"`
}

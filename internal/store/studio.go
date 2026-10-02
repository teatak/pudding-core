package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var ErrStudioItemConflict = errors.New("studio item revision conflict")

// StudioItemKindWidget is an LLM-authored React source package built by Desktop.
const StudioItemKindWidget = "widget"

type StudioItem struct {
	ID              string            `json:"id"`
	Kind            string            `json:"kind"`
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

type StudioItemRevision struct {
	ItemID          string          `json:"itemID"`
	Hash            string          `json:"hash"`
	ParentRevision  string          `json:"parentRevision"`
	ClientRequestID string          `json:"clientRequestID"`
	CreatedAt       time.Time       `json:"createdAt"`
	BuildReceipt    json.RawMessage `json:"buildReceipt,omitempty"`
}
type StudioStore interface {
	ListWidgetLinks(context.Context, string) ([]*WidgetLink, error)
	PutWidgetLink(context.Context, *WidgetLink, int64) error
	DeleteWidgetLink(context.Context, string, string, int64) error

	CreateWidgetAction(context.Context, *WidgetAction) (*WidgetAction, error)
	GetWidgetAction(context.Context, string, string) (*WidgetAction, error)
	ListWidgetActions(context.Context, string) ([]*WidgetAction, error)
	ClaimWidgetAction(context.Context, string, string) error
	FinishWidgetAction(context.Context, string, string, string, json.RawMessage) error

	// ListStudioItems orders by UpdatedAt, the time of the latest saved revision.
	ListStudioItems(context.Context) ([]*StudioItem, error)
	GetStudioItem(context.Context, string) (*StudioItem, error)
	CreateStudioItem(context.Context, *StudioItem) (*StudioItem, error)
	// UpdateStudioItem changes metadata (appearance, bindings, active revision, deletion) and keeps
	// UpdatedAt: only a new revision is new work, so metadata changes do not reorder the list.
	UpdateStudioItem(context.Context, *StudioItem, int64) (*StudioItem, error)
	SaveStudioItemRevision(context.Context, *StudioItemRevision, string) (*StudioItem, error)
	ListStudioItemRevisions(context.Context, string) ([]*StudioItemRevision, error)
	GetStudioItemRevision(context.Context, string, string) (*StudioItemRevision, error)
	PutWidgetBuildReceipt(context.Context, string, string, json.RawMessage) error
}

// WidgetActionSpec is frozen before the trusted host asks for confirmation.
// It contains no connection credentials.
type WidgetActionSpec struct {
	RevisionHash       string          `json:"revisionHash"`
	ResourceRevision   int64           `json:"resourceRevision"`
	BindingVersion     int64           `json:"bindingVersion"`
	OperationID        string          `json:"operationID"`
	OperationHash      string          `json:"operationHash"`
	BindingFingerprint string          `json:"bindingFingerprint"`
	PluginID           string          `json:"pluginID"`
	ConnectionID       string          `json:"connectionID"`
	Description        string          `json:"description"`
	Params             map[string]any  `json:"params"`
	Request            json.RawMessage `json:"request"`
}
type WidgetAction struct {
	ID              string           `json:"id"`
	ItemID          string           `json:"itemID"`
	ClientRequestID string           `json:"clientRequestID"`
	RequestHash     string           `json:"requestHash"`
	State           string           `json:"state"`
	Spec            WidgetActionSpec `json:"spec"`
	Result          json.RawMessage  `json:"result,omitempty"`
	CreatedAt       time.Time        `json:"createdAt"`
}

type WidgetEntity struct {
	PluginID     string `json:"pluginID"`
	ConnectionID string `json:"connectionID"`
	EntityType   string `json:"entityType"`
	EntityID     string `json:"entityID"`
}
type WidgetLink struct {
	ID        string       `json:"id"`
	ItemID    string       `json:"itemID"`
	Left      WidgetEntity `json:"left"`
	Right     WidgetEntity `json:"right"`
	CreatedAt time.Time    `json:"createdAt"`
}

// ProjectSpaceMount resolves a session mount against the canonical resource.
// The mount never owns another copy of the content.
func ProjectStudioMount(mount *StudioMount, w *StudioItem) *StudioMount {
	out := *mount
	out.ItemID, out.Revision = w.ID, w.Revision
	out.Title, out.SourceSessionID, out.UpdatedAt = w.Name, w.SourceSessionID, w.UpdatedAt
	out.Icon, out.IconColor = w.Icon, w.IconColor
	out.Kind = w.Kind
	return &out
}

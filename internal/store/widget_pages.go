package store

import (
	"context"
	"encoding/json"
)

// WidgetPage is a durable page instance. Its current target is a revocable lease,
// not its identity. Interaction stores the participant definition only, never work.
type WidgetPage struct {
	ID           string `json:"id"`
	ItemID       string `json:"itemID"`
	Scope        string `json:"scope"`
	RevisionHash string `json:"revisionHash"`
	TargetID     string `json:"targetID"`
	WidgetData
	Interaction json.RawMessage `json:"interaction,omitempty"`
}
type WidgetPageStore interface {
	SelectWidgetPage(context.Context, string, string, string) (string, error)
	AuthorizeWidgetPage(context.Context, string, string, string) error
	OpenWidgetPage(context.Context, string, string, string, string) (*WidgetPage, error)
	GetWidgetPageByTarget(context.Context, string) (*WidgetPage, error)
	WriteWidgetPage(context.Context, string, string, int64, json.RawMessage) (*WidgetData, error)
	SetWidgetPageInteraction(context.Context, string, json.RawMessage) error
	CloseWidgetPages(context.Context, string, string) error
}

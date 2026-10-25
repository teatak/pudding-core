package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrInvalidAssistant               = errors.New("invalid assistant input")
	ErrAssistantConflict              = errors.New("assistant changed")
	ErrAssistantConversationProtected = errors.New("assistant conversation cannot be archived or deleted")
	ErrAssistantToolUnavailable       = errors.New("assistant tools are only available in the fixed assistant conversation")
	ErrInvalidAssistantMemory         = errors.New("invalid assistant memory")
	ErrAssistantMemoryConflict        = errors.New("assistant memory changed; search again before updating or deleting")
)

// The local owner's assistant has one identity, independent of chat sessions.
const PersonalAssistantID = "personal"

// AssistantAppearance is a client-owned, versioned JSON object. Core does not
// interpret part IDs, palettes or renderer-specific fields.
type AssistantAppearance map[string]any

func (a AssistantAppearance) Validate() error {
	data, err := json.Marshal(a)
	if err != nil || a == nil || len(data) > 16*1024 {
		return ErrInvalidAssistant
	}
	var envelope struct {
		Version int64 `json:"version"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil || envelope.Version < 1 || envelope.Version > 9007199254740991 {
		return ErrInvalidAssistant
	}
	return nil
}

func (a AssistantAppearance) Equal(b AssistantAppearance) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && bytes.Equal(left, right)
}

type AssistantAppearanceFavorite struct {
	ID         string              `json:"id"`
	Appearance AssistantAppearance `json:"appearance"`
}

type Assistant struct {
	ID                  string                        `json:"id"`
	Name                string                        `json:"name"`
	UserName            string                        `json:"userName"`
	Appearance          AssistantAppearance           `json:"appearance"`
	AppearanceHistory   []AssistantAppearance         `json:"appearanceHistory"`
	AppearanceFavorites []AssistantAppearanceFavorite `json:"appearanceFavorites"`
	SessionID           string                        `json:"sessionID,omitempty"`
	Revision            int64                         `json:"revision"`
	CreatedAt           time.Time                     `json:"createdAt"`
	UpdatedAt           time.Time                     `json:"updatedAt"`
}

type AssistantMemory struct {
	ID              string    `json:"id"`
	Content         string    `json:"content"`
	Scope           string    `json:"scope"`
	Source          string    `json:"source"`
	ProjectID       string    `json:"projectID,omitempty"`
	Revision        int64     `json:"revision"`
	SourceSessionID string    `json:"sourceSessionID,omitempty"`
	SourceTurnID    string    `json:"sourceTurnID,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// A null assistant means it has not been opened yet. Reading does not initialize it.
type AssistantSnapshot struct {
	Assistant *Assistant        `json:"assistant"`
	Memories  []AssistantMemory `json:"memories"`
}

type SaveAssistantInput struct {
	AppearanceEditGroup     string              `json:"appearanceEditGroup,omitempty"`
	ExpectedRevision        *int64              `json:"expectedRevision"`
	Name                    string              `json:"name"`
	UserName                string              `json:"userName"`
	Appearance              AssistantAppearance `json:"appearance"`
	CommunicationPreference string              `json:"communicationPreference"`
}

func (in *SaveAssistantInput) Normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.UserName = strings.TrimSpace(in.UserName)
	in.CommunicationPreference = strings.TrimSpace(in.CommunicationPreference)
	if in.ExpectedRevision == nil || *in.ExpectedRevision < 0 || *in.ExpectedRevision > 9007199254740991 ||
		len(in.AppearanceEditGroup) > 128 ||
		utf8.RuneCountInString(in.Name) > 80 || strings.ContainsAny(in.Name, "\r\n") ||
		utf8.RuneCountInString(in.UserName) > 80 || strings.ContainsAny(in.UserName, "\r\n") ||
		utf8.RuneCountInString(in.CommunicationPreference) > 2000 {
		return ErrInvalidAssistant
	}
	return in.Appearance.Validate()
}

type AssistantStore interface {
	FinishAssistantAppearanceEdit(context.Context, string) (*AssistantSnapshot, error)
	SaveAssistantAppearanceFavorite(context.Context, AssistantAppearance) (*AssistantSnapshot, error)
	DeleteAssistantAppearanceFavorite(context.Context, string) (*AssistantSnapshot, error)
	PrepareAssistantDeletion(context.Context, string) error
	DeleteAssistant(context.Context) error
	GetAssistant(context.Context) (*AssistantSnapshot, error)
	SaveAssistant(context.Context, SaveAssistantInput) (*AssistantSnapshot, error)
	OpenAssistant(context.Context, string, string, string, AssistantAppearance) (*AssistantSnapshot, error)
	SearchAssistantMemories(context.Context, string, AssistantMemoryQuery) ([]AssistantMemory, error)
	SaveAssistantMemory(context.Context, string, AssistantMemoryWrite) (*AssistantMemory, error)
	DeleteAssistantMemory(context.Context, string, string, int64) error
}

type AssistantMemoryQuery struct {
	ID        string // Exact lookup instead of a text query.
	Query     string
	ProjectID string // Empty selects personal memories; otherwise selects only this project.
	Limit     int
}

type AssistantMemoryWrite struct {
	ID               string
	ExpectedRevision int64 // Zero creates; updates use the revision returned by search.
	Content          string
	ProjectID        string
	SourceTurnID     string // Set by the engine, never supplied by the model.
}

func (in *AssistantMemoryWrite) Normalize() error {
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" || utf8.RuneCountInString(in.Content) > 2000 ||
		in.ExpectedRevision < 0 || in.ExpectedRevision > 9007199254740991 ||
		(in.ID == "" && in.ExpectedRevision != 0) || in.SourceTurnID == "" ||
		(in.ID == "communication_preference" && in.ProjectID != "") {
		return ErrInvalidAssistantMemory
	}
	return nil
}

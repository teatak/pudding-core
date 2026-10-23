package store

import (
	"context"
	"errors"
	"slices"
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

type AssistantAppearance struct {
	Palette     string `json:"palette"`
	Body        string `json:"body"`
	Eyes        string `json:"eyes"`
	Mouth       string `json:"mouth"`
	Headwear    string `json:"headwear"`
	Arms        string `json:"arms"`
	Eyewear     string `json:"eyewear"`
	FaceSticker string `json:"faceSticker"`
}

type Assistant struct {
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	UserName   string              `json:"userName"`
	Appearance AssistantAppearance `json:"appearance"`
	SessionID  string              `json:"sessionID,omitempty"`
	Revision   int64               `json:"revision"`
	CreatedAt  time.Time           `json:"createdAt"`
	UpdatedAt  time.Time           `json:"updatedAt"`
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
		utf8.RuneCountInString(in.Name) > 80 || strings.ContainsAny(in.Name, "\r\n") ||
		utf8.RuneCountInString(in.UserName) > 80 || strings.ContainsAny(in.UserName, "\r\n") ||
		utf8.RuneCountInString(in.CommunicationPreference) > 2000 {
		return ErrInvalidAssistant
	}
	switch in.Appearance.Palette {
	case "original", "lavender", "mint", "blue", "coral", "yellow", "violet", "emerald":
	default:
		return ErrInvalidAssistant
	}
	a := in.Appearance
	// Core parts are required. Decorative slots always carry an explicit selection,
	// including "none"; only assets supported by the current renderer are accepted.
	if !slices.Contains([]string{"classic", "round", "square"}, a.Body) || !slices.Contains([]string{"classic", "curved", "dots"}, a.Eyes) ||
		!slices.Contains([]string{"classic", "smile", "surprised"}, a.Mouth) ||
		!slices.Contains([]string{"none", "antenna", "hat", "beacon"}, a.Headwear) ||
		!slices.Contains([]string{"none", "classic", "short", "orb"}, a.Arms) ||
		!slices.Contains([]string{"none", "classic", "goggles"}, a.Eyewear) ||
		!slices.Contains([]string{"none", "blush", "freckles", "stars"}, a.FaceSticker) {
		return ErrInvalidAssistant
	}
	return nil
}

type AssistantStore interface {
	GetAssistant(context.Context) (*AssistantSnapshot, error)
	SaveAssistant(context.Context, SaveAssistantInput) (*AssistantSnapshot, error)
	OpenAssistant(context.Context, string, string, string) (*AssistantSnapshot, error)
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

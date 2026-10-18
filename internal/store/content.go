package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

type ContentAuthor struct {
	Kind      string `json:"kind"`
	SessionID string `json:"sessionID,omitempty"`
	TurnID    string `json:"turnID,omitempty"`
}

// ContentSnapshot exposes a compatibility body and version envelope. Pages
// persist Structure and derive Markdown Body; Sheets keep their JSON bodies.
type ContentSnapshot struct {
	ItemID      string        `json:"itemID"`
	Body        string        `json:"body"`
	ContentHash string        `json:"contentHash"`
	RevisionID  string        `json:"revisionID"`
	Structure   *PageDocument `json:"structure,omitempty"`
}

type ContentConflict struct{ CurrentHash string }

func (e *ContentConflict) Error() string {
	return "document content changed; read the latest body before retrying"
}

type InvalidContent struct{ Message string }

func (e *InvalidContent) Error() string { return e.Message }

func ContentHash(body string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(body))) }
func ValidContentHash(hash string) bool {
	b, e := hex.DecodeString(hash)
	return e == nil && len(b) == 32 && hash == strings.ToLower(hash)
}

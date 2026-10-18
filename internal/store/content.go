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

// ContentSnapshot is the persisted UTF-8 body and version envelope shared by
// Pages (Markdown) and Sheets (JSON bodies). It is not an editor model.
type ContentSnapshot struct {
	ItemID      string `json:"itemID"`
	Body        string `json:"body"`
	ContentHash string `json:"contentHash"`
	RevisionID  string `json:"revisionID"`
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

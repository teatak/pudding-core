package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CanvasContent is retained only for the v24-to-v25 source conversion.
type CanvasContent struct {
	Kind  string          `json:"kind"`
	Title string          `json:"title"`
	Item  json.RawMessage `json:"item"`
}

// CanvasResourceID gives legacy items distinct, repeatable migration identities.
func CanvasResourceID(sessionID, itemID string) string {
	h := sha256.Sum256([]byte(sessionID + "\x00" + itemID))
	return "canvas_" + hex.EncodeToString(h[:])
}

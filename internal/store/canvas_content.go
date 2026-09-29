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

// ProjectCanvasItem resolves a session mount against the canonical resource.
// The mount never owns another copy of the content.
func ProjectCanvasItem(mount *CanvasItem, w *Canvas) *CanvasItem {
	out := *mount
	out.ResourceID, out.Revision = w.ID, w.Revision
	out.Title, out.SourceSessionID, out.UpdatedAt = w.Name, w.SourceSessionID, w.UpdatedAt
	out.Icon, out.IconColor = w.Icon, w.IconColor
	out.Kind = "app"
	return &out
}

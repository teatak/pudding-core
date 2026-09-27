package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// CanvasContent is the structured renderer's immutable revision payload.
// App revisions use the existing sandbox source package instead.
type CanvasContent struct {
	Kind  string          `json:"kind"`
	Title string          `json:"title"`
	Item  json.RawMessage `json:"item"`
}

func (c CanvasContent) Encode() (json.RawMessage, string, error) {
	if strings.TrimSpace(c.Kind) == "" || len(c.Kind) > 100 {
		return nil, "", ErrInvalidCanvas
	}
	if len(c.Item) == 0 || len(c.Item) > 2*1024*1024 || !json.Valid(c.Item) || len(c.Title) > 200 {
		return nil, "", ErrInvalidCanvas
	}
	b, err := json.Marshal(c)
	h := sha256.Sum256(b)
	return b, hex.EncodeToString(h[:]), err
}

// CanvasResourceID gives legacy items distinct, repeatable migration identities.
func CanvasResourceID(sessionID, itemID string) string {
	h := sha256.Sum256([]byte(sessionID + "\x00" + itemID))
	return "canvas_" + hex.EncodeToString(h[:])
}

// ProjectCanvasItem resolves a session mount against the canonical resource.
// The mount never owns another copy of the content.
func ProjectCanvasItem(mount *CanvasItem, w *Workbench, r *WorkbenchRevision) (*CanvasItem, error) {
	out := *mount
	out.ResourceID, out.Revision = w.ID, w.Revision
	out.Title, out.SourceSessionID, out.UpdatedAt = w.Name, w.SourceSessionID, w.UpdatedAt
	out.Kind, out.Item = "app", json.RawMessage(`{}`)
	if r != nil && len(r.Content) > 0 {
		var c CanvasContent
		if err := json.Unmarshal(r.Content, &c); err != nil {
			return nil, err
		}
		out.Kind, out.Item = c.Kind, append(json.RawMessage(nil), c.Item...)
	}
	return &out, nil
}

package memstore

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"github.com/teatak/pudding-core/internal/store"
)

func (m *Memstore) canvasProjection(v *store.CanvasItem) (*store.CanvasItem, error) {
	w := m.workbenches[v.ResourceID]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	hash := w.ActiveRevision
	if hash == "" {
		hash = w.HeadRevision
	}
	return store.ProjectCanvasItem(v, w, m.workbenchRevisions[w.ID+"/"+hash])
}
func (m *Memstore) ListCanvasItems(_ context.Context, session string) ([]*store.CanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session] == nil {
		return nil, store.ErrNotFound
	}
	out := []*store.CanvasItem{}
	for _, v := range m.canvas {
		if v.SessionID == session {
			if w := m.workbenches[v.ResourceID]; w != nil && !w.Deleted {
				item, err := m.canvasProjection(v)
				if err != nil {
					return nil, err
				}
				out = append(out, item)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}
func (m *Memstore) PutCanvasItem(_ context.Context, in store.CanvasItemInput) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemInput(&in); err != nil {
		return nil, err
	}
	content, hash, err := (store.CanvasContent{Kind: in.Kind, Title: in.Title, Item: in.Item}).Encode()
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[in.ActorSessionID] == nil {
		return nil, store.ErrNotFound
	}
	now := time.Now().UTC()
	key := canvasMapKey(in.ActorSessionID, in.ID)
	mount := m.canvas[key]
	expected := in.ExpectedRevision
	if mount == nil {
		if expected != 0 {
			return nil, store.ErrWorkbenchConflict
		}
		id := store.NewID("canvas")
		m.workbenches[id] = &store.Workbench{ID: id, Name: in.Title, SourceSessionID: in.ActorSessionID, Revision: 1, BindingVersion: 1, Bindings: map[string]string{}, Grants: map[string]store.WorkbenchGrant{}, CreatedAt: now, UpdatedAt: now}
		mount = &store.CanvasItem{ID: in.ID, SessionID: in.ActorSessionID, CanvasID: store.DefaultCanvasID, ResourceID: id, Window: append(json.RawMessage(nil), in.Window...), Visible: true, CreatedAt: now}
		m.canvas[key] = mount
		expected = 1
	}
	if _, err := m.saveCanvasRevisionLocked(&store.WorkbenchRevision{WorkbenchID: mount.ResourceID, Hash: hash, Content: content, ClientRequestID: store.NewID("edit"), CreatedAt: now}, expected); err != nil {
		return nil, err
	}
	return m.canvasProjection(mount)
}
func (m *Memstore) UpdateCanvasItemWindow(_ context.Context, p store.CanvasItemWindowPatch) (*store.CanvasItem, error) {
	if err := store.NormalizeCanvasItemWindowPatch(&p); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[p.ActorSessionID] == nil {
		return nil, store.ErrNotFound
	}
	v := m.canvas[canvasMapKey(p.ActorSessionID, p.ItemID)]
	if v == nil {
		return nil, store.ErrNotFound
	}
	v.Window = append(json.RawMessage(nil), p.Window...)
	return m.canvasProjection(v)
}
func (m *Memstore) DeleteCanvasItem(_ context.Context, session, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session] == nil {
		return store.ErrNotFound
	}
	key := canvasMapKey(session, id)
	if m.canvas[key] == nil {
		return store.ErrNotFound
	}
	delete(m.canvas, key)
	return nil
}
func (m *Memstore) OpenCanvasResource(_ context.Context, session, id, itemID string) (*store.CanvasItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessions[session] == nil {
		return nil, store.ErrNotFound
	}
	w := m.workbenches[id]
	if w == nil || w.Deleted {
		return nil, store.ErrNotFound
	}
	for _, v := range m.canvas {
		if v.SessionID == session && v.ResourceID == id {
			v.Visible = true
			return m.canvasProjection(v)
		}
	}
	key := canvasMapKey(session, itemID)
	if m.canvas[key] != nil {
		return nil, store.ErrWorkbenchConflict
	}
	v := &store.CanvasItem{ID: itemID, SessionID: session, CanvasID: store.DefaultCanvasID, ResourceID: id, Visible: true, CreatedAt: time.Now().UTC()}
	m.canvas[key] = v
	return m.canvasProjection(v)
}

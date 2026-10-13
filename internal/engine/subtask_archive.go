package engine

import (
	"context"
	"errors"

	"github.com/teatak/pudding-core/internal/store"
)

// An empty childID archives all eligible completed tasks. This is deliberately
// opt-in; old conversations are never cleaned up when enforcing dispatch limits.
func (e *Engine) SetSubtaskArchived(ctx context.Context, parentID, childID string, archived bool) error {
	e.collaborationMu.Lock()
	defer e.collaborationMu.Unlock()
	if childID == "" && !archived {
		return store.ErrInvalidSessionRelation
	}
	if _, err := e.store.GetSession(ctx, parentID); err != nil {
		return err
	}
	owner, err := e.store.ParentSessionID(ctx, parentID)
	if err != nil {
		return err
	}
	if owner != "" {
		return store.ErrInvalidSessionRelation
	}
	children, err := e.ListChildSessions(ctx, parentID)
	if err != nil {
		return err
	}
	found := childID == ""
	for _, child := range children {
		if childID != "" && child.Session.ID != childID {
			continue
		}
		found = true
		if archived && child.ArchivedAt != nil {
			continue
		}
		if archived && !child.CanArchive {
			if childID != "" {
				return store.ErrSubtaskArchiveBusy
			}
			continue
		}
		err := e.store.SetSubtaskArchived(ctx, parentID, child.Session.ID, child.LatestTurnID, archived)
		// A batch skips a child that acquired work after the snapshot. A single
		// action surfaces the conflict so the user can inspect its current state.
		if childID == "" && errors.Is(err, store.ErrSubtaskArchiveBusy) {
			continue
		}
		if err != nil {
			return err
		}
	}
	if !found {
		return store.ErrInvalidSessionRelation
	}
	return nil
}

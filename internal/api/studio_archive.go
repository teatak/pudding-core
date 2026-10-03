package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

func (s *Server) archiveStudioItem(c *cart.Context) error { return s.setStudioItemArchived(c, true) }
func (s *Server) restoreStudioItem(c *cart.Context) error { return s.setStudioItemArchived(c, false) }

func (s *Server) setStudioItemArchived(c *cart.Context, archived bool) error {
	id, _ := c.Param("itemID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	item, err := s.store.SetStudioItemArchived(c.Request.Context(), id, req.ExpectedRevision, archived)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, item)
	return nil
}

func (s *Server) purgeStudioItem(ctx context.Context, id string, expected int64) error {
	widget.DraftMu.Lock()
	defer widget.DraftMu.Unlock()
	// Validate the managed resource path before recording irreversible intent.
	revisions, err := widget.RevisionsDir(s.home, id)
	if err != nil {
		return err
	}
	if _, err := s.store.MarkStudioItemDeleted(ctx, id, expected); err != nil {
		return err
	}
	if err := os.RemoveAll(filepath.Dir(revisions)); err != nil {
		return err
	}
	return s.store.PurgeDeletedStudioItem(ctx, id)
}

func (s *Server) purgeExpiredStudioArchives(ctx context.Context, now time.Time) error {
	items, err := s.store.ListStudioItemsForCleanup(ctx, now.Add(-archiveRetention))
	if err != nil {
		return err
	}
	var cleanupErr error
	for _, item := range items {
		// Restoration or another edit after the scan changes the revision, so
		// cleanup cannot remove a resource that is active again.
		if err := s.purgeStudioItem(ctx, item.ID, item.Revision); err != nil && !errors.Is(err, store.ErrNotFound) && !errors.Is(err, store.ErrStudioItemConflict) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("purge studio item %s: %w", item.ID, err))
		}
	}
	return cleanupErr
}

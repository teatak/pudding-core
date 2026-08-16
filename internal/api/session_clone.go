package api

import (
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

type cloneSessionReq struct {
	ThroughMessageID string `json:"throughMessageID"`
	TitleSuffix      string `json:"titleSuffix"`
}

func (s *Server) cloneSession(c *cart.Context) error {
	sourceSessionID, _ := c.Param("id")
	var req cloneSessionReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	req.ThroughMessageID = strings.TrimSpace(req.ThroughMessageID)
	if req.ThroughMessageID == "" {
		return badRequest(c, "throughMessageID is required")
	}
	if strings.TrimSpace(req.TitleSuffix) == "" {
		return badRequest(c, "titleSuffix is required")
	}

	messages, err := s.store.ListMessages(c.Request.Context(), sourceSessionID, 0)
	if err != nil {
		return s.fail(c, err)
	}
	prefix, ok := store.MessagesThroughBoundary(messages, req.ThroughMessageID)
	if !ok {
		return s.fail(c, store.ErrNotFound)
	}

	targetSessionID := store.NewID("sess")
	attachmentService := attachment.NewService(s.home)
	replacements := make(map[string]store.Attachment)
	copied := make([]store.Attachment, 0)
	for _, message := range prefix {
		for _, item := range store.AttachmentsFromParts(message.Parts) {
			if _, exists := replacements[item.AttachmentKey]; exists {
				continue
			}
			next, err := attachmentService.CopyToSession(sourceSessionID, targetSessionID, item)
			if err != nil {
				cleanupClonedAttachments(attachmentService, targetSessionID, copied)
				return s.fail(c, err)
			}
			replacements[item.AttachmentKey] = next
			copied = append(copied, next)
		}
	}

	cloned, err := s.store.CloneSession(c.Request.Context(), store.CloneSessionInput{
		SourceSessionID:        sourceSessionID,
		ThroughMessageID:       req.ThroughMessageID,
		TargetSessionID:        targetSessionID,
		TitleSuffix:            req.TitleSuffix,
		AttachmentReplacements: replacements,
	})
	if err != nil {
		cleanupClonedAttachments(attachmentService, targetSessionID, copied)
		return s.fail(c, err)
	}
	s.enrichSessionProcesses(cloned)
	c.JSON(http.StatusCreated, cloned)
	return nil
}

func cleanupClonedAttachments(service *attachment.Service, sessionID string, items []store.Attachment) {
	for _, item := range items {
		_ = service.Delete(sessionID, item.AttachmentKey)
	}
}

package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/skill"
)

type skillService interface {
	ListSkills(context.Context) ([]skill.Skill, error)
	DeleteSkill(context.Context, string) error
	ReadAsset(context.Context, string) ([]byte, string, error)
}

func (s *Server) listSkills(c *cart.Context) error {
	if s.skills == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "skill_service_unavailable"})
		return nil
	}
	skills, err := s.skills.ListSkills(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"skills": skills})
	return nil
}

func (s *Server) deleteSkill(c *cart.Context) error {
	if s.skills == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "skill_service_unavailable"})
		return nil
	}
	id, _ := c.Param("id")
	if err := s.skills.DeleteSkill(c.Request.Context(), id); err != nil {
		if errors.Is(err, skill.ErrInvalidID) {
			return badRequest(c, "invalid skill id")
		}
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) getSkillAsset(c *cart.Context) error {
	if s.skills == nil {
		c.JSON(http.StatusInternalServerError, map[string]string{"error": "skill_service_unavailable"})
		return nil
	}
	rel, _ := c.Param("path")
	data, contentType, err := s.skills.ReadAsset(c.Request.Context(), rel)
	if err != nil {
		if errors.Is(err, skill.ErrInvalidAsset) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "skill_asset_not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.Header("Cache-Control", "private, max-age=300")
	c.Data(http.StatusOK, contentType, data)
	return nil
}

package api

import (
	"errors"
	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/store"
	"net/http"
	"strconv"
)

func (s *Server) scheduledFail(c *cart.Context, err error) error {
	if errors.Is(err, store.ErrInvalidSchedule) {
		return badRequest(c, err.Error())
	}
	if errors.Is(err, store.ErrScheduleConflict) || errors.Is(err, store.ErrScheduledTaskBusy) {
		c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
		return nil
	}
	return s.fail(c, err)
}
func (s *Server) listScheduledTasks(c *cart.Context) error {
	tasks, err := s.engine.ScheduledTasks(c.Request.Context(), c.Request.URL.Query().Get("sessionID"), c.Request.URL.Query().Get("includeDeleted") == "true")
	if err != nil {
		return s.scheduledFail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"tasks": tasks})
	return nil
}
func (s *Server) createScheduledTask(c *cart.Context) error {
	var in store.ScheduledTaskCreate
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid json body")
	}
	task, err := s.engine.CreateScheduledTask(c.Request.Context(), in)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	c.JSON(http.StatusCreated, task)
	return nil
}
func (s *Server) getScheduledTask(c *cart.Context) error {
	id, _ := c.Param("taskID")
	task, err := s.store.GetScheduledTask(c.Request.Context(), id)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	if task.Deleted {
		return s.fail(c, store.ErrNotFound)
	}
	c.JSON(http.StatusOK, task)
	return nil
}
func (s *Server) updateScheduledTask(c *cart.Context) error {
	id, _ := c.Param("taskID")
	var in store.ScheduledTaskUpdate
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid json body")
	}
	if c.Request.Method == http.MethodDelete {
		in.Delete = true
	}
	task, err := s.engine.UpdateScheduledTask(c.Request.Context(), id, in)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	c.JSON(http.StatusOK, task)
	return nil
}
func (s *Server) runScheduledTask(c *cart.Context) error {
	id, _ := c.Param("taskID")
	var in struct {
		RequestID string `json:"requestID"`
	}
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid json body")
	}
	run, err := s.engine.RunScheduledTask(c.Request.Context(), id, in.RequestID)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	c.JSON(http.StatusAccepted, run)
	return nil
}
func (s *Server) listScheduledTaskRuns(c *cart.Context) error {
	id, _ := c.Param("taskID")
	if _, err := s.store.GetScheduledTask(c.Request.Context(), id); err != nil {
		return s.scheduledFail(c, err)
	}
	offset := 0
	if value := c.Request.URL.Query().Get("offset"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return badRequest(c, "invalid offset")
		}
		offset = n
	}
	runs, err := s.engine.ScheduledRuns(c.Request.Context(), id, 51, offset)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	hasMore := len(runs) > 50
	if hasMore {
		runs = runs[:50]
	}
	c.JSON(http.StatusOK, map[string]any{"runs": runs, "hasMore": hasMore})
	return nil
}
func (s *Server) getScheduledTaskRun(c *cart.Context) error {
	sessionID, _ := c.Param("id")
	runID, _ := c.Param("runID")
	r, err := s.store.GetScheduledTaskRun(c.Request.Context(), sessionID, runID)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	v, err := s.engine.ScheduledRun(c.Request.Context(), r)
	if err != nil {
		return s.scheduledFail(c, err)
	}
	c.JSON(http.StatusOK, v)
	return nil
}

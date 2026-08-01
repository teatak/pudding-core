package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/githubapp"
	"github.com/teatak/pudding-core/internal/store"
)

type readyConnectionService interface {
	ReadyConnection(ctx context.Context, id string) (*app.Connection, error)
}

type putProjectAppBindingReq struct {
	AppID        string            `json:"appID"`
	ConnectionID string            `json:"connectionID"`
	ResourceType string            `json:"resourceType"`
	ResourceID   string            `json:"resourceID"`
	ResourceName string            `json:"resourceName"`
	Metadata     map[string]string `json:"metadata"`
	Primary      bool              `json:"primary"`
}

func (s *Server) listProjectAppBindings(c *cart.Context) error {
	projectID, _ := c.Param("id")
	bindings, err := s.store.ListProjectAppBindings(c.Request.Context(), projectID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"bindings": bindings})
	return nil
}

func (s *Server) putProjectAppBinding(c *cart.Context) error {
	projectID, _ := c.Param("id")
	var req putProjectAppBindingReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	req.AppID = strings.TrimSpace(req.AppID)
	req.ConnectionID = strings.TrimSpace(req.ConnectionID)
	req.ResourceType = strings.TrimSpace(req.ResourceType)
	req.ResourceID = strings.TrimSpace(req.ResourceID)
	if req.AppID != "github" || req.ResourceType != "repository" {
		return badRequest(c, "project app resource is not supported")
	}
	connection, installations, err := s.githubConnectionResources(c.Request.Context(), req.ConnectionID)
	if err != nil {
		if errors.Is(err, app.ErrConnectionReauthorizationRequired) {
			c.JSON(http.StatusConflict, map[string]string{"error": "connection_reauthorization_required"})
			return nil
		}
		return s.fail(c, err)
	}
	var matchedInstallationID, matchedOwner, matchedHTMLURL, matchedDefaultBranch, matchedPrivate string
	resourceName := ""
	for _, installation := range installations {
		for _, repository := range installation.Repositories {
			if repository.ID != req.ResourceID {
				continue
			}
			matchedInstallationID = installation.ID
			matchedOwner = installation.Account.Login
			matchedHTMLURL = repository.HTMLURL
			matchedDefaultBranch = repository.DefaultBranch
			if repository.Private {
				matchedPrivate = "true"
			}
			resourceName = repository.FullName
			break
		}
	}
	if resourceName == "" {
		return badRequest(c, "repository is not authorized for this connection")
	}
	existing, err := s.store.ListProjectAppBindings(c.Request.Context(), projectID)
	if err != nil {
		return s.fail(c, err)
	}
	primary := req.Primary
	hasGitHubBinding := false
	bindingID := store.NewID("binding")
	for _, item := range existing {
		if item.AppID == "github" {
			hasGitHubBinding = true
		}
		if item.AppID == "github" && item.ResourceType == "repository" && item.ResourceID == req.ResourceID {
			bindingID = item.ID
			if !req.Primary {
				primary = item.Primary
			}
		}
	}
	if !hasGitHubBinding {
		primary = true
	}
	metadata := map[string]string{
		"installationID": matchedInstallationID,
		"owner":          matchedOwner,
		"htmlURL":        matchedHTMLURL,
		"defaultBranch":  matchedDefaultBranch,
		"private":        matchedPrivate,
	}
	if rootDir := strings.TrimSpace(req.Metadata["rootDir"]); rootDir != "" {
		metadata["rootDir"] = rootDir
	}
	binding := &store.ProjectAppBinding{
		ID: bindingID, ProjectID: projectID, AppID: "github", ConnectionID: connection.ID,
		ResourceType: "repository", ResourceID: req.ResourceID, ResourceName: resourceName, Metadata: metadata, Primary: primary,
	}
	if err := s.store.PutProjectAppBinding(c.Request.Context(), binding); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, binding)
	return nil
}

func (s *Server) deleteProjectAppBinding(c *cart.Context) error {
	projectID, _ := c.Param("id")
	bindingID, _ := c.Param("bindingID")
	bindings, err := s.store.ListProjectAppBindings(c.Request.Context(), projectID)
	if err != nil {
		return s.fail(c, err)
	}
	wasPrimary := false
	appID := ""
	for _, binding := range bindings {
		if binding.ID == bindingID {
			wasPrimary = binding.Primary
			appID = binding.AppID
			break
		}
	}
	if err := s.store.DeleteProjectAppBinding(c.Request.Context(), projectID, bindingID); err != nil {
		return s.fail(c, err)
	}
	if wasPrimary {
		remaining, err := s.store.ListProjectAppBindings(c.Request.Context(), projectID)
		if err != nil {
			return s.fail(c, err)
		}
		for _, binding := range remaining {
			if binding.AppID == appID {
				binding.Primary = true
				if err := s.store.PutProjectAppBinding(c.Request.Context(), binding); err != nil {
					return s.fail(c, err)
				}
				break
			}
		}
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) listGitHubConnectionRepositories(c *cart.Context) error {
	connectionID, _ := c.Param("id")
	connection, installations, err := s.githubConnectionResources(c.Request.Context(), connectionID)
	if err != nil {
		if errors.Is(err, app.ErrConnectionReauthorizationRequired) {
			c.JSON(http.StatusConflict, map[string]string{"error": "connection_reauthorization_required"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{
		"account":       connection.Account,
		"installations": installations,
	})
	return nil
}

func (s *Server) githubConnectionResources(ctx context.Context, connectionID string) (*app.Connection, []githubapp.Installation, error) {
	ready, ok := s.apps.(readyConnectionService)
	if !ok {
		return nil, nil, errors.New("app connection auth service unavailable")
	}
	connection, err := ready.ReadyConnection(ctx, strings.TrimSpace(connectionID))
	if err != nil {
		return nil, nil, err
	}
	if connection.AppID != "github" || connection.Auth.Type != app.AuthTypeOAuth2 || connection.Auth.MethodID != app.GitHubAppAuthMethodID || connection.Auth.Variant != app.GitHubAppAuthVariant {
		return nil, nil, app.ErrConnectionReauthorizationRequired
	}
	installations, err := s.github.Installations(ctx, connection.Auth.AccessToken)
	if err != nil {
		return nil, nil, err
	}
	return connection, installations, nil
}

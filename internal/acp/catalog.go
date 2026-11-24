package acp

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Choice struct {
	Value string `json:"value"`
	Name  string `json:"name"`
}

type CatalogModel struct {
	ID               string   `json:"id"`
	DisplayName      string   `json:"displayName"`
	ReasoningOptions []Choice `json:"reasoningOptions"`
	DefaultReasoning string   `json:"defaultReasoning"`
}

type Catalog struct {
	Status          string         `json:"status"`
	Models          []CatalogModel `json:"models"`
	Image           bool           `json:"image"`
	Audio           bool           `json:"audio"`
	EmbeddedContext bool           `json:"embeddedContext"`
}

// Discover only configures a disposable session: no prompts, project files or
// Pudding MCP credentials are sent, and live conversations are never modified.
func Discover(ctx context.Context, paths []string, cwd string) (Catalog, error) {
	out := Catalog{Status: "not_installed", Models: []CatalogModel{}}
	if len(paths) == 0 {
		return out, nil
	}
	c, err := Open(ctx, paths, cwd)
	if err != nil {
		return out, err
	}
	defer c.Close()
	session, err := c.Session(ctx, cwd, "")
	if err != nil {
		return out, err
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = c.CloseSession(closeCtx, session.SessionID)
	}()
	var models []Choice
	for _, option := range session.ConfigOptions {
		if option.ID == "model" && option.Type == "select" {
			for _, choice := range option.Options {
				if choice.Value == "" {
					return out, errors.New("ACP model option has an empty value")
				}
				models = append(models, Choice{Value: choice.Value, Name: choice.Name})
			}
		}
	}
	if len(models) == 0 {
		return out, errors.New("Antigravity returned no selectable models")
	}
	for _, model := range models {
		options, err := c.Configure(ctx, session.SessionID, model.Value, "")
		if err != nil {
			return out, fmt.Errorf("read model %s: %w", model.Value, err)
		}
		entry := CatalogModel{ID: model.Value, DisplayName: model.Name, ReasoningOptions: []Choice{}}
		for _, option := range options {
			if option.ID == "thought_level" && option.Type == "select" {
				entry.DefaultReasoning = option.CurrentValue
				for _, choice := range option.Options {
					entry.ReasoningOptions = append(entry.ReasoningOptions, Choice{Value: choice.Value, Name: choice.Name})
				}
			}
		}
		out.Models = append(out.Models, entry)
	}
	out.Status = "ready"
	out.Image, out.Audio, out.EmbeddedContext = c.Init.Capabilities.Prompt.Image, c.Init.Capabilities.Prompt.Audio, c.Init.Capabilities.Prompt.EmbeddedContext
	return out, nil
}

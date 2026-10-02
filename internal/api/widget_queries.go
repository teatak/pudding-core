package api

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/contracts"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/pluginexec"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

type widgetEndpointResolver interface {
	ResolveBoundEndpoint(context.Context, string, string, string) (*plugin.EndpointBinding, string, error)
}

type widgetEndpointLister interface {
	ListEndpointBindings(context.Context, string) ([]*plugin.EndpointBinding, error)
}

var errWidgetConnectionSelectionRequired = errors.New("connection selection required")

func (s *Server) WithPluginExecutor(executor *pluginexec.Executor) *Server {
	s.pluginHTTP = executor
	return s
}

func (s *Server) bindWidget(c *cart.Context) error {
	id, _ := c.Param("itemID")
	var req struct {
		ExpectedRevision int64             `json:"expectedRevision"`
		RevisionHash     string            `json:"revisionHash"`
		Bindings         map[string]string `json:"bindings"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetStudioItem(c.Request.Context(), id)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if _, err = s.store.GetStudioItemRevision(c.Request.Context(), id, req.RevisionHash); err != nil {
		return s.studioItemError(c, err)
	}
	p, err := widget.ReadPackage(s.home, id, req.RevisionHash)
	if err != nil {
		return s.studioItemError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.studioItemError(c, err)
	}
	resolver, ok := s.plugins.(widgetEndpointResolver)
	if !ok {
		return badRequest(c, "App connections unavailable")
	}
	for slot, connectionID := range req.Bindings {
		source, ok := m.Sources[slot]
		if !ok {
			return badRequest(c, "undeclared source slot")
		}
		if _, _, err := resolver.ResolveBoundEndpoint(c.Request.Context(), source.PluginID, source.Endpoint, connectionID); err != nil {
			return badRequest(c, "binding unavailable: "+err.Error())
		}
	}
	if req.Bindings == nil {
		req.Bindings = map[string]string{}
	}
	if !reflect.DeepEqual(w.Bindings, req.Bindings) {
		w.Bindings = req.Bindings
		w.BindingVersion++
	}
	w, err = s.store.UpdateStudioItem(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.studioItemError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

func (s *Server) widgetOperation(ctx context.Context, id, hash, operationID string) (*store.StudioItem, widget.Operation, *plugin.EndpointBinding, string, error) {
	w, err := s.store.GetStudioItem(ctx, id)
	if err != nil {
		return nil, widget.Operation{}, nil, "", err
	}
	if _, err = s.store.GetStudioItemRevision(ctx, id, hash); err != nil {
		return nil, widget.Operation{}, nil, "", err
	}
	p, err := widget.ReadPackage(s.home, id, hash)
	if err != nil {
		return nil, widget.Operation{}, nil, "", err
	}
	m, _, err := p.Validate()
	if err != nil {
		return nil, widget.Operation{}, nil, "", err
	}
	op, ok := m.Operations[operationID]
	if !ok {
		return nil, op, nil, "", store.ErrNotFound
	}
	source := m.Sources[op.Source]
	binding, fingerprint, err := s.resolveWidgetSource(ctx, w, op.Source, source, op.Kind)
	if err != nil {
		return nil, op, nil, "", err
	}
	if binding.Endpoint.Kind != op.Kind {
		return nil, op, nil, "", fmt.Errorf("endpoint kind changed")
	}
	return w, op, binding, fingerprint, nil
}

func (s *Server) resolveWidgetSource(ctx context.Context, w *store.StudioItem, slot string, source widget.Source, kind string) (*plugin.EndpointBinding, string, error) {
	resolver, ok := s.plugins.(widgetEndpointResolver)
	if !ok {
		return nil, "", fmt.Errorf("App connections unavailable")
	}
	connectionID, bound := w.Bindings[slot]
	if !bound {
		lister, ok := s.plugins.(widgetEndpointLister)
		if !ok {
			return nil, "", fmt.Errorf("binding_unavailable")
		}
		available, err := lister.ListEndpointBindings(ctx, kind)
		if err != nil {
			return nil, "", err
		}
		matches := 0
		for _, candidate := range available {
			if candidate.PluginID == source.PluginID && candidate.EndpointName == source.Endpoint {
				connectionID = candidate.ConnectionID
				matches++
			}
		}
		if matches != 1 {
			return nil, "", errWidgetConnectionSelectionRequired
		}
	}
	binding, identity, err := resolver.ResolveBoundEndpoint(ctx, source.PluginID, source.Endpoint, connectionID)
	if err != nil {
		return nil, "", err
	}
	return binding, fmt.Sprintf("%x", sha256.Sum256([]byte(identity))), nil
}

func (s *Server) queryWidget(c *cart.Context) error {
	id, _ := c.Param("itemID")
	operationID, _ := c.Param("operationID")
	var req struct {
		RevisionHash   string         `json:"revisionHash"`
		BindingVersion int64          `json:"bindingVersion"`
		Params         map[string]any `json:"params"`
	}
	if err := decodeStudioRequest(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, op, binding, _, err := s.widgetOperation(c.Request.Context(), id, req.RevisionHash, operationID)
	if err != nil {
		return s.studioItemError(c, err)
	}
	if !op.SafeRead() {
		c.JSON(http.StatusForbidden, map[string]string{"error": "operation_requires_confirmation"})
		return nil
	}
	if w.BindingVersion != req.BindingVersion {
		return s.studioItemError(c, store.ErrStudioItemConflict)
	}
	resolved, err := op.ResolveRequest(req.Params)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if err = pluginexec.ValidateBoundRequest(binding, resolved.Method, resolved.Query, resolved.Body); err != nil {
		return badRequest(c, err.Error())
	}
	release, ok := s.acquireWidgetRequest(id)
	if !ok {
		c.Header("Retry-After", "30")
		c.JSON(http.StatusTooManyRequests, map[string]any{"error": "request_limit", "retryAfterMS": 30000})
		return nil
	}
	defer release()
	response := s.executeWidgetRequest(c.Request.Context(), binding, op, resolved)
	data, err := widgetResponseData(op, response)
	if status, _ := response["status"].(int); status == http.StatusTooManyRequests {
		retry := 30 * time.Second
		if headers, ok := response["response_headers"].(map[string]any); ok {
			for key, value := range headers {
				if strings.EqualFold(key, "Retry-After") {
					text, _ := value.(string)
					if seconds, e := strconv.Atoi(text); e == nil && seconds > 0 {
						retry = time.Duration(seconds) * time.Second
					} else if until, e := http.ParseTime(text); e == nil && time.Until(until) > 0 {
						retry = time.Until(until)
					}
				}
			}
		}
		if retry > 24*time.Hour {
			retry = 24 * time.Hour
		}
		c.Header("Retry-After", strconv.Itoa(int(retry.Seconds())))
		c.JSON(http.StatusTooManyRequests, map[string]any{"error": "upstream_rate_limited", "retryAfterMS": retry.Milliseconds()})
		return nil
	}
	if err != nil {
		c.JSON(http.StatusBadGateway, map[string]string{"error": "upstream_error", "detail": err.Error()})
		return nil
	}
	c.JSON(http.StatusOK, map[string]any{"data": data, "requestID": store.NewID("query"), "revisionHash": req.RevisionHash, "bindingVersion": w.BindingVersion, "fetchedAt": time.Now().UTC()})
	return nil
}

func widgetResponseData(op widget.Operation, response map[string]any) (any, error) {
	if response["ok"] != true {
		return nil, fmt.Errorf("Plugin request failed: %v", response["reason"])
	}
	status, _ := response["status"].(int)
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("Plugin returned HTTP %d", status)
	}
	if response["body_truncated"] == true {
		return nil, fmt.Errorf("Plugin response exceeds size limit")
	}
	var data any
	if op.Kind == "graphql" {
		if errs, ok := response["errors"].([]any); ok && len(errs) > 0 {
			return nil, fmt.Errorf("GraphQL returned errors")
		}
		data = response["data"]
	} else {
		var exists bool
		data, exists = response["body_json"]
		if !exists {
			return nil, fmt.Errorf("Plugin response is not JSON")
		}
	}
	if len(op.Result.Schema) > 0 {
		if err := widget.ValidateInput(op.Result.Schema, data); err != nil {
			return nil, fmt.Errorf("response_schema_mismatch: %w", err)
		}
	}
	for _, p := range []string{op.Result.Rows, op.Result.Total, op.Result.Cursor} {
		if p != "" {
			if _, err := widget.Pointer(data, p); err != nil {
				return nil, fmt.Errorf("response_schema_mismatch: %w", err)
			}
		}
	}
	if condition := op.Result.Success; condition != nil {
		value, err := widget.Pointer(data, condition.Pointer)
		if err != nil || !reflect.DeepEqual(value, condition.Equals) {
			return nil, fmt.Errorf("Plugin business success condition failed")
		}
	}
	return data, nil
}

func (s *Server) executeWidgetRequest(ctx context.Context, binding *plugin.EndpointBinding, op widget.Operation, r widget.Request) map[string]any {
	limit := contracts.Widget().MaxResponseBytes
	if op.Kind == "rest" {
		return s.pluginHTTP.REST(ctx, binding, map[string]any{"method": r.Method, "path": r.Path, "query": r.Query, "body_json": r.Body}, limit)
	}
	return s.pluginHTTP.GraphQL(ctx, binding, map[string]any{"query": r.Document, "operationName": r.OperationName, "variables": r.Variables}, limit)
}

type widgetRequestBudget struct {
	window        time.Time
	count, active int
}

func (s *Server) acquireWidgetRequest(id string) (func(), bool) {
	s.widgetMu.Lock()
	defer s.widgetMu.Unlock()
	now := time.Now()
	policy := contracts.Widget()
	if s.widgetBudget == nil {
		s.widgetBudget = map[string]*widgetRequestBudget{}
	}
	for key, b := range s.widgetBudget {
		if b.active == 0 && now.Sub(b.window) >= time.Minute {
			delete(s.widgetBudget, key)
		}
	}
	b := s.widgetBudget[id]
	if b == nil {
		b = &widgetRequestBudget{window: now}
		s.widgetBudget[id] = b
	}
	if now.Sub(b.window) >= time.Minute {
		b.window = now
		b.count = 0
	}
	if b.active >= policy.MaxConcurrentRequests || b.count >= policy.MaxRequestsPerMinute {
		return nil, false
	}
	b.active++
	b.count++
	return func() { s.widgetMu.Lock(); defer s.widgetMu.Unlock(); b.active-- }, true
}

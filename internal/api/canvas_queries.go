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
	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/appexec"
	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
)

type canvasEndpointResolver interface {
	ResolveBoundEndpoint(context.Context, string, string, string) (*app.EndpointBinding, string, error)
}

type canvasEndpointLister interface {
	ListEndpointBindings(context.Context, string) ([]*app.EndpointBinding, error)
}

var errCanvasConnectionSelectionRequired = errors.New("connection selection required")

func (s *Server) WithAppExecutor(executor *appexec.Executor) *Server { s.appHTTP = executor; return s }

func (s *Server) bindCanvas(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	var req struct {
		ExpectedRevision int64             `json:"expectedRevision"`
		RevisionHash     string            `json:"revisionHash"`
		Bindings         map[string]string `json:"bindings"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetCanvas(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	if _, err = s.store.GetCanvasRevision(c.Request.Context(), id, req.RevisionHash); err != nil {
		return s.canvasError(c, err)
	}
	p, err := canvas.ReadPackage(s.home, id, req.RevisionHash)
	if err != nil {
		return s.canvasError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.canvasError(c, err)
	}
	resolver, ok := s.apps.(canvasEndpointResolver)
	if !ok {
		return badRequest(c, "App connections unavailable")
	}
	for slot, connectionID := range req.Bindings {
		source, ok := m.Sources[slot]
		if !ok {
			return badRequest(c, "undeclared source slot")
		}
		if _, _, err := resolver.ResolveBoundEndpoint(c.Request.Context(), source.AppID, source.Endpoint, connectionID); err != nil {
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
	w, err = s.store.UpdateCanvas(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

func (s *Server) canvasOperation(ctx context.Context, id, hash, operationID string) (*store.Canvas, canvas.Operation, *app.EndpointBinding, string, error) {
	w, err := s.store.GetCanvas(ctx, id)
	if err != nil {
		return nil, canvas.Operation{}, nil, "", err
	}
	if _, err = s.store.GetCanvasRevision(ctx, id, hash); err != nil {
		return nil, canvas.Operation{}, nil, "", err
	}
	p, err := canvas.ReadPackage(s.home, id, hash)
	if err != nil {
		return nil, canvas.Operation{}, nil, "", err
	}
	m, _, err := p.Validate()
	if err != nil {
		return nil, canvas.Operation{}, nil, "", err
	}
	op, ok := m.Operations[operationID]
	if !ok {
		return nil, op, nil, "", store.ErrNotFound
	}
	source := m.Sources[op.Source]
	binding, fingerprint, err := s.resolveCanvasSource(ctx, w, op.Source, source, op.Kind)
	if err != nil {
		return nil, op, nil, "", err
	}
	if binding.Endpoint.Kind != op.Kind {
		return nil, op, nil, "", fmt.Errorf("endpoint kind changed")
	}
	return w, op, binding, fingerprint, nil
}

func (s *Server) resolveCanvasSource(ctx context.Context, w *store.Canvas, slot string, source canvas.Source, kind string) (*app.EndpointBinding, string, error) {
	resolver, ok := s.apps.(canvasEndpointResolver)
	if !ok {
		return nil, "", fmt.Errorf("App connections unavailable")
	}
	connectionID, bound := w.Bindings[slot]
	if !bound {
		lister, ok := s.apps.(canvasEndpointLister)
		if !ok {
			return nil, "", fmt.Errorf("binding_unavailable")
		}
		available, err := lister.ListEndpointBindings(ctx, kind)
		if err != nil {
			return nil, "", err
		}
		matches := 0
		for _, candidate := range available {
			if candidate.AppID == source.AppID && candidate.EndpointName == source.Endpoint {
				connectionID = candidate.ConnectionID
				matches++
			}
		}
		if matches != 1 {
			return nil, "", errCanvasConnectionSelectionRequired
		}
	}
	binding, identity, err := resolver.ResolveBoundEndpoint(ctx, source.AppID, source.Endpoint, connectionID)
	if err != nil {
		return nil, "", err
	}
	return binding, fmt.Sprintf("%x", sha256.Sum256([]byte(identity))), nil
}

func (s *Server) queryCanvas(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	operationID, _ := c.Param("operationID")
	var req struct {
		RevisionHash   string         `json:"revisionHash"`
		BindingVersion int64          `json:"bindingVersion"`
		Params         map[string]any `json:"params"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, op, binding, _, err := s.canvasOperation(c.Request.Context(), id, req.RevisionHash, operationID)
	if err != nil {
		return s.canvasError(c, err)
	}
	if !op.SafeRead() {
		c.JSON(http.StatusForbidden, map[string]string{"error": "operation_requires_confirmation"})
		return nil
	}
	if w.BindingVersion != req.BindingVersion {
		return s.canvasError(c, store.ErrCanvasConflict)
	}
	resolved, err := op.ResolveRequest(req.Params)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if err = appexec.ValidateBoundRequest(binding, resolved.Method, resolved.Query, resolved.Body); err != nil {
		return badRequest(c, err.Error())
	}
	release, ok := s.acquireCanvasRequest(id)
	if !ok {
		c.Header("Retry-After", "30")
		c.JSON(http.StatusTooManyRequests, map[string]any{"error": "request_limit", "retryAfterMS": 30000})
		return nil
	}
	defer release()
	response := s.executeCanvasRequest(c.Request.Context(), binding, op, resolved)
	data, err := canvasResponseData(op, response)
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

func canvasResponseData(op canvas.Operation, response map[string]any) (any, error) {
	if response["ok"] != true {
		return nil, fmt.Errorf("App request failed: %v", response["reason"])
	}
	status, _ := response["status"].(int)
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("App returned HTTP %d", status)
	}
	if response["body_truncated"] == true {
		return nil, fmt.Errorf("App response exceeds size limit")
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
			return nil, fmt.Errorf("App response is not JSON")
		}
	}
	if len(op.Result.Schema) > 0 {
		if err := canvas.ValidateInput(op.Result.Schema, data); err != nil {
			return nil, fmt.Errorf("response_schema_mismatch: %w", err)
		}
	}
	for _, p := range []string{op.Result.Rows, op.Result.Total, op.Result.Cursor} {
		if p != "" {
			if _, err := canvas.Pointer(data, p); err != nil {
				return nil, fmt.Errorf("response_schema_mismatch: %w", err)
			}
		}
	}
	if condition := op.Result.Success; condition != nil {
		value, err := canvas.Pointer(data, condition.Pointer)
		if err != nil || !reflect.DeepEqual(value, condition.Equals) {
			return nil, fmt.Errorf("App business success condition failed")
		}
	}
	return data, nil
}

func (s *Server) executeCanvasRequest(ctx context.Context, binding *app.EndpointBinding, op canvas.Operation, r canvas.Request) map[string]any {
	limit := contracts.Canvas().MaxResponseBytes
	if op.Kind == "rest" {
		return s.appHTTP.REST(ctx, binding, map[string]any{"method": r.Method, "path": r.Path, "query": r.Query, "body_json": r.Body}, limit)
	}
	return s.appHTTP.GraphQL(ctx, binding, map[string]any{"query": r.Document, "operationName": r.OperationName, "variables": r.Variables}, limit)
}

type canvasRequestBudget struct {
	window        time.Time
	count, active int
}

func (s *Server) acquireCanvasRequest(id string) (func(), bool) {
	s.canvasMu.Lock()
	defer s.canvasMu.Unlock()
	now := time.Now()
	policy := contracts.Canvas()
	if s.canvasBudget == nil {
		s.canvasBudget = map[string]*canvasRequestBudget{}
	}
	for key, b := range s.canvasBudget {
		if b.active == 0 && now.Sub(b.window) >= time.Minute {
			delete(s.canvasBudget, key)
		}
	}
	b := s.canvasBudget[id]
	if b == nil {
		b = &canvasRequestBudget{window: now}
		s.canvasBudget[id] = b
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
	return func() { s.canvasMu.Lock(); defer s.canvasMu.Unlock(); b.active-- }, true
}

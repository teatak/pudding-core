package api

import (
	"context"
	"crypto/sha256"
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
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/workbench"
)

type workbenchEndpointResolver interface {
	ResolveBoundEndpoint(context.Context, string, string, string) (*app.EndpointBinding, string, error)
}

func (s *Server) WithAppExecutor(executor *appexec.Executor) *Server { s.appHTTP = executor; return s }

func (s *Server) bindWorkbench(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		ExpectedRevision int64             `json:"expectedRevision"`
		RevisionHash     string            `json:"revisionHash"`
		Bindings         map[string]string `json:"bindings"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if _, err = s.store.GetWorkbenchRevision(c.Request.Context(), id, req.RevisionHash); err != nil {
		return s.workbenchError(c, err)
	}
	p, err := workbench.ReadPackage(s.home, id, req.RevisionHash)
	if err != nil {
		return s.workbenchError(c, err)
	}
	m, _, err := p.Validate()
	if err != nil {
		return s.workbenchError(c, err)
	}
	resolver, ok := s.apps.(workbenchEndpointResolver)
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
		w.Grants = map[string]store.WorkbenchGrant{}
	}
	w, err = s.store.UpdateWorkbench(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

func (s *Server) workbenchOperation(ctx context.Context, id, hash, operationID string) (*store.Workbench, workbench.Operation, *app.EndpointBinding, string, error) {
	w, err := s.store.GetWorkbench(ctx, id)
	if err != nil {
		return nil, workbench.Operation{}, nil, "", err
	}
	if _, err = s.store.GetWorkbenchRevision(ctx, id, hash); err != nil {
		return nil, workbench.Operation{}, nil, "", err
	}
	p, err := workbench.ReadPackage(s.home, id, hash)
	if err != nil {
		return nil, workbench.Operation{}, nil, "", err
	}
	m, _, err := p.Validate()
	if err != nil {
		return nil, workbench.Operation{}, nil, "", err
	}
	op, ok := m.Operations[operationID]
	if !ok {
		return nil, op, nil, "", store.ErrNotFound
	}
	connectionID, ok := w.Bindings[op.Source]
	if !ok {
		return nil, op, nil, "", fmt.Errorf("binding_unavailable")
	}
	resolver, ok := s.apps.(workbenchEndpointResolver)
	if !ok {
		return nil, op, nil, "", fmt.Errorf("App connections unavailable")
	}
	source := m.Sources[op.Source]
	binding, identity, err := resolver.ResolveBoundEndpoint(ctx, source.AppID, source.Endpoint, connectionID)
	if err != nil {
		return nil, op, nil, "", err
	}
	if binding.Endpoint.Kind != op.Kind {
		return nil, op, nil, "", fmt.Errorf("endpoint kind changed")
	}
	return w, op, binding, fmt.Sprintf("%x", sha256.Sum256([]byte(identity))), nil
}

func (s *Server) grantWorkbenchQuery(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	var req struct {
		ExpectedRevision int64  `json:"expectedRevision"`
		RevisionHash     string `json:"revisionHash"`
		OperationID      string `json:"operationID"`
		ConfirmRead      bool   `json:"confirmRead"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.ConfirmRead {
		return badRequest(c, "explicit read confirmation required")
	}
	w, op, binding, fingerprint, err := s.workbenchOperation(c.Request.Context(), id, req.RevisionHash, req.OperationID)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if op.EffectHint == "write" {
		return badRequest(c, "write operation cannot receive a query grant")
	}
	w.Grants[req.OperationID] = store.WorkbenchGrant{OperationHash: op.Hash(), BindingFingerprint: fingerprint, ConnectionID: binding.ConnectionID, GrantedAt: time.Now().UTC()}
	w, err = s.store.UpdateWorkbench(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}
func (s *Server) revokeWorkbenchQuery(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	operationID, _ := c.Param("operationID")
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, err := s.store.GetWorkbench(c.Request.Context(), id)
	if err != nil {
		return s.workbenchError(c, err)
	}
	delete(w.Grants, operationID)
	w, err = s.store.UpdateWorkbench(c.Request.Context(), w, req.ExpectedRevision)
	if err != nil {
		return s.workbenchError(c, err)
	}
	c.JSON(http.StatusOK, w)
	return nil
}

func (s *Server) queryWorkbench(c *cart.Context) error {
	id, _ := c.Param("workbenchID")
	operationID, _ := c.Param("operationID")
	var req struct {
		RevisionHash   string         `json:"revisionHash"`
		BindingVersion int64          `json:"bindingVersion"`
		Params         map[string]any `json:"params"`
	}
	if err := decodeWorkbench(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	w, op, binding, fingerprint, err := s.workbenchOperation(c.Request.Context(), id, req.RevisionHash, operationID)
	if err != nil {
		return s.workbenchError(c, err)
	}
	if w.BindingVersion != req.BindingVersion {
		return s.workbenchError(c, store.ErrWorkbenchConflict)
	}
	grant, ok := w.Grants[operationID]
	if !ok || grant.OperationHash != op.Hash() || grant.BindingFingerprint != fingerprint || grant.ConnectionID != binding.ConnectionID {
		c.JSON(http.StatusForbidden, map[string]string{"error": "permission_required"})
		return nil
	}
	resolved, err := op.ResolveRequest(req.Params)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if err = appexec.ValidateBoundRequest(binding, resolved.Method, resolved.Query, resolved.Body); err != nil {
		return badRequest(c, err.Error())
	}
	release, ok := s.acquireWorkbenchRequest(id)
	if !ok {
		c.Header("Retry-After", "30")
		c.JSON(http.StatusTooManyRequests, map[string]any{"error": "request_limit", "retryAfterMS": 30000})
		return nil
	}
	defer release()
	response := s.executeWorkbenchRequest(c.Request.Context(), binding, op, resolved)
	data, err := workbenchResponseData(op, response)
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

func workbenchResponseData(op workbench.Operation, response map[string]any) (any, error) {
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
		if err := workbench.ValidateInput(op.Result.Schema, data); err != nil {
			return nil, fmt.Errorf("response_schema_mismatch: %w", err)
		}
	}
	for _, p := range []string{op.Result.Rows, op.Result.Total, op.Result.Cursor} {
		if p != "" {
			if _, err := workbench.Pointer(data, p); err != nil {
				return nil, fmt.Errorf("response_schema_mismatch: %w", err)
			}
		}
	}
	if condition := op.Result.Success; condition != nil {
		value, err := workbench.Pointer(data, condition.Pointer)
		if err != nil || !reflect.DeepEqual(value, condition.Equals) {
			return nil, fmt.Errorf("App business success condition failed")
		}
	}
	return data, nil
}

func (s *Server) executeWorkbenchRequest(ctx context.Context, binding *app.EndpointBinding, op workbench.Operation, r workbench.Request) map[string]any {
	limit := contracts.Workbench().MaxResponseBytes
	if op.Kind == "rest" {
		return s.appHTTP.REST(ctx, binding, map[string]any{"method": r.Method, "path": r.Path, "query": r.Query, "body_json": r.Body}, limit)
	}
	return s.appHTTP.GraphQL(ctx, binding, map[string]any{"query": r.Document, "operationName": r.OperationName, "variables": r.Variables}, limit)
}

type workbenchRequestBudget struct {
	window        time.Time
	count, active int
}

func (s *Server) acquireWorkbenchRequest(id string) (func(), bool) {
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	now := time.Now()
	policy := contracts.Workbench()
	if s.workbenchBudget == nil {
		s.workbenchBudget = map[string]*workbenchRequestBudget{}
	}
	for key, b := range s.workbenchBudget {
		if b.active == 0 && now.Sub(b.window) >= time.Minute {
			delete(s.workbenchBudget, key)
		}
	}
	b := s.workbenchBudget[id]
	if b == nil {
		b = &workbenchRequestBudget{window: now}
		s.workbenchBudget[id] = b
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
	return func() { s.workbenchMu.Lock(); defer s.workbenchMu.Unlock(); b.active-- }, true
}

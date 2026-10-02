package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/appexec"
	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) prepareCanvasAction(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	operationID, _ := c.Param("operationID")
	var req struct {
		RevisionHash    string         `json:"revisionHash"`
		BindingVersion  int64          `json:"bindingVersion"`
		ClientRequestID string         `json:"clientRequestID"`
		Params          map[string]any `json:"params"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if req.ClientRequestID == "" || len(req.ClientRequestID) > 100 {
		return badRequest(c, "clientRequestID required")
	}
	w, op, binding, fingerprint, err := s.canvasOperation(c.Request.Context(), id, req.RevisionHash, operationID)
	if err != nil {
		return s.canvasError(c, err)
	}
	if !binding.Endpoint.CanvasWrites {
		c.JSON(http.StatusForbidden, map[string]string{"error": "app_writes_disabled"})
		return nil
	}
	if w.BindingVersion != req.BindingVersion || w.ActiveRevision != req.RevisionHash {
		return s.canvasError(c, store.ErrCanvasConflict)
	}
	resolved, err := op.ResolveRequest(req.Params)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if err = appexec.ValidateBoundRequest(binding, resolved.Method, resolved.Query, resolved.Body); err != nil {
		return badRequest(c, err.Error())
	}
	frozen, _ := json.Marshal(resolved)
	// Only caller intent participates in idempotency, not mutable connection state.
	intent, _ := json.Marshal(struct {
		RevisionHash, OperationID string
		Params                    map[string]any
	}{req.RevisionHash, operationID, req.Params})
	a := &store.CanvasAction{ID: store.NewID("action"), CanvasID: id, ClientRequestID: req.ClientRequestID, RequestHash: fmt.Sprintf("%x", sha256.Sum256(intent)), State: "prepared", CreatedAt: time.Now().UTC(), Spec: store.CanvasActionSpec{RevisionHash: req.RevisionHash, ResourceRevision: w.Revision, BindingVersion: w.BindingVersion, OperationID: operationID, OperationHash: op.Hash(), BindingFingerprint: fingerprint, AppID: binding.AppID, ConnectionID: binding.ConnectionID, Description: op.Description, Params: req.Params, Request: frozen}}
	a, err = s.store.CreateCanvasAction(c.Request.Context(), a)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, a)
	return nil
}
func (s *Server) listCanvasActions(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	if _, err := s.store.GetCanvas(c.Request.Context(), id); err != nil {
		return s.canvasError(c, err)
	}
	actions, err := s.store.ListCanvasActions(c.Request.Context(), id)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"actions": actions})
	return nil
}
func (s *Server) executeCanvasAction(c *cart.Context) error {
	id, _ := c.Param("canvasID")
	actionID, _ := c.Param("actionID")
	var req struct {
		Confirm bool `json:"confirm"`
	}
	if err := decodeCanvas(c, &req); err != nil {
		return badRequest(c, err.Error())
	}
	if !req.Confirm {
		return badRequest(c, "explicit action confirmation required")
	}
	ctx := c.Request.Context()
	a, err := s.store.GetCanvasAction(ctx, id, actionID)
	if err != nil {
		return s.canvasError(c, err)
	}
	if _, err = s.store.GetCanvas(ctx, id); err != nil {
		return s.canvasError(c, err)
	}
	// Replayed confirmation returns its canonical record; never dispatch twice.
	if a.State != "prepared" {
		c.JSON(http.StatusOK, a)
		return nil
	}
	w, op, binding, fingerprint, err := s.canvasOperation(ctx, id, a.Spec.RevisionHash, a.Spec.OperationID)
	if err != nil {
		return s.canvasError(c, err)
	}
	if !binding.Endpoint.CanvasWrites || fingerprint != a.Spec.BindingFingerprint || op.Hash() != a.Spec.OperationHash || w.Revision != a.Spec.ResourceRevision {
		return s.canvasError(c, store.ErrCanvasConflict)
	}
	resolved, err := op.ResolveRequest(a.Spec.Params)
	if err != nil {
		return badRequest(c, err.Error())
	}
	if err = appexec.ValidateBoundRequest(binding, resolved.Method, resolved.Query, resolved.Body); err != nil {
		return badRequest(c, err.Error())
	}
	release, ok := s.acquireCanvasRequest(id)
	if !ok {
		c.Header("Retry-After", "30")
		c.JSON(http.StatusTooManyRequests, map[string]string{"error": "request_limit"})
		return nil
	}
	defer release()
	if err = s.store.ClaimCanvasAction(ctx, id, actionID); err != nil {
		return s.canvasError(c, err)
	}
	response := s.executeCanvasRequest(ctx, binding, op, resolved)
	data, responseErr := canvasResponseData(op, response)
	state := "succeeded"
	var result any = map[string]any{"data": data}
	if responseErr != nil {
		// A transport/HTTP/decoding error cannot prove a remote write did not commit.
		state = "unknown"
		if response["ok"] != true {
			switch response["reason"] {
			case "invalid_path", "unsupported_method", "invalid_query", "connection_field_error", "invalid_body", "invalid_variables", "encode_error", "request_too_large", "request_error", "token_exchange_failed", "auth_config_error":
				state = "failed"
			}
		}
		result = map[string]any{"error": responseErr.Error(), "retryable": false}
	}
	raw, _ := json.Marshal(result)
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err = s.store.FinishCanvasAction(finishCtx, id, actionID, state, raw); err != nil {
		return s.canvasError(c, err)
	}
	a, err = s.store.GetCanvasAction(finishCtx, id, actionID)
	if err != nil {
		return s.canvasError(c, err)
	}
	c.JSON(http.StatusOK, a)
	return nil
}

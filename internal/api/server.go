// Package api 注册 REST / SSE 路由(docs/technology-decisions.md 第 7 节)。
// 所有业务端点显式带 sessionID,禁止无 session scope 的主路径接口
// (AGENTS.md 硬约束 3 / 4)。
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/mobileauth"
	"github.com/teatak/pudding-core/internal/store"
)

type Server struct {
	engine    *engine.Engine
	store     store.Store
	config    engine.ConfigSource
	providers providerWriter
	hub       *event.Hub
}

func New(eng *engine.Engine, s store.Store, cfg engine.ConfigSource, hub *event.Hub) *Server {
	providers, _ := cfg.(providerWriter)
	return &Server{engine: eng, store: s, config: cfg, providers: providers, hub: hub}
}

// apiPrefixes 是需要 token 鉴权的 API 路径前缀;其余路径交给静态 UI。
var apiPrefixes = []string{"/sessions", "/settings", "/providers", "/tools", "/usage", "/mobile"}

type deviceTokenValidator interface {
	ValidToken(token string) bool
}

type pairingService interface {
	Create(fallbackBaseURL string) (mobileauth.Pairing, error)
	Claim(code, deviceName string) (mobileauth.ClaimResult, error)
}

type handlerConfig struct {
	deviceTokens deviceTokenValidator
	pairing      pairingService
}

type HandlerOption func(*handlerConfig)

func WithDeviceTokenValidator(v deviceTokenValidator) HandlerOption {
	return func(cfg *handlerConfig) { cfg.deviceTokens = v }
}

func WithPairing(p pairingService) HandlerOption {
	return func(cfg *handlerConfig) { cfg.pairing = p }
}

// Handler 返回根 handler:API 前缀走 token 鉴权 + cart 路由,
// 其余路径 serve 静态 web UI(HTML/JS 非敏感,数据全在 API 后面;
// static 为 nil 时只有 API)。
// token 经 Authorization: Bearer 或 ?token= 传递;后者服务 EventSource
// (浏览器 SSE 无法自定义 header)。
func (s *Server) Handler(token string, static http.Handler, options ...HandlerOption) http.Handler {
	cfg := handlerConfig{}
	for _, option := range options {
		option(&cfg)
	}
	app := cart.New()
	public := cart.New()

	app.Route("/sessions").POST(s.createSession).GET(s.listSessions)
	app.Route("/sessions/:id").GET(s.getSession).PATCH(s.patchSession).DELETE(s.deleteSession)
	app.Route("/sessions/:id/submit").POST(s.submit)
	app.Route("/sessions/:id/cancel").POST(s.cancel)
	app.Route("/sessions/:id/compact").POST(s.compactSession)
	app.Route("/sessions/:id/approvals").GET(s.listApprovals)
	app.Route("/sessions/:id/approvals/:approvalID/approve").POST(s.approveApproval)
	app.Route("/sessions/:id/approvals/:approvalID/deny").POST(s.denyApproval)
	app.Route("/sessions/:id/events").GET(s.sessionEvents)
	app.Route("/sessions/:id/usage").GET(s.getSessionUsage)
	app.Route("/sessions/:id/turns").GET(s.listTurns)
	app.Route("/sessions/:id/turns/:turnID").GET(s.getTurn)
	app.Route("/sessions/:id/messages").GET(s.listMessages)
	app.Route("/sessions/:id/queued-inputs").GET(s.listQueuedInputs)
	app.Route("/sessions/:id/queued-inputs/:clientMessageID").PATCH(s.patchQueuedInput)
	app.Route("/settings").GET(s.getSettings).PUT(s.putSettings)
	app.Route("/providers").GET(s.listProviders).POST(s.createProvider)
	app.Route("/providers/models").POST(s.probeProviderModels)
	app.Route("/providers/:name").GET(s.getProvider).PATCH(s.patchProvider).DELETE(s.deleteProvider)
	app.Route("/providers/:name/models").GET(s.listProviderModels)
	app.Route("/tools/builtin").GET(s.listBuiltinTools)
	app.Route("/tools/web").GET(s.getWebTools).PATCH(s.patchWebTools).PUT(s.patchWebTools)
	app.Route("/usage/daily").GET(s.getDailyUsage)
	if cfg.pairing != nil {
		app.Route("/mobile/pairings").POST(func(c *cart.Context) error {
			pairing, err := cfg.pairing.Create(requestBaseURL(c.Request))
			if err != nil {
				return s.fail(c, err)
			}
			c.JSON(http.StatusCreated, pairing)
			return nil
		})
		public.Route("/mobile/pairings/:code/claim").POST(func(c *cart.Context) error {
			code, _ := c.Param("code")
			var req struct {
				DeviceName string `json:"deviceName"`
			}
			if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
				return badRequest(c, "invalid json body")
			}
			claim, err := cfg.pairing.Claim(code, req.DeviceName)
			if errors.Is(err, mobileauth.ErrPairingInvalid) {
				c.JSON(http.StatusNotFound, map[string]string{"error": "pairing_invalid"})
				return nil
			}
			if err != nil {
				return s.fail(c, err)
			}
			c.JSON(http.StatusOK, claim)
			return nil
		})
	}

	authed := withAuth(token, cfg.deviceTokens, app)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.pairing != nil && isPublicMobilePath(r.URL.Path) {
			public.ServeHTTP(w, r)
			return
		}
		for _, prefix := range apiPrefixes {
			if r.URL.Path == prefix || strings.HasPrefix(r.URL.Path, prefix+"/") {
				authed.ServeHTTP(w, r)
				return
			}
		}
		if static == nil {
			http.NotFound(w, r)
			return
		}
		static.ServeHTTP(w, r)
	})
	return withCORS(handler)
}

func withAuth(token string, devices deviceTokenValidator, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if validBearerToken(r, token, devices) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
}

func validBearerToken(r *http.Request, daemonToken string, devices deviceTokenValidator) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == r.Header.Get("Authorization") {
		token = ""
	}
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" {
		return false
	}
	if token == daemonToken {
		return true
	}
	return devices != nil && devices.ValidToken(token)
}

func isPublicMobilePath(path string) bool {
	return strings.HasPrefix(path, "/mobile/pairings/") && strings.HasSuffix(path, "/claim")
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/"
}

type createSessionReq struct {
	Title    string `json:"title"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

func (s *Server) createSession(c *cart.Context) error {
	var req createSessionReq
	// body 可为空(EOF);非空但坏 JSON 必须拒绝,否则会静默建出空会话。
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	if req.Provider == "" || req.Model == "" {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "no_model"})
		return nil
	}
	sess := &store.Session{ID: store.NewID("sess"), Title: req.Title, Provider: req.Provider, Model: req.Model}
	if err := s.store.CreateSession(c.Request.Context(), sess); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, sess)
	return nil
}

func (s *Server) listSessions(c *cart.Context) error {
	sessions, err := s.store.ListSessions(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"sessions": sessions})
	return nil
}

func (s *Server) getSession(c *cart.Context) error {
	id, _ := c.Param("id")
	sess, err := s.store.GetSession(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, sess)
	return nil
}

func (s *Server) getSessionUsage(c *cart.Context) error {
	id, _ := c.Param("id")
	usage, err := s.engine.SessionUsage(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, usage)
	return nil
}

func (s *Server) patchSession(c *cart.Context) error {
	id, _ := c.Param("id")
	var upd store.SessionUpdate
	if err := decode(c, &upd); err != nil {
		return badRequest(c, "invalid json body")
	}
	if upd.Provider != nil {
		provider := strings.TrimSpace(*upd.Provider)
		if provider == "" {
			return badRequest(c, "provider is required")
		}
		upd.Provider = &provider
	}
	if upd.Model != nil {
		model := strings.TrimSpace(*upd.Model)
		if model == "" {
			return badRequest(c, "model is required")
		}
		upd.Model = &model
	}
	sess, err := s.store.UpdateSession(c.Request.Context(), id, upd)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, sess)
	return nil
}

func (s *Server) deleteSession(c *cart.Context) error {
	id, _ := c.Param("id")
	// 先 cancel 进行中的 turn:否则 provider 流会继续跑到自然结束,
	// 且收尾 FinishTurn 撞上已删除的 session。无进行中 turn 时 cancel 返回
	// ErrNoRunningTurn,忽略即可。
	if err := s.engine.Cancel(id); err != nil && !errors.Is(err, engine.ErrNoRunningTurn) {
		return s.fail(c, err)
	}
	if err := s.store.DeleteSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

type submitReq struct {
	ClientMessageID string `json:"clientMessageID"`
	Text            string `json:"text"`
	Kind            string `json:"kind,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

func (s *Server) submit(c *cart.Context) error {
	id, _ := c.Param("id")
	var req submitReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	res, err := s.engine.Submit(c.Request.Context(), engine.SubmitInput{
		SessionID:       id,
		ClientMessageID: req.ClientMessageID,
		Text:            req.Text,
		Kind:            req.Kind,
		ReasoningEffort: req.ReasoningEffort,
	})
	switch {
	case errors.Is(err, engine.ErrEmptyInput):
		return badRequest(c, "text and clientMessageID are required")
	case errors.Is(err, engine.ErrTurnRunning):
		c.JSON(http.StatusConflict, map[string]string{"error": "turn_running"})
		return nil
	case errors.Is(err, engine.ErrNoModel):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "no_model"})
		return nil
	case errors.Is(err, engine.ErrProviderConfig):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "provider_config"})
		return nil
	case err != nil:
		return s.fail(c, err)
	}
	if res.Duplicate {
		c.JSON(http.StatusOK, res)
		return nil
	}
	c.JSON(http.StatusAccepted, res)
	return nil
}

func (s *Server) listQueuedInputs(c *cart.Context) error {
	id, _ := c.Param("id")
	inputs, err := s.store.ListQueuedInputs(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"queuedInputs": inputs})
	return nil
}

type patchQueuedInputReq struct {
	Text   *string `json:"text"`
	Status *string `json:"status"`
}

func (s *Server) patchQueuedInput(c *cart.Context) error {
	id, _ := c.Param("id")
	clientMessageID, _ := c.Param("clientMessageID")
	var req patchQueuedInputReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	var text *string
	if req.Text != nil {
		next := strings.TrimSpace(*req.Text)
		if next == "" {
			return badRequest(c, "text is required")
		}
		text = &next
	}
	var status *store.QueuedInputStatus
	if req.Status != nil {
		next := store.QueuedInputStatus(strings.TrimSpace(*req.Status))
		switch next {
		case store.QueuedInputQueued, store.QueuedInputEditing, store.QueuedInputCancelled:
			status = &next
		default:
			return badRequest(c, "invalid status")
		}
	}
	if text == nil && status == nil {
		return badRequest(c, "text or status is required")
	}
	res, err := s.store.UpdateQueuedInput(c.Request.Context(), store.UpdateQueuedInputInput{
		SessionID:       id,
		ClientMessageID: clientMessageID,
		Text:            text,
		Status:          status,
	})
	if err != nil {
		return s.fail(c, err)
	}
	if res.Event != nil {
		s.hub.Publish(*res.Event)
	}
	if res.Input != nil && (res.Input.Status == store.QueuedInputQueued || res.Input.Status == store.QueuedInputCancelled) {
		s.engine.TryDrainQueued(id)
	}
	c.JSON(http.StatusOK, res.Input)
	return nil
}

func (s *Server) cancel(c *cart.Context) error {
	id, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	if err := s.engine.Cancel(id); err != nil {
		c.JSON(http.StatusConflict, map[string]string{"error": "no_running_turn"})
		return nil
	}
	c.JSON(http.StatusAccepted, map[string]string{"status": "cancelling"})
	return nil
}

type compactReq struct {
	Hint string `json:"hint"`
}

func (s *Server) compactSession(c *cart.Context) error {
	id, _ := c.Param("id")
	var req compactReq
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	res, err := s.engine.Compact(c.Request.Context(), engine.CompactInput{
		SessionID: id,
		Hint:      req.Hint,
	})
	switch {
	case errors.Is(err, engine.ErrTurnRunning):
		c.JSON(http.StatusConflict, map[string]string{"error": "turn_running"})
		return nil
	case errors.Is(err, engine.ErrCompactRunning):
		c.JSON(http.StatusConflict, map[string]string{"error": "compact_running"})
		return nil
	case errors.Is(err, engine.ErrCompactEmpty):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "compact_empty"})
		return nil
	case errors.Is(err, engine.ErrNoModel):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "no_model"})
		return nil
	case errors.Is(err, engine.ErrProviderConfig):
		c.JSON(http.StatusBadRequest, map[string]string{"error": "provider_config"})
		return nil
	case err != nil:
		return s.fail(c, err)
	}
	c.JSON(http.StatusAccepted, res)
	return nil
}

type approveApprovalReq struct {
	Scope         string   `json:"scope"`
	WorkspaceDirs []string `json:"workspaceDirs"`
}

type approvalView struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"sessionID"`
	TurnID       string          `json:"turnID"`
	CallID       string          `json:"callID,omitempty"`
	ApprovalKind string          `json:"approvalKind"`
	TargetMode   store.AgentMode `json:"targetMode,omitempty"`
	Title        string          `json:"title,omitempty"`
	Reason       string          `json:"reason,omitempty"`
	Risk         string          `json:"risk,omitempty"`
	Payload      json.RawMessage `json:"payload,omitempty"`
	CreatedAt    string          `json:"createdAt"`
}

func (s *Server) listApprovals(c *cart.Context) error {
	id, _ := c.Param("id")
	pending := s.engine.PendingApprovals(id)
	views := make([]approvalView, 0, len(pending))
	for _, approval := range pending {
		views = append(views, approvalView{
			ID:           approval.ID,
			SessionID:    approval.SessionID,
			TurnID:       approval.TurnID,
			CallID:       approval.CallID,
			ApprovalKind: approval.Kind,
			TargetMode:   approval.TargetMode,
			Title:        approval.Title,
			Reason:       approval.Reason,
			Risk:         approval.Risk,
			Payload:      approval.Payload,
			CreatedAt:    approval.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, map[string]any{"approvals": views})
	return nil
}

func (s *Server) approveApproval(c *cart.Context) error {
	id, _ := c.Param("id")
	approvalID, _ := c.Param("approvalID")
	var req approveApprovalReq
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	scope := engine.ApprovalScope(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = engine.ApprovalScopeTurn
	}
	if scope != engine.ApprovalScopeTurn && scope != engine.ApprovalScopeSession {
		return badRequest(c, "invalid approval scope")
	}
	if err := s.engine.ApproveApproval(c.Request.Context(), id, approvalID, scope, req.WorkspaceDirs); err != nil {
		if errors.Is(err, engine.ErrApprovalNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
			return nil
		}
		if errors.Is(err, engine.ErrWorkspaceDirsRequired) {
			return badRequest(c, "workspace_dirs_required")
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusAccepted, map[string]string{"status": "approved"})
	return nil
}

type denyApprovalReq struct {
	Reason string `json:"reason"`
}

func (s *Server) denyApproval(c *cart.Context) error {
	id, _ := c.Param("id")
	approvalID, _ := c.Param("approvalID")
	var req denyApprovalReq
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	if err := s.engine.DenyApproval(c.Request.Context(), id, approvalID, strings.TrimSpace(req.Reason)); err != nil {
		if errors.Is(err, engine.ErrApprovalNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
			return nil
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusAccepted, map[string]string{"status": "denied"})
	return nil
}

func (s *Server) listMessages(c *cart.Context) error {
	id, _ := c.Param("id")
	before := strings.TrimSpace(c.Request.URL.Query().Get("before"))
	limit, err := pageLimit(c.Request.URL.Query().Get("limit"))
	if err != nil {
		return badRequest(c, "invalid limit")
	}
	page, err := s.store.ListMessagesPage(c.Request.Context(), id, before, limit)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"messages": page.Messages, "hasMore": page.HasMore})
	return nil
}

func (s *Server) listTurns(c *cart.Context) error {
	id, _ := c.Param("id")
	before := strings.TrimSpace(c.Request.URL.Query().Get("before"))
	limit, err := pageLimit(c.Request.URL.Query().Get("limit"))
	if err != nil {
		return badRequest(c, "invalid limit")
	}
	page, err := s.store.ListTurnsPage(c.Request.Context(), id, before, limit)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"turns": page.Turns, "hasMore": page.HasMore})
	return nil
}

func (s *Server) getTurn(c *cart.Context) error {
	id, _ := c.Param("id")
	turnID, _ := c.Param("turnID")
	turn, err := s.store.GetConversationTurn(c.Request.Context(), id, turnID)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, turn)
	return nil
}

func pageLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if limit < 0 {
		return 0, errors.New("limit must be non-negative")
	}
	if limit > 200 {
		limit = 200
	}
	return limit, nil
}

func (s *Server) getSettings(c *cart.Context) error {
	kv, err := s.config.Settings(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"settings": kv})
	return nil
}

func (s *Server) putSettings(c *cart.Context) error {
	var kv map[string]string
	if err := decode(c, &kv); err != nil {
		return badRequest(c, "invalid json body")
	}
	for k := range kv {
		return badRequest(c, "unknown setting: "+k)
	}
	settings, ok := s.config.(interface {
		SetSettings(context.Context, map[string]string) error
	})
	if !ok {
		return badRequest(c, "settings are read-only")
	}
	if err := settings.SetSettings(c.Request.Context(), kv); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

func (s *Server) fail(c *cart.Context, err error) error {
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
		return nil
	}
	if errors.Is(err, store.ErrInvalidSession) {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "no_model"})
		return nil
	}
	c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	return nil
}

func badRequest(c *cart.Context, msg string) error {
	c.JSON(http.StatusBadRequest, map[string]string{"error": msg})
	return nil
}

func decode(c *cart.Context, v any) error {
	return json.NewDecoder(c.Request.Body).Decode(v)
}

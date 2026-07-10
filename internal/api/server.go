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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/audio/runtimeassets"
	"github.com/teatak/pudding-core/internal/audio/voice"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/config"
	"github.com/teatak/pudding-core/internal/desktopcamera"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/mobileauth"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type Server struct {
	engine     *engine.Engine
	store      store.Store
	config     engine.ConfigSource
	home       string
	providers  providerWriter
	apps       appService
	skills     skillService
	hub        *event.Hub
	voice      voiceController
	audioRT    *runtimeassets.Installer
	browser    browser.Service
	camera     desktopcamera.Capturer
	browserMCP browserMCPService
	browserMu  sync.Mutex
	// browserAllowedTabs is absent until a session's browser surface is explicitly closed or rebound.
	// Once present, only listed tabs are visible; an empty set means the session has no browser tab.
	browserAllowedTabs map[string]map[string]struct{}
	// browserClosedTabs prevents delayed live snapshots from re-adopting explicitly closed tab IDs.
	browserClosedTabs map[string]map[string]struct{}
	oauthMu           sync.Mutex
	oauth             map[string]oauthStartState
}

type voiceController interface {
	Snapshot() voice.Bindings
	BindInput(sessionID string, enabled bool) (voice.Bindings, error)
	BindOutput(sessionID string, enabled bool) (voice.Bindings, error)
	CancelSession(ctx context.Context, sessionID string) bool
	ReleaseSession(sessionID string) voice.Bindings
}

func New(eng *engine.Engine, s store.Store, cfg engine.ConfigSource, hub *event.Hub) *Server {
	providers, _ := cfg.(providerWriter)
	return &Server{engine: eng, store: s, config: cfg, providers: providers, hub: hub, oauth: map[string]oauthStartState{}}
}

func (s *Server) WithApps(apps appService) *Server {
	s.apps = apps
	return s
}

func (s *Server) WithSkills(skills skillService) *Server {
	s.skills = skills
	return s
}

func (s *Server) WithHome(home string) *Server {
	s.home = strings.TrimSpace(home)
	return s
}

func (s *Server) WithBrowserMCP(handler browserMCPService) *Server {
	s.browserMCP = handler
	return s
}

func (s *Server) WithVoice(controller voiceController) *Server {
	s.voice = controller
	return s
}

func (s *Server) WithAudioRuntime(installer *runtimeassets.Installer) *Server {
	s.audioRT = installer
	return s
}

func (s *Server) WithBrowser(service browser.Service) *Server {
	s.browser = service
	return s
}

func (s *Server) WithCamera(capturer desktopcamera.Capturer) *Server {
	s.camera = capturer
	return s
}

// apiPrefixes 是需要 token 鉴权的 API 路径前缀;其余路径交给静态 UI。
var apiPrefixes = []string{"/sessions", "/projects", "/settings", "/providers", "/tools", "/skills", "/skill-drafts", "/skill-assets", "/usage", "/mobile", "/apps", "/app-assets", "/app-skills", "/app-connections", "/app-oauth", "/mcp", "/desktop"}

type appService interface {
	ListDefinitions(ctx context.Context) ([]*app.Definition, error)
	InstallPackage(ctx context.Context, packageJSON []byte, expectedSHA256, sourceURL string) (*app.Definition, error)
	DeleteDefinition(ctx context.Context, id string) error
	ReadAsset(ctx context.Context, rel string) ([]byte, string, error)
	ReadSkill(ctx context.Context, appID, skillID string) (*app.SkillDetail, error)
}

type browserMCPService interface {
	http.Handler
	BrowserSessions() []tool.BrowserMCPSessionSnapshot
}

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
	app.Route("/sessions/:id/attachments").POST(s.uploadAttachment)
	app.Route("/sessions/:id/attachments/*path").GET(s.getAttachment)
	app.Route("/sessions/:id/desktop/screenshot").POST(s.desktopScreenshot)
	app.Route("/sessions/:id/desktop/photo").POST(s.desktopPhoto)
	app.Route("/sessions/:id/audio/bindings").GET(s.getAudioBindings)
	app.Route("/sessions/:id/audio/input").POST(s.bindAudioInput)
	app.Route("/sessions/:id/audio/output").POST(s.bindAudioOutput)
	app.Route("/sessions/:id/browser/state").GET(s.getBrowserState).DELETE(s.clearBrowserState)
	app.Route("/sessions/:id/browser/close").POST(s.closeBrowserSession)
	app.Route("/sessions/:id/browser/open").POST(s.openBrowserSession)
	app.Route("/sessions/:id/browser/tabs").GET(s.listBrowserTabs).POST(s.createBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID").GET(s.getBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/recover").POST(s.recoverBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/adopt").POST(s.adoptBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/sync").POST(s.syncBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/open").POST(s.openBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/back").POST(s.backBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/forward").POST(s.forwardBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/reload").POST(s.reloadBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/observe").POST(s.observeBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/screenshot").POST(s.screenshotBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/click").POST(s.clickBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/type").POST(s.typeBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/scroll").POST(s.scrollBrowserTab)
	app.Route("/sessions/:id/browser/tabs/:tabID/release").POST(s.releaseBrowserTab)
	app.Route("/sessions/:id/queued-inputs").GET(s.listQueuedInputs)
	app.Route("/sessions/:id/queued-inputs/:clientMessageID").PATCH(s.patchQueuedInput)
	app.Route("/sessions/:id/canvas/items").GET(s.listCanvasItems).POST(s.createCanvasItem)
	app.Route("/sessions/:id/canvas/items/:itemID").PUT(s.putCanvasItem).PATCH(s.patchCanvasItem).DELETE(s.deleteCanvasItem)
	app.Route("/sessions/:id/canvas/closed").GET(s.listClosedCanvasItems).POST(s.createClosedCanvasItem).DELETE(s.clearClosedCanvasItems)
	app.Route("/sessions/:id/canvas/closed/:closedID").DELETE(s.deleteClosedCanvasItem)
	app.Route("/projects").GET(s.listProjects).POST(s.createProject)
	app.Route("/projects/:id").GET(s.getProject).PATCH(s.patchProject).DELETE(s.deleteProject)
	app.Route("/settings").GET(s.getSettings).PUT(s.putSettings)
	app.Route("/settings/audio").GET(s.getAudioConfig).PUT(s.putAudioConfig)
	app.Route("/settings/audio/runtime").GET(s.getAudioRuntime)
	app.Route("/settings/audio/runtime/install").POST(s.startAudioRuntimeInstall)
	app.Route("/settings/audio/runtime/cancel").POST(s.cancelAudioRuntimeInstall)
	app.Route("/settings/audio/asr-recordings").DELETE(s.clearASRRecordings)
	app.Route("/settings/user-prompt").GET(s.getUserPrompt).PUT(s.putUserPrompt)
	app.Route("/providers").GET(s.listProviders).POST(s.createProvider)
	app.Route("/providers/models").POST(s.probeProviderModels)
	app.Route("/providers/:name").GET(s.getProvider).PATCH(s.patchProvider).DELETE(s.deleteProvider)
	app.Route("/providers/:name/models").GET(s.listProviderModels)
	app.Route("/tools/builtin").GET(s.listBuiltinTools)
	app.Route("/tools/web").GET(s.getWebTools).PATCH(s.patchWebTools).PUT(s.patchWebTools)
	app.Route("/desktop/about").GET(s.desktopAbout)
	app.Route("/desktop/save-file").POST(s.desktopSaveFile)
	app.Route("/desktop/reveal-file").POST(s.desktopRevealFile)
	app.Route("/mcp/browser-sessions").GET(s.listBrowserMCPSessions)
	app.Route("/skills").GET(s.listSkills)
	app.Route("/skills/:id").DELETE(s.deleteSkill)
	app.Route("/skill-drafts").GET(s.listSkillDrafts)
	app.Route("/skill-drafts/:id").GET(s.getSkillDraft).DELETE(s.deleteSkillDraft)
	app.Route("/skill-drafts/:id/apply").POST(s.applySkillDraft)
	app.Route("/skill-assets/*path").GET(s.getSkillAsset)
	app.Route("/apps").GET(s.listApps)
	app.Route("/apps/install").POST(s.installApp)
	app.Route("/apps/:id/mcp-overrides/:endpoint").GET(s.getAppMCPOverride).PUT(s.putAppMCPOverride).DELETE(s.deleteAppMCPOverride)
	app.Route("/apps/:id/mcp").GET(s.getAppMCPStatus)
	app.Route("/apps/:id").DELETE(s.deleteApp)
	app.Route("/app-assets/*path").GET(s.getAppAsset)
	app.Route("/app-skills/*path").GET(s.getAppSkill)
	app.Route("/app-connections").GET(s.listAppConnections)
	app.Route("/app-connections/:id").GET(s.getAppConnection).PUT(s.putAppConnection).DELETE(s.deleteAppConnection)
	app.Route("/app-oauth/start").POST(s.startAppOAuth)
	public.Route("/oauth/callback/:provider").GET(s.appOAuthCallback)
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
	var mcpAuthed http.Handler
	if s.browserMCP != nil {
		mcpAuthed = withAuth(token, cfg.deviceTokens, s.browserMCP)
	}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mcp/ws" && mcpAuthed != nil {
			mcpAuthed.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == browserTestFormPath {
			serveBrowserTestForm(w, r)
			return
		}
		if cfg.pairing != nil && isPublicMobilePath(r.URL.Path) {
			public.ServeHTTP(w, r)
			return
		}
		if isPublicOAuthPath(r.URL.Path) {
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

func isPublicOAuthPath(path string) bool {
	return strings.HasPrefix(path, "/oauth/callback/")
}

func requestBaseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host + "/"
}

type createSessionReq struct {
	Title     string `json:"title"`
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	ProjectID string `json:"projectID"`
}

func (s *Server) createSession(c *cart.Context) error {
	var req createSessionReq
	// body 可为空(EOF);非空但坏 JSON 必须拒绝,否则会静默建出空会话。
	if err := decode(c, &req); err != nil && !errors.Is(err, io.EOF) {
		return badRequest(c, "invalid json body")
	}
	req.Provider = strings.TrimSpace(req.Provider)
	req.Model = strings.TrimSpace(req.Model)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if req.Provider == "" || req.Model == "" {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "no_model"})
		return nil
	}
	sess := &store.Session{ID: store.NewID("sess"), Title: req.Title, Provider: req.Provider, Model: req.Model, ProjectID: req.ProjectID}
	if req.ProjectID != "" {
		sess.ActiveMode = store.ModeProject
		sess.ModeLease = store.ModeLeaseSession
	}
	if err := s.store.CreateSession(c.Request.Context(), sess); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, sess)
	return nil
}

type createProjectReq struct {
	Name         string             `json:"name"`
	RootDirs     []string           `json:"rootDirs"`
	ApprovalMode store.ApprovalMode `json:"approvalMode"`
}

func (s *Server) createProject(c *cart.Context) error {
	var req createProjectReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	project := &store.Project{
		ID:           store.NewID("proj"),
		Name:         req.Name,
		RootDirs:     req.RootDirs,
		ApprovalMode: req.ApprovalMode,
	}
	if err := s.store.CreateProject(c.Request.Context(), project); err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusCreated, project)
	return nil
}

func (s *Server) listProjects(c *cart.Context) error {
	projects, err := s.store.ListProjects(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, map[string]any{"projects": projects})
	return nil
}

func (s *Server) getProject(c *cart.Context) error {
	id, _ := c.Param("id")
	project, err := s.store.GetProject(c.Request.Context(), id)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, project)
	return nil
}

func (s *Server) patchProject(c *cart.Context) error {
	id, _ := c.Param("id")
	var upd store.ProjectUpdate
	if err := decode(c, &upd); err != nil {
		return badRequest(c, "invalid json body")
	}
	project, err := s.store.UpdateProject(c.Request.Context(), id, upd)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, project)
	return nil
}

func (s *Server) deleteProject(c *cart.Context) error {
	id, _ := c.Param("id")
	if err := s.store.DeleteProject(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
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
	if s.voice != nil {
		s.voice.CancelSession(c.Request.Context(), id)
	}
	// 先 cancel 进行中的 turn:否则 provider 流会继续跑到自然结束,
	// 且收尾 FinishTurn 撞上已删除的 session。无进行中 turn 时 cancel 返回
	// ErrNoRunningTurn,忽略即可。
	if err := s.engine.Cancel(id); err != nil && !errors.Is(err, engine.ErrNoRunningTurn) {
		return s.fail(c, err)
	}
	if err := s.store.DeleteSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	if s.voice != nil {
		s.voice.ReleaseSession(id)
	}
	if s.browser != nil {
		_ = s.browser.ReleaseSession(c.Request.Context(), id)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

type submitReq struct {
	ClientMessageID string              `json:"clientMessageID"`
	Text            string              `json:"text"`
	Parts           []store.ContentPart `json:"parts,omitempty"`
	Kind            string              `json:"kind,omitempty"`
	ReasoningEffort string              `json:"reasoningEffort,omitempty"`
}

func (s *Server) submit(c *cart.Context) error {
	id, _ := c.Param("id")
	var req submitReq
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	parts := store.NormalizeContentParts(req.Parts)
	if req.Kind != "system" {
		parts = store.UserInputParts(req.Text, req.Parts)
		attachments, err := s.normalizeSubmitAttachments(id, store.AttachmentsFromParts(parts))
		if errors.Is(err, errAttachmentHomeUnavailable) {
			c.JSON(http.StatusInternalServerError, map[string]string{"error": "attachment_home_unavailable"})
			return nil
		}
		if errors.Is(err, errInvalidAttachment) {
			return badRequest(c, "invalid attachments")
		}
		if err != nil {
			return s.fail(c, err)
		}
		parts = store.UserInputPartsWithAttachments(req.Text, parts, attachments)
	}
	res, err := s.engine.Submit(c.Request.Context(), engine.SubmitInput{
		SessionID:       id,
		ClientMessageID: req.ClientMessageID,
		Text:            req.Text,
		Parts:           parts,
		Kind:            req.Kind,
		ReasoningEffort: req.ReasoningEffort,
	})
	switch {
	case errors.Is(err, engine.ErrEmptyInput):
		return badRequest(c, "parts and clientMessageID are required")
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
	voiceCancelled := false
	if s.voice != nil {
		voiceCancelled = s.voice.CancelSession(c.Request.Context(), id)
	}
	if err := s.engine.Cancel(id); err != nil {
		if errors.Is(err, engine.ErrNoRunningTurn) && voiceCancelled {
			c.JSON(http.StatusAccepted, map[string]string{"status": "cancelling"})
			return nil
		}
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
	Scope       string   `json:"scope"`
	ProjectDirs []string `json:"projectDirs"`
}

type approvalView struct {
	ID           string          `json:"id"`
	SessionID    string          `json:"sessionID"`
	TurnID       string          `json:"turnID"`
	CallID       string          `json:"callID,omitempty"`
	ApprovalKind string          `json:"approvalKind"`
	TargetMode   string          `json:"targetMode,omitempty"`
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
			TargetMode:   publicApprovalTargetMode(approval),
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
	if err := s.engine.ApproveApproval(c.Request.Context(), id, approvalID, scope, req.ProjectDirs); err != nil {
		if errors.Is(err, engine.ErrApprovalNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "not_found"})
			return nil
		}
		if errors.Is(err, engine.ErrProjectDirsRequired) {
			return badRequest(c, "project_dirs_required")
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusAccepted, map[string]string{"status": "approved"})
	return nil
}

func publicApprovalTargetMode(approval engine.ApprovalRequest) string {
	if approval.Kind == engine.ApprovalKindCapability && approval.TargetMode == store.ModeProject {
		return "project"
	}
	return string(approval.TargetMode)
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
	settings, ok := s.config.(interface {
		SetSettings(context.Context, map[string]string) error
	})
	if !ok {
		return badRequest(c, "settings are read-only")
	}
	if err := settings.SetSettings(c.Request.Context(), kv); err != nil {
		if errors.Is(err, config.ErrInvalidSetting) {
			return badRequest(c, err.Error())
		}
		return s.fail(c, err)
	}
	c.String(http.StatusNoContent, "")
	return nil
}

type audioConfigView struct {
	Path   string             `json:"path"`
	Config config.AudioConfig `json:"config"`
}

func (s *Server) getAudioConfig(c *cart.Context) error {
	audio, ok := s.config.(interface {
		Audio(context.Context) (config.AudioConfig, error)
	})
	if !ok {
		return badRequest(c, "audio config is read-only")
	}
	cfg, err := audio.Audio(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, audioConfigView{Path: filepath.Join(s.home, "config", "audio.yaml"), Config: cfg})
	return nil
}

func (s *Server) putAudioConfig(c *cart.Context) error {
	var req config.AudioConfig
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	audio, ok := s.config.(interface {
		SetAudio(context.Context, config.AudioConfig) (config.AudioConfig, error)
	})
	if !ok {
		return badRequest(c, "audio config is read-only")
	}
	cfg, err := audio.SetAudio(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, config.ErrInvalidSetting) {
			return badRequest(c, err.Error())
		}
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, audioConfigView{Path: filepath.Join(s.home, "config", "audio.yaml"), Config: cfg})
	return nil
}

func (s *Server) getAudioRuntime(c *cart.Context) error {
	if s.audioRT == nil {
		return badRequest(c, "audio runtime installer unavailable")
	}
	cfg, err := s.currentAudioConfig(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, s.audioRT.Status(c.Request.Context(), cfg))
	return nil
}

func (s *Server) startAudioRuntimeInstall(c *cart.Context) error {
	if s.audioRT == nil {
		return badRequest(c, "audio runtime installer unavailable")
	}
	cfg, err := s.currentAudioConfig(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, s.audioRT.Start(c.Request.Context(), cfg))
	return nil
}

func (s *Server) cancelAudioRuntimeInstall(c *cart.Context) error {
	if s.audioRT == nil {
		return badRequest(c, "audio runtime installer unavailable")
	}
	c.JSON(http.StatusOK, s.audioRT.Cancel())
	return nil
}

func (s *Server) currentAudioConfig(ctx context.Context) (config.AudioConfig, error) {
	audio, ok := s.config.(interface {
		Audio(context.Context) (config.AudioConfig, error)
	})
	if !ok {
		return config.AudioConfig{}, errors.New("audio config is read-only")
	}
	cfg, err := audio.Audio(ctx)
	if err != nil {
		return config.AudioConfig{}, err
	}
	return cfg.WithDefaults(), nil
}

type clearASRRecordingsResponse struct {
	OK           bool `json:"ok"`
	Attachments  int  `json:"attachments"`
	Messages     int  `json:"messages"`
	QueuedInputs int  `json:"queuedInputs"`
	DeleteErrors int  `json:"deleteErrors,omitempty"`
}

func (s *Server) clearASRRecordings(c *cart.Context) error {
	result, err := s.store.RemoveAttachmentsByOrigin(c.Request.Context(), attachment.OriginASRAudio)
	if err != nil {
		return s.fail(c, err)
	}
	deleteErrors := 0
	if strings.TrimSpace(s.home) != "" {
		service := attachment.NewService(s.home)
		seen := make(map[string]bool, len(result.Attachments))
		for _, item := range result.Attachments {
			key := item.SessionID + "\x00" + item.Attachment.AttachmentKey
			if seen[key] {
				continue
			}
			seen[key] = true
			if err := service.Delete(item.SessionID, item.Attachment.AttachmentKey); err != nil {
				deleteErrors++
			}
		}
	}
	c.JSON(http.StatusOK, clearASRRecordingsResponse{
		OK:           true,
		Attachments:  len(result.Attachments),
		Messages:     result.MessageCount,
		QueuedInputs: result.QueuedInputCount,
		DeleteErrors: deleteErrors,
	})
	return nil
}

func (s *Server) getUserPrompt(c *cart.Context) error {
	prompts, ok := s.config.(interface {
		UserPrompt(context.Context) (*config.UserPromptView, error)
	})
	if !ok {
		return badRequest(c, "user prompt is read-only")
	}
	view, err := prompts.UserPrompt(c.Request.Context())
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, view)
	return nil
}

func (s *Server) putUserPrompt(c *cart.Context) error {
	var req struct {
		Content string `json:"content"`
	}
	if err := decode(c, &req); err != nil {
		return badRequest(c, "invalid json body")
	}
	prompts, ok := s.config.(interface {
		SetUserPrompt(context.Context, string) (*config.UserPromptView, error)
	})
	if !ok {
		return badRequest(c, "user prompt is read-only")
	}
	view, err := prompts.SetUserPrompt(c.Request.Context(), req.Content)
	if err != nil {
		return s.fail(c, err)
	}
	c.JSON(http.StatusOK, view)
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
	if errors.Is(err, store.ErrInvalidProject) {
		c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid_project"})
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

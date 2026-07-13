// Package engine 实现 per-session turn 状态机,是 messages 的唯一写入方
// (docs/phase-1-plan.md 第 6 节)。turn lifecycle 事件全部由本包生成,
// provider 只供模型流(AGENTS.md 硬约束 17)。
package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const defaultMaxToolLoops = 16
const maxToolkitLoads = 2
const toolCallTimeout = 60 * time.Second
const streamEventCoalesceInterval = 30 * time.Millisecond

var (
	// ErrTurnRunning:第一阶段同一 session 不允许并发 turn,API 映射 409。
	ErrTurnRunning = store.ErrTurnRunning
	// ErrNoRunningTurn:cancel 时没有进行中的 turn,API 映射 409。
	ErrNoRunningTurn = errors.New("engine: no running turn")
	ErrEmptyInput    = errors.New("engine: empty text or clientMessageID")
	// ErrNoModel:会话未解析出可用 provider/model,且当前没有可 fallback 的配置。
	// API 映射 400 "no_model"。
	ErrNoModel        = errors.New("engine: no model configured for session")
	ErrProviderConfig = errors.New("engine: provider config unavailable")
	ErrCompactRunning = errors.New("engine: compact already running")
	ErrCompactEmpty   = errors.New("engine: not enough history to compact")
)

// Resolver 把 provider profile 名解析为 client 实例;
// 生产实现是 provider/registry,--mock 与测试用 registry.Static。
type Resolver interface {
	Resolve(ctx context.Context, name string) (provider.Client, error)
}

type ConfigSource interface {
	Settings(ctx context.Context) (map[string]string, error)
	ListProviderProfiles(ctx context.Context) ([]*store.ProviderProfile, error)
	GetProviderProfile(ctx context.Context, name string) (*store.ProviderProfile, error)
}

type AppSource interface {
	ListDefinitions(ctx context.Context) ([]*app.Definition, error)
	ReadSkill(ctx context.Context, appID, skillID string) (*app.SkillDetail, error)
}

type appEndpointResolver interface {
	ResolveEndpoint(ctx context.Context, sessionID, endpointName, connectionRef string) (*app.EndpointBinding, error)
}

type emptyConfig struct{}

func (emptyConfig) Settings(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (emptyConfig) ListProviderProfiles(context.Context) ([]*store.ProviderProfile, error) {
	return nil, nil
}

func (emptyConfig) GetProviderProfile(context.Context, string) (*store.ProviderProfile, error) {
	return nil, store.ErrNotFound
}

type Engine struct {
	store    store.Store
	config   ConfigSource
	hub      *event.Hub
	resolver Resolver
	builder  *contextbuilder.Builder
	tools    tool.Runner
	apps     AppSource

	promptSource   contextbuilder.PromptSource
	attachmentHome string

	// auxCtx 是辅助 goroutine(自动标题,将来工具相关后台任务)的基 ctx;
	// Stop() 取消它,优雅退出时这些 best-effort 任务立即中断,不拖住
	// Wait()。turn goroutine 不挂在这上面——turn 要写完 canonical 才退,
	// 由各自 turnCtx + provider 超时兜底。
	auxCtx    context.Context
	auxCancel context.CancelFunc

	mu                sync.Mutex
	running           map[string]context.CancelFunc // sessionID → 当前 turn 的 cancel
	approvals         map[string]*pendingApproval
	turnProjectAccess map[string]ProjectAccessGrant // turnID → 本轮临时目录授权
	queuedRuntimeIDs  map[string]string             // queued input → originating UI runtime
	wg                sync.WaitGroup
	compactMu         sync.Mutex
	toolCloseOnce     sync.Once
}

type Option func(*Engine)

func WithTools(runner tool.Runner) Option {
	return func(e *Engine) {
		e.tools = runner
	}
}

func WithApps(source AppSource) Option {
	return func(e *Engine) {
		e.apps = source
	}
}

func WithPromptSource(source contextbuilder.PromptSource) Option {
	return func(e *Engine) {
		e.promptSource = source
		e.rebuildBuilder()
	}
}

func WithAttachmentHome(home string) Option {
	return func(e *Engine) {
		e.attachmentHome = strings.TrimSpace(home)
		e.rebuildBuilder()
	}
}

func (e *Engine) rebuildBuilder() {
	e.builder = contextbuilder.New(e.store, e.promptSource, contextbuilder.WithAttachmentHome(e.attachmentHome))
}

func New(s store.Store, hub *event.Hub, resolver Resolver, cfg ConfigSource, opts ...Option) *Engine {
	if cfg == nil {
		cfg = emptyConfig{}
	}
	auxCtx, auxCancel := context.WithCancel(context.Background())
	e := &Engine{
		store:             s,
		config:            cfg,
		hub:               hub,
		resolver:          resolver,
		builder:           contextbuilder.New(s, nil),
		auxCtx:            auxCtx,
		auxCancel:         auxCancel,
		running:           make(map[string]context.CancelFunc),
		approvals:         make(map[string]*pendingApproval),
		turnProjectAccess: make(map[string]ProjectAccessGrant),
		queuedRuntimeIDs:  make(map[string]string),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Stop 取消辅助 goroutine 的基 ctx(幂等);在 Wait() 前调用,
// 让 best-effort 后台任务(自动标题等)优雅退出时立即收手。
func (e *Engine) Stop() {
	e.auxCancel()
	e.toolCloseOnce.Do(func() {
		if closer, ok := e.tools.(tool.ResourceCloser); ok {
			if err := closer.Close(); err != nil {
				slog.Warn("engine: close tool resources failed", "err", err)
			}
		}
	})
}

func (e *Engine) ReleaseSessionResources(sessionID string) {
	e.mu.Lock()
	for key := range e.queuedRuntimeIDs {
		if strings.HasPrefix(key, strings.TrimSpace(sessionID)+"\x00") {
			delete(e.queuedRuntimeIDs, key)
		}
	}
	e.mu.Unlock()
	if cleaner, ok := e.tools.(tool.SessionResourceCleaner); ok {
		cleaner.CloseSession(strings.TrimSpace(sessionID))
	}
}

func (e *Engine) BackgroundProcesses(sessionID string) []tool.BackgroundProcessSnapshot {
	if controller, ok := e.tools.(tool.BackgroundProcessController); ok {
		return controller.ListBackgroundProcesses(strings.TrimSpace(sessionID))
	}
	return []tool.BackgroundProcessSnapshot{}
}

func (e *Engine) BackgroundProcessCount(sessionID string) int {
	if controller, ok := e.tools.(tool.BackgroundProcessController); ok {
		return controller.BackgroundProcessCount(strings.TrimSpace(sessionID))
	}
	return 0
}

func (e *Engine) ReadBackgroundProcess(sessionID, processID string, offset int64, maxBytes, tailBytes int) (tool.BackgroundProcessLogSnapshot, error) {
	controller, ok := e.tools.(tool.BackgroundProcessController)
	if !ok {
		return tool.BackgroundProcessLogSnapshot{}, tool.ErrBackgroundProcessNotFound
	}
	return controller.ReadBackgroundProcess(strings.TrimSpace(sessionID), strings.TrimSpace(processID), offset, maxBytes, tailBytes)
}

func (e *Engine) StopBackgroundProcess(sessionID, processID string) (tool.BackgroundProcessSnapshot, error) {
	controller, ok := e.tools.(tool.BackgroundProcessController)
	if !ok {
		return tool.BackgroundProcessSnapshot{}, tool.ErrBackgroundProcessNotFound
	}
	return controller.StopBackgroundProcess(strings.TrimSpace(sessionID), strings.TrimSpace(processID))
}

type SubmitInput struct {
	SessionID       string
	ClientMessageID string
	Text            string
	Parts           []store.ContentPart
	Kind            string
	ReasoningEffort string
}

type SubmitResult struct {
	// Duplicate:同一 clientMessageID 重复提交,返回已有 turn,不触发新 turn。
	Duplicate       bool   `json:"duplicate,omitempty"`
	Queued          bool   `json:"queued,omitempty"`
	TurnID          string `json:"turnID,omitempty"`
	UserMessageID   string `json:"userMessageID,omitempty"`
	Status          string `json:"status,omitempty"`
	ClientMessageID string `json:"clientMessageID,omitempty"`
}

type SessionUsageInfo struct {
	SessionID                       string    `json:"sessionID"`
	ContextWindow                   int       `json:"contextWindow"`
	ContextEstimatedTokens          int       `json:"contextEstimatedTokens"`
	MessageEstimatedTokens          int       `json:"messageEstimatedTokens"`
	PromptOverheadEstimatedTokens   int       `json:"promptOverheadEstimatedTokens"`
	SystemPromptEstimatedTokens     int       `json:"systemPromptEstimatedTokens"`
	ToolsSchemaEstimatedTokens      int       `json:"toolsSchemaEstimatedTokens"`
	AutoCompactThresholdTokens      int       `json:"autoCompactThresholdTokens"`
	RequestCount                    int       `json:"requestCount"`
	LastPromptTokens                int       `json:"lastPromptTokens"`
	LastInputUncachedTokens         int       `json:"lastInputUncachedTokens"`
	LastInputCachedTokens           int       `json:"lastInputCachedTokens"`
	LastCacheCreationTokens         int       `json:"lastCacheCreationTokens"`
	LastOutputContentTokens         int       `json:"lastOutputContentTokens"`
	LastOutputReasoningTokens       int       `json:"lastOutputReasoningTokens"`
	LastOutputTokens                int       `json:"lastOutputTokens"`
	CumulativeInputUncachedTokens   int       `json:"cumulativeInputUncachedTokens"`
	CumulativeInputCachedTokens     int       `json:"cumulativeInputCachedTokens"`
	CumulativeCacheCreationTokens   int       `json:"cumulativeCacheCreationTokens"`
	CumulativeOutputContentTokens   int       `json:"cumulativeOutputContentTokens"`
	CumulativeOutputReasoningTokens int       `json:"cumulativeOutputReasoningTokens"`
	CumulativeInputTokens           int       `json:"cumulativeInputTokens"`
	CumulativeOutputTokens          int       `json:"cumulativeOutputTokens"`
	CumulativeTotalTokens           int       `json:"cumulativeTotalTokens"`
	UpdatedAt                       time.Time `json:"updatedAt,omitempty"`
}

func (e *Engine) AudioInputSupported(ctx context.Context, sessionID string) (bool, error) {
	sess, err := e.store.GetSession(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return false, err
	}
	resolved, err := e.resolveModel(ctx, sess)
	if err != nil {
		return false, err
	}
	return resolved.config.Capabilities != nil && resolved.config.Capabilities.Audio, nil
}

type resolvedModel struct {
	providerName  string
	providerBrand string
	protocol      string
	model         string
	mode          store.AgentMode
	config        provider.ModelConfig
	configJSON    json.RawMessage
}

func (e *Engine) Submit(ctx context.Context, in SubmitInput) (*SubmitResult, error) {
	kind := strings.TrimSpace(in.Kind)
	if in.ClientMessageID == "" {
		return nil, ErrEmptyInput
	}
	switch kind {
	case "", "user":
		in.Parts = store.UserInputParts(in.Text, in.Parts)
		in.Text = store.TextFromParts(in.Parts)
		if len(in.Parts) == 0 {
			return nil, ErrEmptyInput
		}
	case "system":
		if strings.TrimSpace(in.Text) == "" || len(in.Parts) > 0 {
			return nil, ErrEmptyInput
		}
	default:
		return nil, ErrEmptyInput
	}
	sess, err := e.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return nil, err
	}

	resolved, err := e.resolveModel(ctx, sess)
	if err != nil {
		return nil, err
	}
	resolved.mode = initialMode(sess)
	if err := resolved.applyReasoningEffort(activeReasoningEffort(in.ReasoningEffort, sess, resolved)); err != nil {
		return nil, err
	}
	if err := resolved.normalizeReasoningOptions(); err != nil {
		return nil, err
	}
	client, err := e.resolver.Resolve(ctx, resolved.providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderConfig, err)
	}
	switch kind {
	case "", "user":
	case "system":
		return e.submitSystem(ctx, in, resolved, client)
	}

	queued, err := e.store.HasQueuedInputs(ctx, in.SessionID)
	if err != nil {
		return nil, err
	}
	if queued {
		return e.queueSubmit(ctx, in, resolved)
	}

	res, err := e.store.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       in.SessionID,
		TurnID:          store.NewID("turn"),
		UserMessageID:   store.NewID("msg"),
		ClientMessageID: in.ClientMessageID,
		UserText:        in.Text,
		UserParts:       in.Parts,
		Provider:        resolved.providerName,
		Model:           resolved.model,
		Mode:            resolved.mode,
		ModelConfig:     resolved.configJSON,
	})
	if errors.Is(err, store.ErrTurnRunning) {
		return e.queueSubmit(ctx, in, resolved)
	}
	if err != nil {
		return nil, err
	}
	if res.Duplicate {
		out := &SubmitResult{Duplicate: true, TurnID: res.Turn.ID}
		if res.UserMessage != nil {
			out.UserMessageID = res.UserMessage.ID
		}
		return out, nil
	}
	// 先注册 cancel 再 publish started:否则收到 turn.started 立刻 cancel 的
	// 客户端可能落在注册之前,错拿 no_running_turn。turn 生命周期长于 HTTP
	// 请求,不继承请求 ctx;取消只走 Cancel()。
	turnCtx, cancel := context.WithCancel(app.WithRuntimeID(context.Background(), app.RuntimeIDFromContext(ctx)))
	e.mu.Lock()
	e.running[in.SessionID] = cancel
	e.mu.Unlock()

	e.hub.Publish(*res.StartedEvent)

	// 空标题会话:首条消息触发自动标题(provisional + 异步 LLM),
	// 与 turn 生命周期解耦(titler.go)
	if sess.Title == "" && strings.TrimSpace(in.Text) != "" {
		e.autoTitle(in.SessionID, resolved.providerName, resolved.model, resolved.config, in.Text)
	}

	e.wg.Add(1)
	go e.runTurn(turnCtx, in.SessionID, res.Turn.ID, resolved, client)

	return &SubmitResult{TurnID: res.Turn.ID, UserMessageID: res.UserMessage.ID}, nil
}

func (e *Engine) submitSystem(ctx context.Context, in SubmitInput, resolved *resolvedModel, client provider.Client) (*SubmitResult, error) {
	queued, err := e.store.HasQueuedInputs(ctx, in.SessionID)
	if err != nil {
		return nil, err
	}
	if queued {
		return nil, ErrTurnRunning
	}
	res, err := e.store.BeginSystemTurn(ctx, store.BeginSystemTurnInput{
		SessionID:       in.SessionID,
		TurnID:          store.NewID("turn"),
		SystemMessageID: store.NewID("msg"),
		ClientMessageID: in.ClientMessageID,
		Text:            in.Text,
		Provider:        resolved.providerName,
		Model:           resolved.model,
		Mode:            resolved.mode,
		ModelConfig:     resolved.configJSON,
	})
	if errors.Is(err, store.ErrTurnRunning) {
		return nil, ErrTurnRunning
	}
	if err != nil {
		return nil, err
	}
	if res.Duplicate {
		return &SubmitResult{Duplicate: true, TurnID: res.Turn.ID}, nil
	}
	turnCtx, cancel := context.WithCancel(app.WithRuntimeID(context.Background(), app.RuntimeIDFromContext(ctx)))
	e.mu.Lock()
	e.running[in.SessionID] = cancel
	e.mu.Unlock()

	e.hub.Publish(*res.StartedEvent)

	e.wg.Add(1)
	go e.runTurn(turnCtx, in.SessionID, res.Turn.ID, resolved, client)

	return &SubmitResult{TurnID: res.Turn.ID}, nil
}

func (e *Engine) SessionUsage(ctx context.Context, sessionID string) (*SessionUsageInfo, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	resolved, err := e.resolveModel(ctx, sess)
	if err != nil {
		return nil, err
	}
	mode := initialMode(sess)
	req, err := e.builder.Build(ctx, sessionID, resolved.model, string(mode), resolved.config)
	if err != nil {
		return nil, err
	}
	req.Config = resolved.config
	if modelSupportsTools(resolved.config) {
		defs, catalog, err := e.toolDefinitions(ctx, sessionID, mode, nil)
		if err != nil {
			return nil, err
		}
		req.Tools = defs
		req.System = appendToolkitIndex(req.System, tool.ToolkitIndex(mode, catalog))
	}
	estimate := contextbuilder.EstimateRequest(req)
	stat, err := e.store.SessionUsage(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	contextWindow := resolved.config.ContextWindow
	threshold := 0
	if contextWindow > 0 {
		percent := e.autoCompactThresholdPercent(ctx)
		if percent > 0 {
			threshold = contextWindow * percent / 100
		}
	}
	return &SessionUsageInfo{
		SessionID:                       sessionID,
		ContextWindow:                   contextWindow,
		ContextEstimatedTokens:          estimate.Total(),
		MessageEstimatedTokens:          estimate.MessageTokens,
		PromptOverheadEstimatedTokens:   estimate.SystemTokens + estimate.ToolsTokens,
		SystemPromptEstimatedTokens:     estimate.SystemTokens,
		ToolsSchemaEstimatedTokens:      estimate.ToolsTokens,
		AutoCompactThresholdTokens:      threshold,
		RequestCount:                    stat.RequestCount,
		LastPromptTokens:                stat.LastInputTokens(),
		LastInputUncachedTokens:         stat.LastInputUncachedTokens,
		LastInputCachedTokens:           stat.LastInputCachedTokens,
		LastCacheCreationTokens:         stat.LastCacheCreationTokens,
		LastOutputContentTokens:         stat.LastOutputContentTokens,
		LastOutputReasoningTokens:       stat.LastOutputReasoningTokens,
		LastOutputTokens:                stat.LastOutputTokens(),
		CumulativeInputUncachedTokens:   stat.CumulativeInputUncachedTokens,
		CumulativeInputCachedTokens:     stat.CumulativeInputCachedTokens,
		CumulativeCacheCreationTokens:   stat.CumulativeCacheCreationTokens,
		CumulativeOutputContentTokens:   stat.CumulativeOutputContentTokens,
		CumulativeOutputReasoningTokens: stat.CumulativeOutputReasoningTokens,
		CumulativeInputTokens:           stat.CumulativeInputTokens(),
		CumulativeOutputTokens:          stat.CumulativeOutputTokens(),
		CumulativeTotalTokens:           stat.CumulativeTotalTokens(),
		UpdatedAt:                       stat.UpdatedAt,
	}, nil
}

func (e *Engine) queueSubmit(ctx context.Context, in SubmitInput, resolved *resolvedModel) (*SubmitResult, error) {
	res, err := e.store.QueueInput(ctx, store.QueueInputInput{
		SessionID:       in.SessionID,
		ClientMessageID: in.ClientMessageID,
		Text:            strings.TrimSpace(in.Text),
		Parts:           in.Parts,
		Provider:        resolved.providerName,
		Model:           resolved.model,
		Mode:            resolved.mode,
		ModelConfig:     resolved.configJSON,
	})
	if err != nil {
		return nil, err
	}
	if res.ExistingTurn != nil {
		return &SubmitResult{Duplicate: true, TurnID: res.ExistingTurn.ID}, nil
	}
	e.rememberQueuedRuntime(in.SessionID, in.ClientMessageID, app.RuntimeIDFromContext(ctx))
	if res.QueuedEvent != nil {
		e.hub.Publish(*res.QueuedEvent)
	}
	out := &SubmitResult{Queued: true, ClientMessageID: in.ClientMessageID, Status: string(store.QueuedInputQueued)}
	if res.Duplicate {
		out.Duplicate = true
	}
	if res.Input != nil {
		out.Status = string(res.Input.Status)
	}
	return out, nil
}

func queuedRuntimeKey(sessionID, clientMessageID string) string {
	return strings.TrimSpace(sessionID) + "\x00" + strings.TrimSpace(clientMessageID)
}

func (e *Engine) rememberQueuedRuntime(sessionID, clientMessageID, runtimeID string) {
	key := queuedRuntimeKey(sessionID, clientMessageID)
	if key == "\x00" {
		return
	}
	e.mu.Lock()
	if runtimeID = strings.TrimSpace(runtimeID); runtimeID != "" {
		e.queuedRuntimeIDs[key] = runtimeID
	} else {
		delete(e.queuedRuntimeIDs, key)
	}
	e.mu.Unlock()
}

func (e *Engine) takeQueuedRuntime(sessionID, clientMessageID string) string {
	key := queuedRuntimeKey(sessionID, clientMessageID)
	e.mu.Lock()
	runtimeID := e.queuedRuntimeIDs[key]
	delete(e.queuedRuntimeIDs, key)
	e.mu.Unlock()
	return runtimeID
}

func (e *Engine) resolveModel(ctx context.Context, sess *store.Session) (*resolvedModel, error) {
	// provider / model 在提交时刻解析并随 turn 快照,改配置不影响进行中的 turn。
	// 若会话引用的 profile/model 已被删除,自动落到当前第一个可用配置。
	providerName := strings.TrimSpace(sess.Provider)
	model := strings.TrimSpace(sess.Model)
	if providerName == "" || model == "" {
		return e.fallbackModel(ctx, sess)
	}

	var cfg provider.ModelConfig
	p, err := e.config.GetProviderProfile(ctx, providerName)
	if err != nil {
		if fallback, fallbackErr := e.fallbackModel(ctx, sess); fallbackErr == nil {
			return fallback, nil
		} else if !errors.Is(fallbackErr, ErrNoModel) {
			return nil, fallbackErr
		}
		return resolvedModelFromParts(providerName, "", "", model, cfg)
	}
	if len(p.Models) > 0 && !p.HasModel(model) {
		return e.fallbackModel(ctx, sess)
	}
	if entry, ok := p.ModelByID(model); ok {
		cfg = modelConfigFromEntry(entry)
	}
	return resolvedModelFromParts(providerName, p.Brand, p.Protocol, model, cfg)
}

func (e *Engine) fallbackModel(ctx context.Context, sess *store.Session) (*resolvedModel, error) {
	profiles, err := e.config.ListProviderProfiles(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if p == nil || len(p.Models) == 0 {
			continue
		}
		providerName := strings.TrimSpace(p.ProfileID())
		model := strings.TrimSpace(p.Models[0].ID)
		if providerName == "" || model == "" {
			continue
		}
		cfg := modelConfigFromEntry(p.Models[0])
		cfgJSON, err := json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		if sess != nil && (strings.TrimSpace(sess.Provider) != providerName || strings.TrimSpace(sess.Model) != model) {
			_, _ = e.store.UpdateSession(ctx, sess.ID, store.SessionUpdate{Provider: &providerName, Model: &model})
		}
		return &resolvedModel{
			providerName:  providerName,
			providerBrand: p.Brand,
			protocol:      p.Protocol,
			model:         model,
			config:        cfg,
			configJSON:    cfgJSON,
		}, nil
	}
	return nil, ErrNoModel
}

func resolvedModelFromParts(providerName, brand, protocol, model string, cfg provider.ModelConfig) (*resolvedModel, error) {
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return &resolvedModel{
		providerName:  providerName,
		providerBrand: brand,
		protocol:      protocol,
		model:         model,
		config:        cfg,
		configJSON:    cfgJSON,
	}, nil
}

func activeReasoningEffort(requestValue string, sess *store.Session, resolved *resolvedModel) string {
	if effort := strings.TrimSpace(requestValue); effort != "" {
		return effort
	}
	if sess == nil || resolved == nil {
		return ""
	}
	if strings.TrimSpace(sess.ReasoningModelKey) != resolved.modelKey() {
		return ""
	}
	return strings.TrimSpace(sess.ReasoningEffort)
}

func (r *resolvedModel) modelKey() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.providerName) + ":" + strings.TrimSpace(r.model)
}

func (r *resolvedModel) applyReasoningEffort(value string) error {
	effort := strings.TrimSpace(value)
	if effort == "" || effort == "auto" {
		return nil
	}
	target := r.reasoningTarget()
	switch target {
	case "deepseek-openai":
		if r.config.ProviderOptions == nil {
			r.config.ProviderOptions = &provider.ModelProviderOptions{}
		}
		if r.config.ProviderOptions.OpenAI == nil {
			r.config.ProviderOptions.OpenAI = map[string]any{}
		}
		if effort != "low" && effort != "medium" && effort != "high" && effort != "xhigh" {
			return nil
		}
		r.config.ProviderOptions.OpenAI["reasoning_effort"] = effort
	case "deepseek-anthropic":
		mapped, ok := deepSeekReasoningEffort(effort)
		if !ok {
			return nil
		}
		if r.config.ProviderOptions == nil {
			r.config.ProviderOptions = &provider.ModelProviderOptions{}
		}
		if r.config.ProviderOptions.Anthropic == nil {
			r.config.ProviderOptions.Anthropic = map[string]any{}
		}
		r.config.ProviderOptions.Anthropic["output_config"] = map[string]any{"effort": mapped}
	case "google":
		if effort != "low" && effort != "medium" && effort != "high" {
			return nil
		}
		if r.config.ProviderOptions == nil {
			r.config.ProviderOptions = &provider.ModelProviderOptions{}
		}
		if r.config.ProviderOptions.Google == nil {
			r.config.ProviderOptions.Google = map[string]any{}
		}
		existingThinking, _ := r.config.ProviderOptions.Google["thinking"].(map[string]any)
		thinking := map[string]any{}
		for k, v := range existingThinking {
			thinking[k] = v
		}
		thinking["include_thoughts"] = true
		thinking["level"] = effort
		r.config.ProviderOptions.Google["thinking"] = thinking
	case "openai":
		if effort != "none" && effort != "low" && effort != "medium" && effort != "high" && effort != "xhigh" {
			return nil
		}
		if r.config.ProviderOptions == nil {
			r.config.ProviderOptions = &provider.ModelProviderOptions{}
		}
		if r.config.ProviderOptions.OpenAI == nil {
			r.config.ProviderOptions.OpenAI = map[string]any{}
		}
		r.config.ProviderOptions.OpenAI["reasoning_effort"] = effort
	default:
		return nil
	}
	cfgJSON, err := json.Marshal(r.config)
	if err != nil {
		return err
	}
	r.configJSON = cfgJSON
	return nil
}

func (r *resolvedModel) reasoningTarget() string {
	if r.isDeepSeek() {
		switch strings.TrimSpace(r.protocol) {
		case "openai-compatible", "openai-responses":
			return "deepseek-openai"
		case "anthropic":
			return "deepseek-anthropic"
		}
	}
	switch strings.TrimSpace(r.protocol) {
	case "google":
		return "google"
	case "openai-compatible", "openai-responses":
		return "openai"
	}
	if r.config.ProviderOptions != nil {
		if r.config.ProviderOptions.Google != nil {
			return "google"
		}
		if r.config.ProviderOptions.OpenAI != nil {
			return "openai"
		}
	}
	return ""
}

func (r *resolvedModel) normalizeReasoningOptions() error {
	if r == nil || !r.isDeepSeek() || r.config.ProviderOptions == nil {
		return nil
	}
	switch r.reasoningTarget() {
	case "deepseek-openai":
		opts := r.config.ProviderOptions.OpenAI
		if opts == nil {
			return nil
		}
		_, hadThinking := opts["thinking"]
		delete(opts, "thinking")
		effort, _ := provider.StringOption(opts, "reasoning_effort")
		if effort == "" {
			if hadThinking {
				return r.refreshConfigJSON()
			}
			return nil
		}
		if effort != "low" && effort != "medium" && effort != "high" && effort != "xhigh" {
			delete(opts, "reasoning_effort")
		}
	case "deepseek-anthropic":
		opts := r.config.ProviderOptions.Anthropic
		outputConfig, _ := opts["output_config"].(map[string]any)
		effort, _ := provider.StringOption(outputConfig, "effort")
		mapped, ok := deepSeekReasoningEffort(effort)
		if !ok {
			return nil
		}
		opts["output_config"] = map[string]any{"effort": mapped}
	default:
		return nil
	}
	return r.refreshConfigJSON()
}

func (r *resolvedModel) refreshConfigJSON() error {
	cfgJSON, err := json.Marshal(r.config)
	if err != nil {
		return err
	}
	r.configJSON = cfgJSON
	return nil
}

func (r *resolvedModel) isDeepSeek() bool {
	if r == nil {
		return false
	}
	return strings.TrimSpace(r.providerBrand) == "deepseek" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(r.model)), "deepseek-v4-")
}

func deepSeekReasoningEffort(effort string) (string, bool) {
	switch strings.TrimSpace(effort) {
	case "low", "medium", "high":
		return "high", true
	case "xhigh", "max":
		return "max", true
	default:
		return "", false
	}
}

func initialMode(sess *store.Session) store.AgentMode {
	if sess != nil && sess.ModeLease == store.ModeLeaseSession {
		mode := store.NormalizeAgentMode(sess.ActiveMode)
		if store.ValidAgentMode(mode) {
			return mode
		}
	}
	return store.ModeChat
}

func modelConfigFromEntry(m store.ProviderModel) provider.ModelConfig {
	cfg := provider.ModelConfig{
		ContextWindow: m.ContextWindow,
	}
	if m.Capabilities != nil {
		cfg.Capabilities = &provider.ModelCapabilities{
			Image: m.Capabilities.Image,
			Audio: m.Capabilities.Audio,
			Tools: m.Capabilities.Tools,
		}
	}
	if m.Limits != nil {
		cfg.Limits = &provider.ModelLimits{
			MaxOutputTokens: m.Limits.MaxOutputTokens,
			MaxToolLoops:    m.Limits.MaxToolLoops,
		}
	}
	if m.ProviderOptions != nil {
		cfg.ProviderOptions = &provider.ModelProviderOptions{
			OpenAI:    cloneOptions(m.ProviderOptions.OpenAI),
			Google:    cloneOptions(m.ProviderOptions.Google),
			Anthropic: cloneOptions(m.ProviderOptions.Anthropic),
		}
	}
	return cfg
}

func cloneOptions(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// Recover 把上次进程退出时残留的 running turn 收尾为 failed,在 daemon
// 启动、开始服务之前调用一次。delta 不落库,半截输出已随进程丢失,只能
// 如实标记失败;落库的 turn.failed 事件让重连客户端续传后 refetch 对齐。
func (e *Engine) Recover(ctx context.Context) error {
	turns, err := e.store.RunningTurns(ctx)
	if err != nil {
		return err
	}
	for _, t := range turns {
		res, err := e.store.FinishTurn(ctx, store.FinishTurnInput{
			TurnID: t.ID,
			Status: store.TurnFailed,
			Error:  "interrupted by daemon restart",
		})
		if err != nil {
			return fmt.Errorf("recover turn %s: %w", t.ID, err)
		}
		e.hub.Publish(*res.FinalEvent)
	}
	if len(turns) > 0 {
		slog.Info("engine: recovered interrupted turns", "count", len(turns))
	}
	sessions, err := e.store.QueuedSessions(ctx)
	if err != nil {
		return err
	}
	for _, sessionID := range sessions {
		e.TryDrainQueued(sessionID)
	}
	return nil
}

// Cancel 中断 session 当前 turn;收尾(落库 + final 事件)由 runTurn 完成。
func (e *Engine) Cancel(sessionID string) error {
	e.mu.Lock()
	cancel, ok := e.running[sessionID]
	e.mu.Unlock()
	if !ok {
		return ErrNoRunningTurn
	}
	cancel()
	return nil
}

// Wait 等待所有进行中的 turn 收尾,服务优雅退出。
func (e *Engine) Wait() { e.wg.Wait() }

func (e *Engine) runTurn(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, client provider.Client) {
	defer e.wg.Done()

	var parts turnPartAccumulator
	status, errMsg, mode := e.streamTurn(ctx, sessionID, turnID, resolved, client, &parts)
	e.finishTurn(sessionID, turnID, mode, status, errMsg, parts.UncommittedParts())
}

func (e *Engine) finishTurn(sessionID, turnID string, mode store.AgentMode, status store.TurnStatus, errMsg string, assistantParts []store.ContentPart) {
	in := store.FinishTurnInput{TurnID: turnID, Status: status, Mode: mode, Error: errMsg}
	if len(assistantParts) > 0 {
		in.AssistantParts = assistantParts
	}
	if status != store.TurnCompleted && len(assistantParts) > 0 {
		// 半截输出保留为 canonical message 并标记 interrupted
		// (docs/technology-decisions.md 第 14 节的当前倾向)。
		in.Interrupted = true
	}
	res, err := e.store.FinishTurn(context.Background(), in)
	if err != nil {
		// session 在 turn 进行中被删除:turn 已随级联删除消失,收尾无处可写,
		// 静默(删除路径已 cancel 本 turn,见 api deleteSession)。
		if errors.Is(err, store.ErrNotFound) {
			slog.Debug("engine: finish turn skipped, session/turn gone", "turnID", turnID)
			e.clearRunning(sessionID, turnID)
			return
		}
		slog.Error("engine: finish turn", "turnID", turnID, "err", err)
		e.clearRunning(sessionID, turnID)
		return
	}
	e.clearRunning(sessionID, turnID)
	e.hub.Publish(*res.FinalEvent)
	e.TryDrainQueued(sessionID)
	if status == store.TurnCompleted {
		e.scheduleAutoCompact(sessionID)
	}
}

func (e *Engine) clearRunning(sessionID, turnID string) {
	e.mu.Lock()
	delete(e.running, sessionID)
	delete(e.turnProjectAccess, turnID)
	e.mu.Unlock()
}

func (e *Engine) TryDrainQueued(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	res, err := e.store.PromoteNextQueuedInput(context.Background(), store.PromoteQueuedInputInput{
		SessionID:     sessionID,
		TurnID:        store.NewID("turn"),
		UserMessageID: store.NewID("msg"),
	})
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrQueueBlocked) || errors.Is(err, store.ErrTurnRunning) {
		return
	}
	if err != nil {
		slog.Error("engine: drain queued input", "sessionID", sessionID, "err", err)
		return
	}
	if res == nil || res.Turn == nil || res.Input == nil || res.StartedEvent == nil {
		return
	}
	runtimeID := e.takeQueuedRuntime(sessionID, res.Input.ClientMessageID)
	turnCtx, cancel := context.WithCancel(app.WithRuntimeID(context.Background(), runtimeID))
	e.mu.Lock()
	e.running[sessionID] = cancel
	e.mu.Unlock()
	e.hub.Publish(*res.StartedEvent)

	var cfg provider.ModelConfig
	if len(res.Input.ModelConfig) > 0 {
		if err := json.Unmarshal(res.Input.ModelConfig, &cfg); err != nil {
			e.finishTurn(sessionID, res.Turn.ID, res.Turn.Mode, store.TurnFailed, fmt.Sprintf("model config: %v", err), nil)
			return
		}
	}
	client, err := e.resolver.Resolve(turnCtx, res.Input.Provider)
	if err != nil {
		e.finishTurn(sessionID, res.Turn.ID, res.Turn.Mode, store.TurnFailed, fmt.Sprintf("%v: %v", ErrProviderConfig, err), nil)
		return
	}
	resolved := &resolvedModel{
		providerName: res.Input.Provider,
		model:        res.Input.Model,
		config:       cfg,
		configJSON:   append(json.RawMessage(nil), res.Input.ModelConfig...),
		mode:         res.Input.Mode,
	}
	if resolved.mode == "" {
		resolved.mode = store.ModeChat
	} else {
		resolved.mode = store.NormalizeAgentMode(resolved.mode)
		if resolved.mode == "" {
			resolved.mode = store.ModeChat
		}
	}
	e.wg.Add(1)
	go e.runTurn(turnCtx, sessionID, res.Turn.ID, resolved, client)
}

func (e *Engine) streamTurn(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, client provider.Client, parts *turnPartAccumulator) (store.TurnStatus, string, store.AgentMode) {
	currentMode := resolved.mode
	currentMode = store.NormalizeAgentMode(currentMode)
	if currentMode == "" {
		currentMode = store.ModeChat
	}
	toolkits := newTurnToolkitState()
	baseReq, err := e.buildProviderRequest(ctx, sessionID, resolved, currentMode, toolkits)
	if err != nil {
		return store.TurnFailed, fmt.Sprintf("build context: %v", err), currentMode
	}

	maxLoops := defaultMaxToolLoops
	if v, ok := resolved.config.MaxToolLoops(); ok {
		maxLoops = v
	}
	consecutiveToolOnlyLoops := 0
	for {
		req := baseReq
		req.Messages = requestMessagesWithTurnParts(baseReq.Messages, parts.Parts(), sessionID, e.attachmentHome, resolved.config)
		ch, err := client.Stream(ctx, req)
		if err != nil {
			return store.TurnFailed, fmt.Sprintf("provider: %v", err), currentMode
		}
		finish, status, errMsg, assistantOutput := e.consumeStream(ctx, sessionID, turnID, resolved.model, ch, parts)
		if status != store.TurnRunning {
			return status, errMsg, currentMode
		}
		if finish == "" || finish == provider.FinishStop {
			if err := e.commitTurnParts(turnID, parts, true); err != nil {
				return store.TurnFailed, fmt.Sprintf("append output: %v", err), currentMode
			}
			return store.TurnCompleted, "", currentMode
		}
		if finish != provider.FinishToolCalls {
			return store.TurnFailed, fmt.Sprintf("unsupported finish reason: %s", finish), currentMode
		}
		if len(baseReq.Tools) == 0 {
			return store.TurnFailed, "provider requested tool calls but no tools are available", currentMode
		}
		if assistantOutput {
			consecutiveToolOnlyLoops = 0
		} else {
			if consecutiveToolOnlyLoops >= maxLoops {
				return store.TurnFailed, "max tool loops exceeded", currentMode
			}
			consecutiveToolOnlyLoops++
		}
		if err := e.commitTurnParts(turnID, parts, true); err != nil {
			return store.TurnFailed, fmt.Sprintf("append output: %v", err), currentMode
		}
		status, msg, nextMode, changed := e.executePendingTools(ctx, sessionID, turnID, currentMode, baseReq.Tools, toolkits, parts)
		if status != store.TurnRunning {
			return status, msg, currentMode
		}
		if changed {
			messages := baseReq.Messages
			currentMode = nextMode
			baseReq, err = e.buildProviderRequest(ctx, sessionID, resolved, currentMode, toolkits)
			if err != nil {
				return store.TurnFailed, fmt.Sprintf("build context: %v", err), currentMode
			}
			baseReq.Messages = messages
		}
	}
}

func (e *Engine) commitTurnParts(turnID string, parts *turnPartAccumulator, includeLast bool) error {
	upto := len(parts.parts)
	if !includeLast && upto > parts.committedParts {
		upto--
	}
	if upto <= parts.committedParts {
		return nil
	}
	commitParts := parts.PartsRange(parts.committedParts, upto)
	if _, err := e.store.AppendTurnOutput(context.Background(), store.AppendTurnOutputInput{TurnID: turnID, Parts: commitParts}); err != nil {
		return err
	}
	parts.committedParts = upto
	return nil
}

func (e *Engine) buildProviderRequest(ctx context.Context, sessionID string, resolved *resolvedModel, mode store.AgentMode, toolkits *turnToolkitState) (provider.Request, error) {
	req, err := e.builder.Build(ctx, sessionID, resolved.model, string(mode), resolved.config)
	if err != nil {
		return provider.Request{}, err
	}
	req.Config = resolved.config
	if modelSupportsTools(resolved.config) {
		var active map[string]bool
		if toolkits != nil {
			active = toolkits.active
		}
		defs, catalog, err := e.toolDefinitions(ctx, sessionID, mode, active)
		if err != nil {
			return provider.Request{}, err
		}
		req.Tools = defs
		req.System = appendToolkitIndex(req.System, tool.ToolkitIndex(mode, catalog))
		if toolkits != nil {
			toolkits.catalog = catalog
		}
	}
	return req, nil
}

func (e *Engine) toolDefinitions(ctx context.Context, sessionID string, mode store.AgentMode, active map[string]bool) ([]provider.ToolDef, []tool.ToolkitManifest, error) {
	if e.apps == nil {
		var defs []provider.ToolDef
		if e.tools != nil {
			runnerDefs, err := e.tools.Definitions(ctx, sessionID)
			if err != nil {
				return nil, nil, fmt.Errorf("list tools: %w", err)
			}
			defs = runnerDefs
		}
		catalog := tool.BuildToolkitCatalog(defs)
		return tool.DefinitionsFromCatalog(mode, defs, catalog, active), catalog, nil
	}
	appStates, err := e.sessionAppStates(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	loadedAppIDs := callableAppIDs(appStates, mode)
	var defs []provider.ToolDef
	if e.tools != nil {
		var runnerDefs []provider.ToolDef
		if scoped, ok := e.tools.(tool.AppScopedDefinitionRunner); ok {
			runnerDefs, err = scoped.DefinitionsForApps(ctx, sessionID, loadedAppIDs)
		} else {
			runnerDefs, err = e.tools.Definitions(ctx, sessionID)
		}
		if err != nil {
			return nil, nil, fmt.Errorf("list tools: %w", err)
		}
		defs = runnerDefs
	}
	toolkitDefs := make([]provider.ToolDef, 0, len(defs))
	appDefs := make([]provider.ToolDef, 0)
	for _, def := range defs {
		appID, appTool := tool.BuiltinAppIDForTool(def.Name)
		if appTool {
			if appDefinitionCallable(appStates[appID], mode) && tool.ToolDefAllowedForMode(mode, def) {
				appDefs = append(appDefs, def)
			}
			continue
		}
		if def.AppID != "" {
			if appDefinitionCallable(appStates[def.AppID], mode) && tool.ToolDefAllowedForMode(mode, def) {
				appDefs = append(appDefs, def)
			}
			continue
		}
		if tool.IsAppAPITool(def.Name) {
			if appAPIToolCallable(def.Name, appStates, mode) && tool.ToolDefAllowedForMode(mode, def) {
				appDefs = append(appDefs, def)
			}
			continue
		}
		toolkitDefs = append(toolkitDefs, def)
	}
	catalog := tool.BuildToolkitCatalog(toolkitDefs)
	out := tool.DefinitionsFromCatalog(mode, toolkitDefs, catalog, active)
	out = append(out, tool.AppLoadDefinition())
	sort.Slice(appDefs, func(i, j int) bool { return appDefs[i].Name < appDefs[j].Name })
	out = append(out, appDefs...)
	return out, catalog, nil
}

func callableAppIDs(states map[string]sessionAppState, mode store.AgentMode) []string {
	ids := make([]string, 0, len(states))
	for id, state := range states {
		if appDefinitionCallable(state, mode) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

func appDefinitionCallable(state sessionAppState, mode store.AgentMode) bool {
	return state.definition != nil && state.definition.Enabled && state.loaded && store.AgentModeRank(mode) >= store.AgentModeRank(requiredAppMode(state.definition))
}

func appAPIToolCallable(name string, states map[string]sessionAppState, mode store.AgentMode) bool {
	kind := app.EndpointKindREST
	if name != tool.RESTRequest {
		kind = app.EndpointKindGraphQL
	}
	for _, state := range states {
		if !appDefinitionCallable(state, mode) {
			continue
		}
		for _, endpoint := range state.definition.Endpoints {
			if endpoint.Kind == kind {
				return true
			}
		}
	}
	return false
}

type sessionAppState struct {
	definition *app.Definition
	loaded     bool
}

func (e *Engine) sessionAppStates(ctx context.Context, sessionID string) (map[string]sessionAppState, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	definitions, err := e.apps.ListDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list apps: %w", err)
	}
	loaded := make(map[string]bool, len(sess.LoadedAppIDs))
	for _, id := range sess.LoadedAppIDs {
		loaded[id] = true
	}
	out := make(map[string]sessionAppState, len(definitions))
	for _, definition := range definitions {
		if definition == nil || strings.TrimSpace(definition.ID) == "" {
			continue
		}
		out[definition.ID] = sessionAppState{definition: definition, loaded: loaded[definition.ID]}
	}
	return out, nil
}

func requiredAppMode(definition *app.Definition) store.AgentMode {
	if definition == nil {
		return store.ModeWork
	}
	mode := store.NormalizeAgentMode(store.AgentMode(definition.RequiredMode))
	if !store.ValidAgentMode(mode) {
		return store.ModeWork
	}
	return mode
}

func appendToolkitIndex(system, index string) string {
	index = strings.TrimSpace(index)
	if index == "" {
		return system
	}
	if strings.TrimSpace(system) == "" {
		return index
	}
	return strings.TrimRight(system, "\n") + "\n\n" + index
}

func modelSupportsTools(cfg provider.ModelConfig) bool {
	return cfg.Capabilities == nil || cfg.Capabilities.Tools
}

func (e *Engine) consumeStream(ctx context.Context, sessionID, turnID, model string, ch <-chan provider.Chunk, parts *turnPartAccumulator) (provider.FinishReason, store.TurnStatus, string, bool) {
	coalescer := newStreamEventCoalescer(e.hub)
	defer coalescer.Flush()
	var usage provider.UsageInfo
	usageSeen := false
	assistantOutput := false

	for {
		select {
		case <-ctx.Done():
			return "", store.TurnCancelled, "", assistantOutput
		case <-coalescer.C():
			coalescer.Flush()
		case chunk, ok := <-ch:
			if !ok {
				// provider 违反契约提前关 channel:cancel 中的截断仍按 cancelled 收尾,
				// 避免用户主动停止被记成 failed。
				if ctx.Err() != nil {
					return "", store.TurnCancelled, "", assistantOutput
				}
				if usageSeen {
					e.recordUsage(ctx, sessionID, model, usage, 1)
				}
				return "", store.TurnFailed, "provider stream ended without terminal chunk", assistantOutput
			}
			switch {
			case chunk.Err != nil:
				if errors.Is(chunk.Err, context.Canceled) {
					return "", store.TurnCancelled, "", assistantOutput
				}
				if usageSeen {
					e.recordUsage(ctx, sessionID, model, usage, 1)
				}
				return "", store.TurnFailed, chunk.Err.Error(), assistantOutput
			case chunk.Usage != nil:
				mergeUsageInfo(&usage, *chunk.Usage)
				usageSeen = true
			case chunk.Done:
				if usageSeen {
					e.recordUsage(ctx, sessionID, model, usage, 1)
				} else {
					e.recordUsage(ctx, sessionID, model, provider.UsageInfo{}, 1)
				}
				return chunk.Finish, store.TurnRunning, "", assistantOutput
			case chunk.Tool != nil:
				callID, name, argsDelta := parts.AppendTool(*chunk.Tool)
				if err := e.commitTurnParts(turnID, parts, false); err != nil {
					return "", store.TurnFailed, fmt.Sprintf("append output: %v", err), assistantOutput
				}
				coalescer.Push(event.Event{
					SessionID: sessionID,
					Kind:      event.TurnTool,
					TurnID:    turnID,
					CallID:    callID,
					Name:      name,
					Phase:     "streaming_args",
					ArgsDelta: argsDelta,
				})
			case chunk.Delta != "":
				part := chunk.Part
				if part == "" {
					part = provider.PartText
				}
				if part != provider.PartText && part != provider.PartThought {
					continue
				}
				assistantOutput = true
				parts.AppendDelta(part, chunk.Delta)
				if err := e.commitTurnParts(turnID, parts, false); err != nil {
					return "", store.TurnFailed, fmt.Sprintf("append output: %v", err), assistantOutput
				}
				coalescer.Push(event.Event{
					SessionID: sessionID,
					Kind:      event.TurnDelta,
					TurnID:    turnID,
					Part:      string(part),
					Delta:     chunk.Delta,
				})
			}
		}
	}
}

func mergeUsageInfo(dst *provider.UsageInfo, src provider.UsageInfo) {
	if src.InputUncachedTokens != 0 {
		dst.InputUncachedTokens = src.InputUncachedTokens
	}
	if src.InputCachedTokens != 0 {
		dst.InputCachedTokens = src.InputCachedTokens
	}
	if src.CacheCreationTokens != 0 {
		dst.CacheCreationTokens = src.CacheCreationTokens
	}
	if src.OutputContentTokens != 0 {
		dst.OutputContentTokens = src.OutputContentTokens
	}
	if src.OutputReasoningTokens != 0 {
		dst.OutputReasoningTokens = src.OutputReasoningTokens
	}
}

func (e *Engine) recordUsage(ctx context.Context, sessionID, model string, usage provider.UsageInfo, requestCount int) {
	if usage.Empty() && requestCount <= 0 {
		return
	}
	in := store.UsageRecordInput{
		OccurredAt:            time.Now(),
		Model:                 model,
		RequestCount:          requestCount,
		InputUncachedTokens:   usage.InputUncachedTokens,
		InputCachedTokens:     usage.InputCachedTokens,
		CacheCreationTokens:   usage.CacheCreationTokens,
		OutputContentTokens:   usage.OutputContentTokens,
		OutputReasoningTokens: usage.OutputReasoningTokens,
	}
	if _, err := e.store.RecordUsage(ctx, in); err != nil {
		slog.Warn("engine: record usage failed", "err", err)
	}
	if _, err := e.store.RecordSessionUsage(ctx, sessionID, in); err != nil {
		slog.Warn("engine: record session usage failed", "sessionID", sessionID, "err", err)
	}
}

type streamEventCoalescer struct {
	hub      *event.Hub
	pending  []event.Event
	timer    *time.Timer
	timerC   <-chan time.Time
	draining bool
}

func newStreamEventCoalescer(hub *event.Hub) *streamEventCoalescer {
	return &streamEventCoalescer{hub: hub}
}

func (c *streamEventCoalescer) C() <-chan time.Time {
	return c.timerC
}

func (c *streamEventCoalescer) Push(ev event.Event) {
	if len(c.pending) > 0 {
		last := c.pending[len(c.pending)-1]
		if merged, ok := mergeStreamEvents(last, ev); ok {
			c.pending[len(c.pending)-1] = merged
			c.schedule()
			return
		}
	}
	c.pending = append(c.pending, ev)
	c.schedule()
}

func (c *streamEventCoalescer) Flush() {
	c.stopTimer()
	if c.draining || len(c.pending) == 0 {
		return
	}
	c.draining = true
	events := c.pending
	c.pending = nil
	for _, ev := range events {
		c.hub.Publish(ev)
	}
	c.draining = false
}

func (c *streamEventCoalescer) schedule() {
	if c.timerC != nil {
		return
	}
	c.timer = time.NewTimer(streamEventCoalesceInterval)
	c.timerC = c.timer.C
}

func (c *streamEventCoalescer) stopTimer() {
	if c.timer == nil {
		return
	}
	if !c.timer.Stop() {
		select {
		case <-c.timer.C:
		default:
		}
	}
	c.timer = nil
	c.timerC = nil
}

func mergeStreamEvents(previous, next event.Event) (event.Event, bool) {
	if previous.Kind == event.TurnDelta &&
		next.Kind == event.TurnDelta &&
		previous.SessionID == next.SessionID &&
		previous.TurnID == next.TurnID &&
		previous.Part == next.Part {
		previous.Delta += next.Delta
		return previous, true
	}
	if previous.Kind == event.TurnTool &&
		next.Kind == event.TurnTool &&
		previous.SessionID == next.SessionID &&
		previous.TurnID == next.TurnID &&
		previous.CallID == next.CallID &&
		previous.Phase == "streaming_args" &&
		next.Phase == "streaming_args" {
		previous.ArgsDelta += next.ArgsDelta
		if previous.Name == "" {
			previous.Name = next.Name
		}
		return previous, true
	}
	return event.Event{}, false
}

func (e *Engine) executePendingTools(ctx context.Context, sessionID, turnID string, currentMode store.AgentMode, allowedTools []provider.ToolDef, toolkits *turnToolkitState, parts *turnPartAccumulator) (store.TurnStatus, string, store.AgentMode, bool) {
	calls := parts.PendingToolCalls()
	if len(calls) == 0 {
		return store.TurnFailed, "provider finished with tool_calls but emitted no complete tool call", currentMode, false
	}
	nextMode := currentMode
	modeChanged := false
	toolsChanged := false
	for _, call := range calls {
		call.SessionID = sessionID
		call.TurnID = turnID
		e.hub.Publish(event.Event{
			SessionID: sessionID,
			Kind:      event.TurnTool,
			TurnID:    turnID,
			CallID:    call.CallID,
			Name:      call.Name,
			Phase:     "running",
		})
		var result tool.Result
		if call.Name == tool.RequestCapability {
			var requestedMode store.AgentMode
			var changed bool
			result, requestedMode, changed = e.requestCapabilityApproval(ctx, sessionID, turnID, call, nextMode)
			if changed {
				nextMode = requestedMode
				modeChanged = true
			}
		} else if call.Name == tool.ToolkitLoad {
			var changed bool
			result, changed = loadTurnToolkits(call, nextMode, toolkits)
			toolsChanged = toolsChanged || changed
		} else if call.Name == tool.AppLoad {
			var changed bool
			result, changed = e.loadApp(ctx, sessionID, call, nextMode)
			toolsChanged = toolsChanged || changed
		} else if appID, appTool := tool.BuiltinAppIDForTool(call.Name); appTool && e.apps != nil {
			if !tool.NameAllowedForMode(nextMode, call.Name) {
				result = toolNotAllowedResult(call, nextMode)
			} else if !tool.HasDefinition(allowedTools, call.Name) || !e.appToolCallable(ctx, sessionID, appID, nextMode) {
				result = e.appToolUnavailableResult(ctx, sessionID, call, appID)
			} else {
				result = e.executeAllowedTool(ctx, sessionID, turnID, call)
			}
		} else if definition, ok := providerToolDefinition(allowedTools, call.Name); ok && definition.AppID != "" && e.apps != nil {
			if !e.appToolCallable(ctx, sessionID, definition.AppID, nextMode) {
				result = e.appToolUnavailableResult(ctx, sessionID, call, definition.AppID)
			} else {
				result = e.executeAllowedTool(ctx, sessionID, turnID, call)
			}
		} else if tool.IsAppAPITool(call.Name) && e.apps != nil {
			appID, resolved := e.appEndpointTarget(ctx, sessionID, call)
			switch {
			case resolved && !e.appToolCallable(ctx, sessionID, appID, nextMode):
				result = e.appToolUnavailableResult(ctx, sessionID, call, appID)
			case !tool.HasDefinition(allowedTools, call.Name):
				result = appAPINotLoadedResult(call)
			default:
				result = e.executeAllowedTool(ctx, sessionID, turnID, call)
			}
		} else if !tool.HasDefinition(allowedTools, call.Name) {
			known, err := e.toolNameKnown(ctx, sessionID, call.Name)
			if err != nil {
				result = tool.Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: fmt.Sprintf("list tools: %v", err)}
			} else if known {
				if tool.NameAllowedForMode(nextMode, call.Name) {
					if appID, appTool := tool.BuiltinAppIDForTool(call.Name); appTool && e.apps != nil {
						result = e.appToolUnavailableResult(ctx, sessionID, call, appID)
					} else {
						result = toolNotLoadedResult(call, toolkits.catalog)
					}
				} else {
					result = toolNotAllowedResult(call, nextMode)
				}
			} else {
				result = unknownToolResult(call)
			}
		} else {
			result = e.executeAllowedTool(ctx, sessionID, turnID, call)
		}
		if ctx.Err() != nil {
			return store.TurnCancelled, "", nextMode, modeChanged
		}
		if result.CallID == "" {
			result.CallID = call.CallID
		}
		if result.Name == "" {
			result.Name = call.Name
		}
		parts.AppendToolResult(result)
		parts.AppendAttachments(result.ContextAttachments)
		if err := e.commitTurnParts(turnID, parts, true); err != nil {
			return store.TurnFailed, fmt.Sprintf("append output: %v", err), nextMode, modeChanged
		}
		phase := "ok"
		if !result.Ok {
			phase = "error"
		}
		e.hub.Publish(event.Event{
			SessionID:    sessionID,
			Kind:         event.TurnTool,
			TurnID:       turnID,
			CallID:       result.CallID,
			Name:         result.Name,
			Phase:        phase,
			Ok:           &result.Ok,
			Content:      result.Content,
			SummaryKind:  result.SummaryKind,
			SummaryCount: result.SummaryCount,
			Attachments:  eventAttachmentsFromStore(result.ContextAttachments),
		})
		if ctx.Err() != nil {
			return store.TurnCancelled, "", nextMode, modeChanged
		}
	}
	return store.TurnRunning, "", nextMode, modeChanged || toolsChanged
}

func (e *Engine) executeAllowedTool(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	if call.Name == tool.SkillSubmit {
		return e.requestSkillDraftApproval(ctx, sessionID, turnID, call)
	}
	if e.tools == nil {
		return tool.Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: "tool runner unavailable"}
	}
	call.ProjectDirs = e.projectRootDirsForToolCall(ctx, sessionID, turnID)
	call.CommandSandbox = commandSandboxModeForProject(nil)
	var result tool.Result
	if risk, ok := tool.ClassifyToolCallForProject(call.Name, call.Args, call.ProjectDirs); ok {
		var approvalDetails map[string]any
		var approvalDetailsErr error
		if tool.RequiresApprovalDetails(call.Name) {
			if source, ok := e.tools.(tool.ApprovalDetailsProvider); ok {
				approvalDetails, approvalDetailsErr = source.ApprovalDetails(ctx, call)
			} else {
				approvalDetailsErr = errors.New("tool approval details unavailable")
			}
		}
		risk = refineToolRisk(call.Name, risk, approvalDetails)
		project, required, err := e.toolCallApprovalRequired(ctx, sessionID, risk)
		call.CommandSandbox = commandSandboxModeForProject(project)
		if approvalDetailsErr != nil {
			result = tool.ApprovalDetailsFailure(call, approvalDetailsErr)
		} else if err != nil {
			result = tool.Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: fmt.Sprintf("approval policy: %v", err)}
		} else if required {
			var approved bool
			result, approved = e.requestToolCallApproval(ctx, sessionID, turnID, call, risk, project, approvalDetails)
			if approved {
				result = e.callTool(ctx, sessionID, turnID, call)
			}
		}
	}
	if result.CallID == "" && result.Name == "" {
		result = e.callTool(ctx, sessionID, turnID, call)
	}
	return result
}

func (e *Engine) loadApp(ctx context.Context, sessionID string, call tool.Call, mode store.AgentMode) (tool.Result, bool) {
	if e.apps == nil {
		return appLoadFailure(call, "app_service_unavailable", "App loading is unavailable", nil), false
	}
	request, err := tool.DecodeAppLoadRequest(call.Args)
	if err != nil {
		return appLoadFailure(call, "invalid_arguments", err.Error(), nil), false
	}
	definitions, err := e.apps.ListDefinitions(ctx)
	if err != nil {
		return appLoadFailure(call, "app_state_unavailable", err.Error(), nil), false
	}
	var definition *app.Definition
	for _, candidate := range definitions {
		if candidate != nil && candidate.ID == request.AppID {
			definition = candidate
			break
		}
	}
	if definition == nil {
		return appLoadFailure(call, "app_unavailable", "the App is not available in the current runtime", map[string]any{"appID": request.AppID}), false
	}
	if !definition.Enabled {
		return appLoadFailure(call, "app_disabled", "the App is disabled", map[string]any{"appID": request.AppID}), false
	}
	requiredMode := requiredAppMode(definition)
	if store.AgentModeRank(mode) < store.AgentModeRank(requiredMode) {
		return appLoadFailure(call, "capability_required", "the App requires a higher capability", map[string]any{
			"appID":        request.AppID,
			"currentMode":  store.NormalizeAgentMode(mode),
			"requiredMode": requiredMode,
		}), false
	}
	skillID := request.SkillID
	if skillID == "" {
		skillID = defaultAppSkillID(definition)
	}
	if skillID == "" {
		return appLoadFailure(call, "app_skill_unavailable", "the App has no default skill", map[string]any{"appID": request.AppID}), false
	}
	detail, err := e.apps.ReadSkill(ctx, request.AppID, skillID)
	if err != nil {
		reason := "app_skill_read_failed"
		switch {
		case errors.Is(err, app.ErrInvalidID):
			reason = "invalid_app_id"
		case errors.Is(err, app.ErrNotFound):
			reason = "app_skill_not_found"
		case errors.Is(err, app.ErrDisabled):
			reason = "app_disabled"
		}
		return appLoadFailure(call, reason, err.Error(), map[string]any{"appID": request.AppID, "skillID": skillID}), false
	}
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return appLoadFailure(call, "app_state_unavailable", err.Error(), map[string]any{"appID": request.AppID}), false
	}
	alreadyLoaded := false
	for _, loadedID := range sess.LoadedAppIDs {
		if loadedID == request.AppID {
			alreadyLoaded = true
			break
		}
	}
	if !alreadyLoaded {
		loaded := store.NormalizeAppIDs(append(append([]string(nil), sess.LoadedAppIDs...), request.AppID))
		if _, err := e.store.UpdateSession(ctx, sessionID, store.SessionUpdate{LoadedAppIDs: &loaded}); err != nil {
			return appLoadFailure(call, "app_state_unavailable", err.Error(), map[string]any{"appID": request.AppID}), false
		}
	}
	resolvedSkillID := strings.TrimSpace(detail.ID)
	if resolvedSkillID == "" {
		resolvedSkillID = skillID
	}
	payload := map[string]any{
		"ok":            true,
		"appID":         request.AppID,
		"skillID":       resolvedSkillID,
		"name":          detail.Name,
		"description":   detail.Description,
		"path":          detail.Path,
		"content":       detail.Content,
		"newlyLoaded":   !alreadyLoaded,
		"alreadyLoaded": alreadyLoaded,
		"message":       "the App is loaded; its tools are available on the next model step",
	}
	content, _ := json.Marshal(payload)
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           true,
		Content:      string(content),
		SummaryKind:  tool.SummaryReadChars,
		SummaryCount: len(detail.Content),
	}, !alreadyLoaded
}

func defaultAppSkillID(definition *app.Definition) string {
	if definition == nil {
		return ""
	}
	if skillID := strings.TrimSpace(definition.DefaultSkillID); skillID != "" {
		return skillID
	}
	for _, skill := range definition.Skills {
		if id := strings.TrimSpace(skill.ID); id != "" {
			return id
		}
		if name := strings.TrimSpace(skill.Name); name != "" {
			return name
		}
		if path := strings.TrimSpace(skill.Path); path != "" {
			return path
		}
	}
	return ""
}

func appLoadFailure(call tool.Call, reason, message string, extra map[string]any) tool.Result {
	payload := map[string]any{"ok": false, "reason": reason, "message": message}
	for key, value := range extra {
		payload[key] = value
	}
	content, _ := json.Marshal(payload)
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(content),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

type turnToolkitState struct {
	active  map[string]bool
	loads   int
	catalog []tool.ToolkitManifest
}

func newTurnToolkitState() *turnToolkitState {
	return &turnToolkitState{active: make(map[string]bool)}
}

func loadTurnToolkits(call tool.Call, mode store.AgentMode, state *turnToolkitState) (tool.Result, bool) {
	if state == nil {
		return toolkitLoadFailure(call, "toolkit_state_unavailable", "turn toolkit state is unavailable", nil), false
	}
	if state.loads >= maxToolkitLoads {
		return toolkitLoadFailure(call, "toolkit_load_limit", "toolkits can be expanded at most twice per turn", map[string]any{"maxLoads": maxToolkitLoads}), false
	}
	request, err := tool.DecodeToolkitLoadRequest(call.Args)
	if err != nil {
		return toolkitLoadFailure(call, "invalid_arguments", err.Error(), nil), false
	}
	manifests := make([]tool.ToolkitManifest, 0, len(request.ToolkitIDs))
	for _, id := range request.ToolkitIDs {
		manifest, ok := tool.ToolkitByID(state.catalog, id)
		if !ok {
			return toolkitLoadFailure(call, "unknown_toolkit", "toolkit is not listed in Available Toolkits", map[string]any{"toolkitID": id}), false
		}
		if store.AgentModeRank(mode) < store.AgentModeRank(manifest.Capability) {
			return toolkitLoadFailure(call, "capability_required", "toolkit requires a higher capability", map[string]any{
				"toolkitID":    id,
				"currentMode":  store.NormalizeAgentMode(mode),
				"requiredMode": manifest.Capability,
			}), false
		}
		manifests = append(manifests, manifest)
	}
	state.loads++
	loaded := make([]string, 0, len(manifests))
	alreadyActive := make([]string, 0, len(manifests))
	tools := make([]string, 0)
	for _, manifest := range manifests {
		if manifest.Default || state.active[manifest.ID] {
			alreadyActive = append(alreadyActive, manifest.ID)
			continue
		}
		state.active[manifest.ID] = true
		loaded = append(loaded, manifest.ID)
		tools = append(tools, manifest.ToolNames...)
	}
	sort.Strings(loaded)
	sort.Strings(alreadyActive)
	sort.Strings(tools)
	payload := map[string]any{
		"ok":             true,
		"loaded":         loaded,
		"alreadyActive":  alreadyActive,
		"activeToolkits": tool.ActiveToolkitIDs(state.active),
		"tools":          tools,
		"loadsRemaining": maxToolkitLoads - state.loads,
	}
	content, _ := json.Marshal(payload)
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           true,
		Content:      string(content),
		SummaryKind:  tool.SummaryReturnedItems,
		SummaryCount: len(tools),
	}, len(loaded) > 0
}

func toolkitLoadFailure(call tool.Call, reason, message string, extra map[string]any) tool.Result {
	payload := map[string]any{"ok": false, "reason": reason, "message": message}
	for key, value := range extra {
		payload[key] = value
	}
	content, _ := json.Marshal(payload)
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(content),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

func (e *Engine) callTool(ctx context.Context, sessionID, turnID string, call tool.Call) tool.Result {
	toolCtx, cancel, timed := toolContext(ctx, call.Name)
	toolCtx = tool.WithProgressSink(toolCtx, func(progress tool.Progress) {
		e.hub.Publish(event.Event{
			SessionID: sessionID,
			Kind:      event.TurnTool,
			TurnID:    turnID,
			CallID:    call.CallID,
			Name:      call.Name,
			Phase:     "output",
			Stream:    progress.Stream,
			Content:   progress.Content,
		})
	})
	result := e.tools.Call(toolCtx, call)
	cancel()
	if timed && toolCtx.Err() != nil && result.Content == "" {
		return tool.Result{
			CallID:  call.CallID,
			Name:    call.Name,
			Ok:      false,
			Content: "tool timed out",
		}
	}
	return result
}

func toolContext(ctx context.Context, name string) (context.Context, context.CancelFunc, bool) {
	if strings.HasPrefix(name, "ui_") || name == tool.CommandRun {
		return ctx, func() {}, false
	}
	toolCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
	return toolCtx, cancel, true
}

func eventAttachmentsFromStore(attachments []store.Attachment) []event.Attachment {
	attachments = store.NormalizeAttachments(attachments)
	if len(attachments) == 0 {
		return nil
	}
	out := make([]event.Attachment, 0, len(attachments))
	for _, item := range attachments {
		out = append(out, event.Attachment{
			ID:              item.ID,
			Name:            item.Name,
			AttachmentKey:   item.AttachmentKey,
			URL:             item.URL,
			MIME:            item.MIME,
			Size:            item.Size,
			Origin:          item.Origin,
			SourcePath:      item.SourcePath,
			CreatedAt:       item.CreatedAt,
			AudioTranscript: item.AudioTranscript,
		})
	}
	return out
}

func (e *Engine) projectRootDirsForToolCall(ctx context.Context, sessionID, turnID string) []string {
	var dirs []string
	if sess, err := e.store.GetSession(ctx, sessionID); err == nil {
		if sess.ProjectID != "" {
			if project, err := e.store.GetProject(ctx, sess.ProjectID); err == nil {
				dirs = append(dirs, project.RootDirs...)
			}
		}
	}
	e.mu.Lock()
	grant := e.turnProjectAccess[turnID]
	e.mu.Unlock()
	dirs = append(dirs, grant.RootDirs...)
	return store.NormalizeProjectDirs(dirs)
}

func (e *Engine) toolNameKnown(ctx context.Context, sessionID, name string) (bool, error) {
	if name == tool.RequestCapability || name == tool.ToolkitLoad || (name == tool.AppLoad && e.apps != nil) {
		return true, nil
	}
	if e.tools == nil {
		return false, nil
	}
	defs, err := e.tools.Definitions(ctx, sessionID)
	if err != nil {
		return false, err
	}
	return tool.HasDefinition(defs, name), nil
}

func (e *Engine) appToolUnavailableResult(ctx context.Context, sessionID string, call tool.Call, appID string) tool.Result {
	payload := map[string]any{
		"ok":      false,
		"appID":   appID,
		"tool":    call.Name,
		"reason":  "app_not_loaded",
		"message": "load the App with builtin_app_load before using its tools",
	}
	states, err := e.sessionAppStates(ctx, sessionID)
	if err != nil {
		payload["reason"] = "app_state_unavailable"
		payload["message"] = err.Error()
	} else if state, ok := states[appID]; !ok || !state.definition.Enabled {
		payload["reason"] = "app_disabled"
		payload["message"] = "the app is disabled or unavailable"
	} else {
		if state.definition.DefaultSkillID != "" {
			payload["defaultSkillID"] = state.definition.DefaultSkillID
		}
		if state.loaded {
			payload["reason"] = "app_tool_unavailable"
			payload["message"] = "the app is loaded but this tool is not available in the current request"
		}
	}
	content, _ := json.Marshal(payload)
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(content),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

func (e *Engine) appToolCallable(ctx context.Context, sessionID, appID string, mode store.AgentMode) bool {
	states, err := e.sessionAppStates(ctx, sessionID)
	if err != nil {
		return false
	}
	state, ok := states[appID]
	return ok && state.definition.Enabled && state.loaded && store.AgentModeRank(mode) >= store.AgentModeRank(requiredAppMode(state.definition))
}

func (e *Engine) appEndpointTarget(ctx context.Context, sessionID string, call tool.Call) (string, bool) {
	resolver, ok := e.apps.(appEndpointResolver)
	if !ok {
		return "", false
	}
	var args struct {
		Endpoint   string `json:"endpoint"`
		Connection string `json:"connection"`
	}
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil || strings.TrimSpace(args.Endpoint) == "" {
		return "", false
	}
	binding, err := resolver.ResolveEndpoint(ctx, sessionID, args.Endpoint, args.Connection)
	if err == nil && binding != nil && strings.TrimSpace(binding.AppID) != "" {
		return binding.AppID, true
	}
	states, stateErr := e.sessionAppStates(ctx, sessionID)
	if stateErr != nil {
		return "", false
	}
	matched := ""
	for appID, state := range states {
		if _, ok := state.definition.Endpoints[strings.TrimSpace(args.Endpoint)]; !ok {
			continue
		}
		if matched != "" && matched != appID {
			return "", false
		}
		matched = appID
	}
	return matched, matched != ""
}

func providerToolDefinition(defs []provider.ToolDef, name string) (provider.ToolDef, bool) {
	for _, definition := range defs {
		if definition.Name == name {
			return definition, true
		}
	}
	return provider.ToolDef{}, false
}

func appAPINotLoadedResult(call tool.Call) tool.Result {
	payload := map[string]any{
		"ok":      false,
		"reason":  "app_not_loaded",
		"tool":    call.Name,
		"message": "load the target App with builtin_app_load before using its API tools",
	}
	content, _ := json.Marshal(payload)
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(content),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

func toolNotLoadedResult(call tool.Call, catalog []tool.ToolkitManifest) tool.Result {
	payload := map[string]any{
		"ok":      false,
		"reason":  "tool_not_loaded",
		"tool":    call.Name,
		"message": "tool is known but not loaded for this turn; call builtin_toolkit_load first",
	}
	if manifest, ok := tool.ToolkitForTool(catalog, call.Name); ok {
		payload["toolkitID"] = manifest.ID
	}
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{"ok":false,"reason":"tool_not_loaded"}`)
	}
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(b),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

func unknownToolResult(call tool.Call) tool.Result {
	payload := map[string]any{
		"ok":      false,
		"reason":  "unknown_tool",
		"tool":    call.Name,
		"message": "tool is not defined; use one of the advertised tool names exactly",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{"ok":false,"reason":"unknown_tool"}`)
	}
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(b),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

func toolNotAllowedResult(call tool.Call, mode store.AgentMode) tool.Result {
	payload := map[string]any{
		"ok":          false,
		"reason":      "capability_required",
		"currentMode": store.NormalizeAgentMode(mode),
		"tool":        call.Name,
		"message":     "tool is not available in the current capability; call request_capability first",
	}
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(`{"ok":false,"reason":"capability_required"}`)
	}
	return tool.Result{
		CallID:       call.CallID,
		Name:         call.Name,
		Ok:           false,
		Content:      string(b),
		SummaryKind:  tool.SummaryReturnedFields,
		SummaryCount: len(payload),
	}
}

type turnPartAccumulator struct {
	parts          []store.ContentPart
	committedParts int
	toolPartByKey  map[string]int
	toolKeyByIndex map[int]string
	toolArgs       map[string]*strings.Builder
}

func (a *turnPartAccumulator) AppendDelta(part provider.PartType, delta string) {
	if delta == "" {
		return
	}
	partType := store.ContentPartText
	if part == provider.PartThought {
		partType = store.ContentPartThought
	}
	last := len(a.parts) - 1
	if last >= 0 && a.parts[last].Type == partType {
		a.parts[last].Text += delta
		return
	}
	a.parts = append(a.parts, store.ContentPart{Type: partType, Text: delta})
}

func (a *turnPartAccumulator) AppendTool(chunk provider.ToolCallChunk) (string, string, string) {
	if a.toolPartByKey == nil {
		a.toolPartByKey = make(map[string]int)
	}
	if a.toolKeyByIndex == nil {
		a.toolKeyByIndex = make(map[int]string)
	}
	if a.toolArgs == nil {
		a.toolArgs = make(map[string]*strings.Builder)
	}
	key := a.toolKey(chunk)
	if chunk.CallID != "" {
		a.toolKeyByIndex[chunk.Index] = key
	}
	partIndex, ok := a.toolPartByKey[key]
	if !ok {
		callID := chunk.CallID
		if callID == "" {
			callID = fmt.Sprintf("tool_%d", chunk.Index)
		}
		a.parts = append(a.parts, store.ContentPart{
			Type:   store.ContentPartToolUse,
			CallID: callID,
			Name:   chunk.Name,
		})
		partIndex = len(a.parts) - 1
		a.toolPartByKey[key] = partIndex
	}
	part := &a.parts[partIndex]
	if chunk.CallID != "" {
		part.CallID = chunk.CallID
	}
	if part.CallID == "" {
		part.CallID = fmt.Sprintf("tool_%d", chunk.Index)
	}
	if chunk.Name != "" {
		part.Name = chunk.Name
	}
	if chunk.ArgsDelta != "" {
		builder := a.toolArgs[key]
		if builder == nil {
			builder = &strings.Builder{}
			a.toolArgs[key] = builder
		}
		builder.WriteString(chunk.ArgsDelta)
	}
	return part.CallID, part.Name, chunk.ArgsDelta
}

func (a *turnPartAccumulator) toolKey(chunk provider.ToolCallChunk) string {
	if chunk.CallID != "" {
		return "id:" + chunk.CallID
	}
	if key := a.toolKeyByIndex[chunk.Index]; key != "" {
		return key
	}
	return fmt.Sprintf("idx:%d", chunk.Index)
}

func (a *turnPartAccumulator) AppendToolResult(result tool.Result) {
	if result.CallID == "" && result.Content == "" {
		return
	}
	a.parts = append(a.parts, store.ContentPart{
		Type:         store.ContentPartToolResult,
		CallID:       result.CallID,
		Name:         result.Name,
		Ok:           result.Ok,
		Content:      result.Content,
		SummaryKind:  result.SummaryKind,
		SummaryCount: result.SummaryCount,
	})
}

func (a *turnPartAccumulator) AppendAttachments(attachments []store.Attachment) {
	for _, item := range store.NormalizeAttachments(attachments) {
		a.parts = append(a.parts, store.ContentPart{
			Type:                store.ContentPartAttachment,
			CallID:              item.ID,
			Name:                item.Name,
			AttachmentKey:       item.AttachmentKey,
			URL:                 item.URL,
			MIME:                item.MIME,
			Size:                item.Size,
			Origin:              item.Origin,
			SourcePath:          item.SourcePath,
			AttachmentCreatedAt: item.CreatedAt,
			AudioTranscript:     item.AudioTranscript,
		})
	}
}

func (a *turnPartAccumulator) PendingToolCalls() []tool.Call {
	results := make(map[string]bool)
	for _, part := range a.parts {
		if part.Type == store.ContentPartToolResult && part.CallID != "" {
			results[part.CallID] = true
		}
	}
	var calls []tool.Call
	for partIndex, part := range a.parts {
		if part.Type != store.ContentPartToolUse {
			continue
		}
		if part.CallID != "" && results[part.CallID] {
			continue
		}
		args := a.rawToolArgs(partIndex)
		calls = append(calls, tool.Call{
			CallID: part.CallID,
			Name:   part.Name,
			Args:   append(json.RawMessage(nil), args...),
		})
	}
	return calls
}

func (a *turnPartAccumulator) rawToolArgs(partIndex int) json.RawMessage {
	for key, idx := range a.toolPartByKey {
		if idx != partIndex {
			continue
		}
		if builder := a.toolArgs[key]; builder != nil {
			return json.RawMessage(builder.String())
		}
	}
	return nil
}

func (a *turnPartAccumulator) Parts() []store.ContentPart {
	return a.PartsRange(0, len(a.parts))
}

func (a *turnPartAccumulator) UncommittedParts() []store.ContentPart {
	return a.PartsRange(a.committedParts, len(a.parts))
}

func (a *turnPartAccumulator) PartsRange(start, end int) []store.ContentPart {
	if start < 0 {
		start = 0
	}
	if end > len(a.parts) {
		end = len(a.parts)
	}
	if start >= end {
		return nil
	}
	partOffset := start
	source := a.parts[start:end]
	out := store.CloneContentParts(source)
	for key, partIndex := range a.toolPartByKey {
		if partIndex < start || partIndex >= end {
			continue
		}
		raw := ""
		if builder := a.toolArgs[key]; builder != nil {
			raw = builder.String()
		}
		if raw == "" {
			continue
		}
		args := json.RawMessage(raw)
		if json.Valid(args) {
			out[partIndex-partOffset].Args = append(json.RawMessage(nil), args...)
		}
	}
	return store.NormalizeContentParts(out)
}

func requestMessagesWithTurnParts(base []provider.Message, parts []store.ContentPart, sessionID, attachmentHome string, cfg provider.ModelConfig) []provider.Message {
	out := cloneProviderMessages(base)
	assistantStoreParts := nonAttachmentParts(parts)
	assistantProviderParts := providerPartsFromStore(assistantStoreParts)
	if len(assistantProviderParts) > 0 {
		out = append(out, provider.Message{
			Role:  provider.RoleAssistant,
			Text:  store.TextFromParts(assistantStoreParts),
			Parts: assistantProviderParts,
		})
	}
	attachmentProviderParts := providerAttachmentPartsFromStore(sessionID, parts, attachmentHome, cfg)
	if len(attachmentProviderParts) > 0 {
		out = append(out, provider.Message{
			Role:  provider.RoleUser,
			Text:  textFromProviderParts(attachmentProviderParts),
			Parts: attachmentProviderParts,
		})
	}
	return out
}

func nonAttachmentParts(parts []store.ContentPart) []store.ContentPart {
	out := make([]store.ContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Type == store.ContentPartAttachment {
			continue
		}
		out = append(out, part)
	}
	return out
}

func providerPartsFromStore(parts []store.ContentPart) []provider.Part {
	out := make([]provider.Part, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case store.ContentPartText:
			if part.Text != "" {
				out = append(out, provider.Part{Type: provider.PartText, Text: part.Text})
			}
		case store.ContentPartThought:
			continue
		case store.ContentPartToolUse:
			out = append(out, provider.Part{
				Type:   provider.PartToolUse,
				CallID: part.CallID,
				Name:   part.Name,
				Args:   append(json.RawMessage(nil), part.Args...),
			})
		case store.ContentPartToolResult:
			out = append(out, provider.Part{
				Type:    provider.PartToolResult,
				CallID:  part.CallID,
				Name:    part.Name,
				Ok:      part.Ok,
				Content: part.Content,
			})
		}
	}
	return out
}

func providerAttachmentPartsFromStore(sessionID string, parts []store.ContentPart, attachmentHome string, cfg provider.ModelConfig) []provider.Part {
	out := make([]provider.Part, 0, len(parts))
	for _, part := range parts {
		if part.Type != store.ContentPartAttachment {
			continue
		}
		if part.Origin == attachment.OriginASRAudio {
			continue
		}
		if imagePart, ok := providerImagePartFromAttachment(sessionID, attachmentHome, part, cfg); ok {
			if text := providerAttachmentFallbackText(part, "image"); text != "" {
				out = append(out, provider.Part{Type: provider.PartText, Text: text})
			}
			out = append(out, imagePart)
			continue
		}
		if text := providerAttachmentFallbackText(part, ""); text != "" {
			out = append(out, provider.Part{Type: provider.PartText, Text: text})
		}
	}
	return out
}

func providerImagePartFromAttachment(sessionID, attachmentHome string, part store.ContentPart, cfg provider.ModelConfig) (provider.Part, bool) {
	if cfg.Capabilities == nil || !cfg.Capabilities.Image {
		return provider.Part{}, false
	}
	if strings.TrimSpace(attachmentHome) == "" {
		return provider.Part{}, false
	}
	mime := strings.ToLower(strings.TrimSpace(part.MIME))
	if !strings.HasPrefix(mime, "image/") || mime == "image/svg+xml" {
		return provider.Part{}, false
	}
	modelImage, err := attachment.NewService(attachmentHome).ModelImageForProvider(attachmentSessionIDForPart(sessionID, part), part.AttachmentKey, mime)
	if err != nil || len(modelImage.Data) == 0 {
		return provider.Part{}, false
	}
	return provider.Part{
		Type:   provider.PartImage,
		MIME:   modelImage.MIME,
		Data:   modelImage.Data,
		Width:  modelImage.Width,
		Height: modelImage.Height,
	}, true
}

func attachmentSessionIDForPart(sessionID string, part store.ContentPart) string {
	if part.Origin == attachment.OriginTemp && strings.Contains(part.AttachmentKey, "/"+attachment.DraftSessionID+"/") {
		return attachment.DraftSessionID
	}
	return sessionID
}

func providerAttachmentFallbackText(part store.ContentPart, mediaKind string) string {
	var b strings.Builder
	b.WriteString("[Attachment]\n")
	if part.Name != "" {
		b.WriteString("Name: ")
		b.WriteString(part.Name)
		b.WriteByte('\n')
	}
	if part.MIME != "" {
		b.WriteString("MIME: ")
		b.WriteString(part.MIME)
		b.WriteByte('\n')
	}
	if part.Size > 0 {
		b.WriteString("Size bytes: ")
		b.WriteString(fmt.Sprintf("%d", part.Size))
		b.WriteByte('\n')
	}
	if part.AttachmentKey != "" {
		b.WriteString("attachmentKey: ")
		b.WriteString(part.AttachmentKey)
		b.WriteByte('\n')
	}
	if part.URL != "" {
		b.WriteString("displayURL (UI only): ")
		b.WriteString(part.URL)
		b.WriteByte('\n')
	}
	if part.SourcePath != "" {
		b.WriteString("Source path: ")
		b.WriteString(part.SourcePath)
		b.WriteByte('\n')
	}
	if mediaKind == "image" {
		b.WriteString("Image content: provided as an image part.\n")
	} else if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part.MIME)), "image/") {
		b.WriteString("Image content: not provided because the current model does not support image inputs. Do not describe visual details from metadata alone.\n")
	}
	return strings.TrimSpace(b.String())
}

func textFromProviderParts(parts []provider.Part) string {
	var b strings.Builder
	for _, part := range parts {
		switch part.Type {
		case "", provider.PartText, provider.PartThought:
			b.WriteString(part.Text)
		case provider.PartToolResult:
			if b.Len() > 0 && part.Content != "" {
				b.WriteByte('\n')
			}
			b.WriteString(part.Content)
		}
	}
	return b.String()
}

func cloneProviderMessages(in []provider.Message) []provider.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]provider.Message, 0, len(in))
	for _, msg := range in {
		cp := msg
		if len(msg.Parts) > 0 {
			cp.Parts = make([]provider.Part, 0, len(msg.Parts))
			for _, part := range msg.Parts {
				pp := part
				if part.Args != nil {
					pp.Args = append(json.RawMessage(nil), part.Args...)
				}
				cp.Parts = append(cp.Parts, pp)
			}
		}
		out = append(out, cp)
	}
	return out
}

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
	"strings"
	"sync"
	"time"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const defaultMaxToolLoops = 16
const toolCallTimeout = 60 * time.Second
const streamEventCoalesceInterval = 30 * time.Millisecond

var (
	// ErrTurnRunning:第一阶段同一 session 不允许并发 turn,API 映射 409。
	ErrTurnRunning = store.ErrTurnRunning
	// ErrNoRunningTurn:cancel 时没有进行中的 turn,API 映射 409。
	ErrNoRunningTurn = errors.New("engine: no running turn")
	ErrEmptyInput    = errors.New("engine: empty text or clientMessageID")
	// ErrNoModel:会话未解析出可用 provider/model(空配置),提交时直接报错,
	// 不做任何隐式 provider/model 兜底。API 映射 400 "no_model"。
	ErrNoModel        = errors.New("engine: no model configured for session")
	ErrProviderConfig = errors.New("engine: provider config unavailable")
)

// Resolver 把 provider profile 名解析为 client 实例;
// 生产实现是 provider/registry,--mock 与测试用 registry.Static。
type Resolver interface {
	Resolve(ctx context.Context, name string) (provider.Client, error)
}

type ConfigSource interface {
	Settings(ctx context.Context) (map[string]string, error)
	GetProviderProfile(ctx context.Context, name string) (*store.ProviderProfile, error)
}

type emptyConfig struct{}

func (emptyConfig) Settings(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
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

	// auxCtx 是辅助 goroutine(自动标题,将来工具相关后台任务)的基 ctx;
	// Stop() 取消它,优雅退出时这些 best-effort 任务立即中断,不拖住
	// Wait()。turn goroutine 不挂在这上面——turn 要写完 canonical 才退,
	// 由各自 turnCtx + provider 超时兜底。
	auxCtx    context.Context
	auxCancel context.CancelFunc

	mu      sync.Mutex
	running map[string]context.CancelFunc // sessionID → 当前 turn 的 cancel
	wg      sync.WaitGroup
}

type Option func(*Engine)

func WithTools(runner tool.Runner) Option {
	return func(e *Engine) {
		e.tools = runner
	}
}

func WithPromptSource(source contextbuilder.PromptSource) Option {
	return func(e *Engine) {
		e.builder = contextbuilder.New(e.store, source)
	}
}

func New(s store.Store, hub *event.Hub, resolver Resolver, cfg ConfigSource, opts ...Option) *Engine {
	if cfg == nil {
		cfg = emptyConfig{}
	}
	auxCtx, auxCancel := context.WithCancel(context.Background())
	e := &Engine{
		store:     s,
		config:    cfg,
		hub:       hub,
		resolver:  resolver,
		builder:   contextbuilder.New(s, nil),
		auxCtx:    auxCtx,
		auxCancel: auxCancel,
		running:   make(map[string]context.CancelFunc),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Stop 取消辅助 goroutine 的基 ctx(幂等);在 Wait() 前调用,
// 让 best-effort 后台任务(自动标题等)优雅退出时立即收手。
func (e *Engine) Stop() { e.auxCancel() }

type SubmitInput struct {
	SessionID       string
	ClientMessageID string
	Text            string
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

type resolvedModel struct {
	providerName string
	model        string
	config       provider.ModelConfig
	configJSON   json.RawMessage
}

func (e *Engine) Submit(ctx context.Context, in SubmitInput) (*SubmitResult, error) {
	if strings.TrimSpace(in.Text) == "" || in.ClientMessageID == "" {
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
	client, err := e.resolver.Resolve(ctx, resolved.providerName)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderConfig, err)
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
		Provider:        resolved.providerName,
		Model:           resolved.model,
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
	turnCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.running[in.SessionID] = cancel
	e.mu.Unlock()

	e.hub.Publish(*res.StartedEvent)

	// 空标题会话:首条消息触发自动标题(provisional + 异步 LLM),
	// 与 turn 生命周期解耦(titler.go)
	if sess.Title == "" {
		e.autoTitle(in.SessionID, resolved.providerName, resolved.model, resolved.config, in.Text)
	}

	e.wg.Add(1)
	go e.runTurn(turnCtx, in.SessionID, res.Turn.ID, resolved, client)

	return &SubmitResult{TurnID: res.Turn.ID, UserMessageID: res.UserMessage.ID}, nil
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
	req, err := e.builder.Build(ctx, sessionID, resolved.model)
	if err != nil {
		return nil, err
	}
	req.Config = resolved.config
	if e.tools != nil && modelSupportsTools(resolved.config) {
		defs, err := e.tools.Definitions(ctx, sessionID)
		if err != nil {
			return nil, fmt.Errorf("list tools: %w", err)
		}
		req.Tools = defs
	}
	estimate := contextbuilder.EstimateRequest(req)
	stat, err := e.store.SessionUsage(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	contextWindow := resolved.config.ContextWindow
	threshold := 0
	if contextWindow > 0 {
		threshold = contextWindow * 4 / 5
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
		Provider:        resolved.providerName,
		Model:           resolved.model,
		ModelConfig:     resolved.configJSON,
	})
	if err != nil {
		return nil, err
	}
	if res.ExistingTurn != nil {
		return &SubmitResult{Duplicate: true, TurnID: res.ExistingTurn.ID}, nil
	}
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

func (e *Engine) resolveModel(ctx context.Context, sess *store.Session) (*resolvedModel, error) {
	// provider / model 在提交时刻解析并随 turn 快照,改配置不影响进行中的 turn。
	providerName := strings.TrimSpace(sess.Provider)
	model := strings.TrimSpace(sess.Model)
	if providerName == "" || model == "" {
		return nil, ErrNoModel
	}

	var cfg provider.ModelConfig
	if p, err := e.config.GetProviderProfile(ctx, providerName); err == nil {
		if len(p.Models) > 0 && !p.HasModel(model) {
			return nil, ErrNoModel
		}
		if entry, ok := p.ModelByID(model); ok {
			cfg = modelConfigFromEntry(entry)
		}
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	return &resolvedModel{
		providerName: providerName,
		model:        model,
		config:       cfg,
		configJSON:   cfgJSON,
	}, nil
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
	status, errMsg := e.streamTurn(ctx, sessionID, turnID, resolved, client, &parts)
	e.finishTurn(sessionID, turnID, status, errMsg, parts.Parts())
}

func (e *Engine) finishTurn(sessionID, turnID string, status store.TurnStatus, errMsg string, assistantParts []store.ContentPart) {
	in := store.FinishTurnInput{TurnID: turnID, Status: status, Error: errMsg}
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
			e.clearRunning(sessionID)
			return
		}
		slog.Error("engine: finish turn", "turnID", turnID, "err", err)
		e.clearRunning(sessionID)
		return
	}
	e.clearRunning(sessionID)
	e.hub.Publish(*res.FinalEvent)
	e.TryDrainQueued(sessionID)
}

func (e *Engine) clearRunning(sessionID string) {
	e.mu.Lock()
	delete(e.running, sessionID)
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
	turnCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.running[sessionID] = cancel
	e.mu.Unlock()
	e.hub.Publish(*res.StartedEvent)

	var cfg provider.ModelConfig
	if len(res.Input.ModelConfig) > 0 {
		if err := json.Unmarshal(res.Input.ModelConfig, &cfg); err != nil {
			e.finishTurn(sessionID, res.Turn.ID, store.TurnFailed, fmt.Sprintf("model config: %v", err), nil)
			return
		}
	}
	client, err := e.resolver.Resolve(turnCtx, res.Input.Provider)
	if err != nil {
		e.finishTurn(sessionID, res.Turn.ID, store.TurnFailed, fmt.Sprintf("%v: %v", ErrProviderConfig, err), nil)
		return
	}
	resolved := &resolvedModel{
		providerName: res.Input.Provider,
		model:        res.Input.Model,
		config:       cfg,
		configJSON:   append(json.RawMessage(nil), res.Input.ModelConfig...),
	}
	e.wg.Add(1)
	go e.runTurn(turnCtx, sessionID, res.Turn.ID, resolved, client)
}

func (e *Engine) streamTurn(ctx context.Context, sessionID, turnID string, resolved *resolvedModel, client provider.Client, parts *turnPartAccumulator) (store.TurnStatus, string) {
	baseReq, err := e.builder.Build(ctx, sessionID, resolved.model)
	if err != nil {
		return store.TurnFailed, fmt.Sprintf("build context: %v", err)
	}
	baseReq.Config = resolved.config
	if e.tools != nil && modelSupportsTools(resolved.config) {
		defs, err := e.tools.Definitions(ctx, sessionID)
		if err != nil {
			return store.TurnFailed, fmt.Sprintf("list tools: %v", err)
		}
		baseReq.Tools = defs
	}

	maxLoops := defaultMaxToolLoops
	if v, ok := resolved.config.MaxToolLoops(); ok {
		maxLoops = v
	}
	for loop := 0; ; loop++ {
		req := baseReq
		req.Messages = requestMessagesWithTurnParts(baseReq.Messages, parts.Parts())
		ch, err := client.Stream(ctx, req)
		if err != nil {
			return store.TurnFailed, fmt.Sprintf("provider: %v", err)
		}
		finish, status, errMsg := e.consumeStream(ctx, sessionID, turnID, resolved.model, ch, parts)
		if status != store.TurnRunning {
			return status, errMsg
		}
		if finish == "" || finish == provider.FinishStop {
			return store.TurnCompleted, ""
		}
		if finish != provider.FinishToolCalls {
			return store.TurnFailed, fmt.Sprintf("unsupported finish reason: %s", finish)
		}
		if len(baseReq.Tools) == 0 {
			return store.TurnFailed, "provider requested tool calls but no tools are available"
		}
		if loop >= maxLoops {
			return store.TurnFailed, "max tool loops exceeded"
		}
		if status, msg := e.executePendingTools(ctx, sessionID, turnID, parts); status != store.TurnRunning {
			return status, msg
		}
	}
}

func modelSupportsTools(cfg provider.ModelConfig) bool {
	return cfg.Capabilities != nil && cfg.Capabilities.Tools
}

func (e *Engine) consumeStream(ctx context.Context, sessionID, turnID, model string, ch <-chan provider.Chunk, parts *turnPartAccumulator) (provider.FinishReason, store.TurnStatus, string) {
	coalescer := newStreamEventCoalescer(e.hub)
	defer coalescer.Flush()
	var usage provider.UsageInfo
	usageSeen := false

	for {
		select {
		case <-ctx.Done():
			return "", store.TurnCancelled, ""
		case <-coalescer.C():
			coalescer.Flush()
		case chunk, ok := <-ch:
			if !ok {
				// provider 违反契约提前关 channel:cancel 中的截断仍按 cancelled 收尾,
				// 避免用户主动停止被记成 failed。
				if ctx.Err() != nil {
					return "", store.TurnCancelled, ""
				}
				if usageSeen {
					e.recordUsage(ctx, sessionID, model, usage, 1)
				}
				return "", store.TurnFailed, "provider stream ended without terminal chunk"
			}
			switch {
			case chunk.Err != nil:
				if errors.Is(chunk.Err, context.Canceled) {
					return "", store.TurnCancelled, ""
				}
				if usageSeen {
					e.recordUsage(ctx, sessionID, model, usage, 1)
				}
				return "", store.TurnFailed, chunk.Err.Error()
			case chunk.Usage != nil:
				mergeUsageInfo(&usage, *chunk.Usage)
				usageSeen = true
			case chunk.Done:
				if usageSeen {
					e.recordUsage(ctx, sessionID, model, usage, 1)
				} else {
					e.recordUsage(ctx, sessionID, model, provider.UsageInfo{}, 1)
				}
				return chunk.Finish, store.TurnRunning, ""
			case chunk.Tool != nil:
				callID, name, argsDelta := parts.AppendTool(*chunk.Tool)
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
				parts.AppendDelta(part, chunk.Delta)
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
		if next.Summary != "" {
			previous.Summary = next.Summary
		}
		return previous, true
	}
	return event.Event{}, false
}

func (e *Engine) executePendingTools(ctx context.Context, sessionID, turnID string, parts *turnPartAccumulator) (store.TurnStatus, string) {
	calls := parts.PendingToolCalls()
	if len(calls) == 0 {
		return store.TurnFailed, "provider finished with tool_calls but emitted no complete tool call"
	}
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
		toolCtx, cancel := context.WithTimeout(ctx, toolCallTimeout)
		result := e.tools.Call(toolCtx, call)
		cancel()
		if ctx.Err() != nil {
			return store.TurnCancelled, ""
		}
		if toolCtx.Err() != nil && result.Content == "" {
			result = tool.Result{
				CallID:  call.CallID,
				Name:    call.Name,
				Ok:      false,
				Content: "tool timed out",
			}
		}
		if result.CallID == "" {
			result.CallID = call.CallID
		}
		if result.Name == "" {
			result.Name = call.Name
		}
		parts.AppendToolResult(result)
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
			SummaryKind:  result.SummaryKind,
			SummaryCount: result.SummaryCount,
		})
		if ctx.Err() != nil {
			return store.TurnCancelled, ""
		}
	}
	return store.TurnRunning, ""
}

type turnPartAccumulator struct {
	parts          []store.ContentPart
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
	out := store.CloneContentParts(a.parts)
	for key, partIndex := range a.toolPartByKey {
		if partIndex < 0 || partIndex >= len(out) {
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
			out[partIndex].Args = append(json.RawMessage(nil), args...)
		}
	}
	return store.NormalizeContentParts(out)
}

func requestMessagesWithTurnParts(base []provider.Message, parts []store.ContentPart) []provider.Message {
	out := cloneProviderMessages(base)
	providerParts := providerPartsFromStore(parts)
	if len(providerParts) > 0 {
		out = append(out, provider.Message{
			Role:  provider.RoleAssistant,
			Text:  store.TextFromParts(parts),
			Parts: providerParts,
		})
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

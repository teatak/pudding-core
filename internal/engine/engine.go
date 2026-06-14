// Package engine 实现 per-session turn 状态机,是 messages 的唯一写入方
// (docs/phase-1-plan.md 第 6 节)。turn lifecycle 事件全部由本包生成,
// provider 只供模型流(AGENTS.md 硬约束 17)。
package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

var (
	// ErrTurnRunning:第一阶段同一 session 不允许并发 turn,API 映射 409。
	ErrTurnRunning = store.ErrTurnRunning
	// ErrNoRunningTurn:cancel 时没有进行中的 turn,API 映射 409。
	ErrNoRunningTurn = errors.New("engine: no running turn")
	ErrEmptyInput    = errors.New("engine: empty text or clientMessageID")
	// ErrNoModel:会话未解析出可用 provider/model(空配置),提交时直接报错,
	// 不静默落内置 default profile / mock 兜底。API 映射 400 "no_model"。
	ErrNoModel = errors.New("engine: no model configured for session")
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
	store        store.Store
	config       ConfigSource
	hub          *event.Hub
	resolver     Resolver
	builder      *contextbuilder.Builder
	defaultModel string

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

func New(s store.Store, hub *event.Hub, resolver Resolver, cfg ConfigSource, defaultModel string) *Engine {
	if cfg == nil {
		cfg = emptyConfig{}
	}
	auxCtx, auxCancel := context.WithCancel(context.Background())
	return &Engine{
		store:        s,
		config:       cfg,
		hub:          hub,
		resolver:     resolver,
		builder:      contextbuilder.New(s, cfg),
		defaultModel: defaultModel,
		auxCtx:       auxCtx,
		auxCancel:    auxCancel,
		running:      make(map[string]context.CancelFunc),
	}
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
	Duplicate     bool   `json:"duplicate,omitempty"`
	TurnID        string `json:"turnID"`
	UserMessageID string `json:"userMessageID,omitempty"`
}

func (e *Engine) Submit(ctx context.Context, in SubmitInput) (*SubmitResult, error) {
	if strings.TrimSpace(in.Text) == "" || in.ClientMessageID == "" {
		return nil, ErrEmptyInput
	}
	sess, err := e.store.GetSession(ctx, in.SessionID)
	if err != nil {
		return nil, err
	}

	// provider / model 在提交时刻解析并随 turn 快照,改配置不影响进行中的 turn。
	// provider:session 字段 > config default_profile > 内置 "default";
	// model:session 字段 > profile.models[0] >(仅 dev/mock)--model。
	// 模型名只在 profile 下有意义,不存在全局默认模型。
	providerName := sess.Provider
	if providerName == "" {
		if settings, err := e.config.Settings(ctx); err == nil {
			providerName = settings[store.SettingDefaultProvider]
		}
	}
	if providerName == "" {
		providerName = store.DefaultProviderProfile
	}
	model := sess.Model
	if p, err := e.config.GetProviderProfile(ctx, providerName); err == nil {
		if model == "" {
			model = p.FirstModelID()
		} else if len(p.Models) > 0 && !p.HasModel(model) {
			return nil, ErrNoModel
		}
	}
	if model == "" {
		model = e.defaultModel
	}
	// 空配置直接报错,不静默回显 mock 或抛误导性的 "profile default not found":
	// model 解析不出,或 provider 名没有对应的可用 profile(如桌面/生产下从没配过的
	// 内置 "default"),提交即返回 ErrNoModel。--mock 下 resolver 是 Static(任意名都
	// 解析)、--model 给了非空 model,故此校验不影响 mock/测试。
	if model == "" {
		return nil, ErrNoModel
	}
	if _, err := e.resolver.Resolve(ctx, providerName); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoModel, err)
	}

	res, err := e.store.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       in.SessionID,
		TurnID:          store.NewID("turn"),
		UserMessageID:   store.NewID("msg"),
		ClientMessageID: in.ClientMessageID,
		UserText:        in.Text,
		Provider:        providerName,
		Model:           model,
	})
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
		e.autoTitle(in.SessionID, providerName, model, in.Text)
	}

	e.wg.Add(1)
	go e.runTurn(turnCtx, in.SessionID, res.Turn.ID, providerName, model)

	return &SubmitResult{TurnID: res.Turn.ID, UserMessageID: res.UserMessage.ID}, nil
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

func (e *Engine) runTurn(ctx context.Context, sessionID, turnID, providerName, model string) {
	defer e.wg.Done()
	defer func() {
		e.mu.Lock()
		delete(e.running, sessionID)
		e.mu.Unlock()
	}()

	var buf strings.Builder
	status, errMsg := e.streamTurn(ctx, sessionID, turnID, providerName, model, &buf)

	in := store.FinishTurnInput{TurnID: turnID, Status: status, Error: errMsg}
	if status == store.TurnCompleted {
		text := buf.String()
		in.AssistantText = &text
	} else if buf.Len() > 0 {
		// 半截输出保留为 canonical message 并标记 interrupted
		// (docs/technology-decisions.md 第 14 节的当前倾向)。
		text := buf.String()
		in.AssistantText = &text
		in.Interrupted = true
	}
	res, err := e.store.FinishTurn(context.Background(), in)
	if err != nil {
		// session 在 turn 进行中被删除:turn 已随级联删除消失,收尾无处可写,
		// 静默(删除路径已 cancel 本 turn,见 api deleteSession)。
		if errors.Is(err, store.ErrNotFound) {
			slog.Debug("engine: finish turn skipped, session/turn gone", "turnID", turnID)
			return
		}
		slog.Error("engine: finish turn", "turnID", turnID, "err", err)
		return
	}
	e.hub.Publish(*res.FinalEvent)
}

func (e *Engine) streamTurn(ctx context.Context, sessionID, turnID, providerName, model string, buf *strings.Builder) (store.TurnStatus, string) {
	req, err := e.builder.Build(ctx, sessionID, model)
	if err != nil {
		return store.TurnFailed, fmt.Sprintf("build context: %v", err)
	}
	client, err := e.resolver.Resolve(ctx, providerName)
	if err != nil {
		return store.TurnFailed, fmt.Sprintf("provider: %v", err)
	}
	ch, err := client.Stream(ctx, req)
	if err != nil {
		return store.TurnFailed, fmt.Sprintf("provider: %v", err)
	}
	for chunk := range ch {
		switch {
		case chunk.Err != nil:
			if errors.Is(chunk.Err, context.Canceled) {
				return store.TurnCancelled, ""
			}
			return store.TurnFailed, chunk.Err.Error()
		case chunk.Done:
			return store.TurnCompleted, ""
		case chunk.Delta != "":
			buf.WriteString(chunk.Delta)
			e.hub.Publish(event.Event{
				SessionID: sessionID,
				Kind:      event.TurnDelta,
				TurnID:    turnID,
				Delta:     chunk.Delta,
			})
		}
	}
	// provider 违反契约提前关 channel:cancel 中的截断仍按 cancelled 收尾,
	// 避免用户主动停止被记成 failed。
	if ctx.Err() != nil {
		return store.TurnCancelled, ""
	}
	return store.TurnFailed, "provider stream ended without terminal chunk"
}

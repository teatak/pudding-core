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
)

type Engine struct {
	store        store.Store
	hub          *event.Hub
	client       provider.Client
	builder      *contextbuilder.Builder
	defaultModel string

	mu      sync.Mutex
	running map[string]context.CancelFunc // sessionID → 当前 turn 的 cancel
	wg      sync.WaitGroup
}

func New(s store.Store, hub *event.Hub, client provider.Client, defaultModel string) *Engine {
	return &Engine{
		store:        s,
		hub:          hub,
		client:       client,
		builder:      contextbuilder.New(s),
		defaultModel: defaultModel,
		running:      make(map[string]context.CancelFunc),
	}
}

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

	res, err := e.store.BeginTurn(ctx, store.BeginTurnInput{
		SessionID:       in.SessionID,
		TurnID:          store.NewID("turn"),
		UserMessageID:   store.NewID("msg"),
		ClientMessageID: in.ClientMessageID,
		UserText:        in.Text,
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
	e.hub.Publish(*res.StartedEvent)

	// 模型解析:session.model > settings model.default > --model flag
	model := sess.Model
	if model == "" {
		if kv, err := e.store.Settings(ctx); err == nil {
			model = kv[store.SettingDefaultModel]
		}
	}
	if model == "" {
		model = e.defaultModel
	}
	// turn 的生命周期长于 HTTP 请求,不继承请求 ctx;取消只走 Cancel()。
	turnCtx, cancel := context.WithCancel(context.Background())
	e.mu.Lock()
	e.running[in.SessionID] = cancel
	e.mu.Unlock()

	e.wg.Add(1)
	go e.runTurn(turnCtx, in.SessionID, res.Turn.ID, model)

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

func (e *Engine) runTurn(ctx context.Context, sessionID, turnID, model string) {
	defer e.wg.Done()
	defer func() {
		e.mu.Lock()
		delete(e.running, sessionID)
		e.mu.Unlock()
	}()

	var buf strings.Builder
	status, errMsg := e.streamTurn(ctx, sessionID, turnID, model, &buf)

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
		slog.Error("engine: finish turn", "turnID", turnID, "err", err)
		return
	}
	e.hub.Publish(*res.FinalEvent)
}

func (e *Engine) streamTurn(ctx context.Context, sessionID, turnID, model string, buf *strings.Builder) (store.TurnStatus, string) {
	req, err := e.builder.Build(ctx, sessionID, model)
	if err != nil {
		return store.TurnFailed, fmt.Sprintf("build context: %v", err)
	}
	ch, err := e.client.Stream(ctx, req)
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

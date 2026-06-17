// Package store 定义持久层契约。messages 是 LLM context 的唯一事实源,
// turns 承载 turn 状态,events 只存 lifecycle 事件(docs/technology-decisions.md 第 6 节)。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/teatak/pudding-core/internal/event"
)

var (
	ErrNotFound = errors.New("store: not found")
	// ErrTurnRunning:同一 session 已有 running turn。第一阶段不允许并发 turn,
	// API 层映射为 409(docs/technology-decisions.md 第 14 节)。
	ErrTurnRunning = errors.New("store: session has a running turn")
)

// EventsRetainPerSession 是每个 session 的 lifecycle 事件保留条数。
// 窗口只需覆盖 SSE 断线续传;更早的事件随写入事务滚动清理,
// 超窗的缺口由客户端收到 lifecycle 后 refetch messages 兜底。
const EventsRetainPerSession = 1000

type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Provider 是 provider profile 名,空 = 默认 profile(settings provider.default)。
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// LastActivityAt 只描述会话内容活动时间:用户提交 / assistant 收尾推进。
	// 列表排序和"最近"时间显示使用它,避免 rename / 改模型把会话顶到最上面。
	LastActivityAt time.Time `json:"lastActivityAt"`
	// Running 是读取时从 turns 派生的运行态(不落库,turns 仍是唯一事实源),
	// 服务会话栏"哪个 session 正在干活"的指示。
	Running bool `json:"running"`
}

type SessionUpdate struct {
	Title    *string `json:"title"`
	Provider *string `json:"provider"`
	Model    *string `json:"model"`
}

// ProviderProfile 描述一个 LLM 端点实例。新的事实源是 config/profiles.yaml;
// store 里保留类型是为了让 registry / API / 测试共用契约。
// APIKey 只进不出:API 层读端点一律脱敏。
type ProviderProfile struct {
	ID        string          `json:"id" yaml:"-"`
	Name      string          `json:"name" yaml:"name,omitempty"`
	Type      string          `json:"type" yaml:"type"` // openai-compatible | openai-responses | google | ...
	BaseURL   string          `json:"baseURL" yaml:"base_url,omitempty"`
	APIKey    string          `json:"-" yaml:"api_key,omitempty"`
	APIKeyEnv string          `json:"apiKeyEnv,omitempty" yaml:"api_key_env,omitempty"`
	Models    []ProviderModel `json:"models" yaml:"models"`
	CreatedAt time.Time       `json:"createdAt,omitempty" yaml:"-"`
	UpdatedAt time.Time       `json:"updatedAt,omitempty" yaml:"-"`
}

func (p *ProviderProfile) ProfileID() string {
	if p == nil {
		return ""
	}
	if p.ID != "" {
		return p.ID
	}
	return p.Name
}

func (p *ProviderProfile) DisplayName() string {
	if p == nil {
		return ""
	}
	if p.Name != "" {
		return p.Name
	}
	return p.ProfileID()
}

func (p *ProviderProfile) HasModel(id string) bool {
	_, ok := p.ModelByID(id)
	return ok
}

func (p *ProviderProfile) FirstModelID() string {
	if p == nil || len(p.Models) == 0 {
		return ""
	}
	return p.Models[0].ID
}

func (p *ProviderProfile) ModelByID(id string) (ProviderModel, bool) {
	if p == nil || id == "" {
		return ProviderModel{}, false
	}
	for _, m := range p.Models {
		if m.ID == id {
			return m, true
		}
	}
	return ProviderModel{}, false
}

type ProviderModel struct {
	ID            string         `json:"id" yaml:"id"`
	Name          string         `json:"name,omitempty" yaml:"name,omitempty"`
	ContextWindow int            `json:"contextWindow,omitempty" yaml:"context_window,omitempty"`
	Capabilities  *ModelCaps     `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	OpenAI        map[string]any `json:"openai,omitempty" yaml:"openai,omitempty"`
	Google        map[string]any `json:"google,omitempty" yaml:"google,omitempty"`
	Anthropic     map[string]any `json:"anthropic,omitempty" yaml:"anthropic,omitempty"`
}

type ModelCaps struct {
	Image bool `json:"image" yaml:"image"`
	Audio bool `json:"audio" yaml:"audio"`
	Tools bool `json:"tools" yaml:"tools"`
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type Message struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	TurnID    string `json:"turnID"`
	Role      Role   `json:"role"`
	Text      string `json:"text"`
	// ClientMessageID 只在 user message 上有值,承载 submit 幂等与前端 overlay 对账。
	ClientMessageID string `json:"clientMessageID,omitempty"`
	// Interrupted 标记 cancel / failed 时保留的半截 assistant 输出
	// (开放问题的当前倾向:保留进 canonical context)。
	Interrupted bool      `json:"interrupted,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type TurnStatus string

const (
	TurnRunning   TurnStatus = "running"
	TurnCompleted TurnStatus = "completed"
	TurnFailed    TurnStatus = "failed"
	TurnCancelled TurnStatus = "cancelled"
)

type Turn struct {
	ID              string     `json:"id"`
	SessionID       string     `json:"sessionID"`
	ClientMessageID string     `json:"clientMessageID"`
	Status          TurnStatus `json:"status"`
	// Provider / Model / ModelConfig 是 BeginTurn 时刻的解析快照,审计与
	// 进行中 turn 稳定性用;用户改 profile 不影响已开始的 turn。
	Provider    string          `json:"provider,omitempty"`
	Model       string          `json:"model,omitempty"`
	ModelConfig json.RawMessage `json:"modelConfig,omitempty"`
	Error       string          `json:"error,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

type BeginTurnInput struct {
	SessionID       string
	TurnID          string
	UserMessageID   string
	ClientMessageID string
	UserText        string
	// Provider / Model 由 engine 在提交时刻解析后传入,随 turn 落库。
	Provider    string
	Model       string
	ModelConfig json.RawMessage
}

type BeginTurnResult struct {
	// Duplicate 表示 clientMessageID 已存在:返回已有 turn 与 user message,
	// 不开新 turn、不产生新事件(幂等语义)。
	Duplicate    bool
	Turn         *Turn
	UserMessage  *Message
	StartedEvent *event.Event // 已分配 seq 并落库;Duplicate 时为 nil
}

type FinishTurnInput struct {
	TurnID string
	Status TurnStatus // completed | failed | cancelled
	// AssistantText 为 nil 表示无产出(失败/早期取消时不落 assistant message)。
	AssistantText *string
	Interrupted   bool
	Error         string
}

type FinishTurnResult struct {
	AssistantMessage *Message // AssistantText 为 nil 时为 nil
	FinalEvent       *event.Event
}

// Store 的每个方法是一个完整事务。BeginTurn 与 FinishTurn 内部必须把
// message、turns 状态、lifecycle event 写在同一事务里(AGENTS.md 硬约束 15);
// 事件 seq 由 Store 在事务内按 session 单调分配。
// SQLite 实现要求 WAL + 单 writer;schema 契约见 schema.sql。
type Store interface {
	CreateSession(ctx context.Context, s *Session) error
	GetSession(ctx context.Context, id string) (*Session, error)
	ListSessions(ctx context.Context) ([]*Session, error)
	UpdateSession(ctx context.Context, id string, upd SessionUpdate) (*Session, error)
	DeleteSession(ctx context.Context, id string) error

	// BeginTurn:幂等检查(clientMessageID 重复则返回 Duplicate)→ 校验无
	// running turn(否则 ErrTurnRunning)→ 落 user message + running turn
	// + turn.started 事件。
	BeginTurn(ctx context.Context, in BeginTurnInput) (*BeginTurnResult, error)
	// FinishTurn:更新 turn 状态 + 落 assistant message(如有)+ 落 final 事件。
	FinishTurn(ctx context.Context, in FinishTurnInput) (*FinishTurnResult, error)
	// RunningTurn 返回 session 当前 running 的 turn,无则 ErrNotFound。
	RunningTurn(ctx context.Context, sessionID string) (*Turn, error)
	// RunningTurns 返回所有 session 的 running turn,服务 daemon 启动恢复。
	RunningTurns(ctx context.Context) ([]*Turn, error)

	// ListMessages 按时间升序返回最近 limit 条;limit <= 0 表示全部。
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*Message, error)
	// EventsAfter 返回 seq > afterSeq 的 lifecycle 事件,按 seq 升序,
	// 承载 SSE Last-Event-ID 续传;limit <= 0 表示全部。
	EventsAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]event.Event, error)
	// LatestSeq 返回 session 当前最大事件 seq(无事件为 0),
	// 服务无续传位点的全新 SSE 连接从尾部开始(tail)。
	LatestSeq(ctx context.Context, sessionID string) (int64, error)

	Close() error
}

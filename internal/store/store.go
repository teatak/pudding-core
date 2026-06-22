// Package store 定义持久层契约。messages 是 LLM context 的唯一事实源,
// turns 承载 turn 状态,events 只存 lifecycle 事件(docs/technology-decisions.md 第 6 节)。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/event"
)

var (
	ErrNotFound = errors.New("store: not found")
	// ErrTurnRunning:同一 session 已有 running turn。第一阶段不允许并发 turn,
	// API 层映射为 409(docs/technology-decisions.md 第 14 节)。
	ErrTurnRunning    = errors.New("store: session has a running turn")
	ErrInvalidSession = errors.New("store: session provider and model are required")
	ErrQueueBlocked   = errors.New("store: queued input is editing")
)

// EventsRetainPerSession 是每个 session 的 lifecycle 事件保留条数。
// 窗口只需覆盖 SSE 断线续传;更早的事件随写入事务滚动清理,
// 超窗的缺口由客户端收到 lifecycle 后 refetch messages 兜底。
const EventsRetainPerSession = 1000

type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Provider 是 provider profile 名;会话创建时必须显式写入。
	Provider  string    `json:"provider"`
	Model     string    `json:"model"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	// LastActivityAt 只描述会话内容活动时间:用户提交 / assistant 收尾推进。
	// 列表排序和"最近"时间显示使用它,避免 rename / 改模型把会话顶到最上面。
	LastActivityAt time.Time `json:"lastActivityAt"`
	Pinned         bool      `json:"pinned"`
	PinnedOrder    int64     `json:"pinnedOrder"`
	// Running 是读取时从 turns 派生的运行态(不落库,turns 仍是唯一事实源),
	// 服务会话栏"哪个 session 正在干活"的指示。
	Running bool `json:"running"`
}

type SessionUpdate struct {
	Title    *string `json:"title"`
	Provider *string `json:"provider"`
	Model    *string `json:"model"`
	Pinned   *bool   `json:"pinned"`
	// PinnedOrder 仅描述 pinned 组内手动排序,不改变最近会话排序。
	PinnedOrder *int64 `json:"pinnedOrder"`
}

// ProviderProfile 描述一个 LLM 端点实例。新的事实源是 config/profiles.yaml;
// store 里保留 provider 配置契约是为了让 registry / API / 测试共用。
// APIKey 存在本地配置中,API 设置视图会显式映射为 apiKey 用于编辑回显。
type ProviderProfile struct {
	ID          string          `json:"id" yaml:"-"`
	DisplayName string          `json:"displayName" yaml:"display_name,omitempty"`
	Brand       string          `json:"brand,omitempty" yaml:"brand,omitempty"`
	Protocol    string          `json:"protocol" yaml:"protocol"` // openai-compatible | openai-responses | google | ...
	BaseURL     string          `json:"baseURL" yaml:"base_url,omitempty"`
	APIKey      string          `json:"-" yaml:"api_key,omitempty"`
	Models      []ProviderModel `json:"models" yaml:"models"`
	CreatedAt   time.Time       `json:"createdAt,omitempty" yaml:"-"`
	UpdatedAt   time.Time       `json:"updatedAt,omitempty" yaml:"-"`
}

func (p *ProviderProfile) ProfileID() string {
	if p == nil {
		return ""
	}
	if p.ID != "" {
		return p.ID
	}
	return p.DisplayName
}

func (p *ProviderProfile) DisplayLabel() string {
	if p == nil {
		return ""
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return p.ProfileID()
}

func (p *ProviderProfile) HasModel(id string) bool {
	_, ok := p.ModelByID(id)
	return ok
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

func NormalizeSessionProviderModel(s *Session) error {
	if s == nil {
		return ErrInvalidSession
	}
	s.Provider = strings.TrimSpace(s.Provider)
	s.Model = strings.TrimSpace(s.Model)
	if s.Provider == "" || s.Model == "" {
		return ErrInvalidSession
	}
	return nil
}

func NormalizeSessionUpdate(upd *SessionUpdate) error {
	if upd.Provider != nil {
		provider := strings.TrimSpace(*upd.Provider)
		if provider == "" {
			return ErrInvalidSession
		}
		upd.Provider = &provider
	}
	if upd.Model != nil {
		model := strings.TrimSpace(*upd.Model)
		if model == "" {
			return ErrInvalidSession
		}
		upd.Model = &model
	}
	return nil
}

type ProviderModel struct {
	ID              string           `json:"id" yaml:"id"`
	DisplayName     string           `json:"displayName,omitempty" yaml:"display_name,omitempty"`
	ContextWindow   int              `json:"contextWindow,omitempty" yaml:"context_window,omitempty"`
	Capabilities    *ModelCaps       `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	Limits          *ModelLimits     `json:"limits,omitempty" yaml:"limits,omitempty"`
	ProviderOptions *ProviderOptions `json:"providerOptions,omitempty" yaml:"provider_options,omitempty"`
}

type ModelCaps struct {
	Image bool `json:"image" yaml:"image"`
	Audio bool `json:"audio" yaml:"audio"`
	Tools bool `json:"tools" yaml:"tools"`
}

type ModelLimits struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty" yaml:"max_output_tokens,omitempty"`
	MaxToolLoops    int `json:"maxToolLoops,omitempty" yaml:"max_tool_loops,omitempty"`
}

type ProviderOptions struct {
	OpenAI    map[string]any `json:"openai,omitempty" yaml:"openai,omitempty"`
	Google    map[string]any `json:"google,omitempty" yaml:"google,omitempty"`
	Anthropic map[string]any `json:"anthropic,omitempty" yaml:"anthropic,omitempty"`
}

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
	RoleSummary   Role = "summary"
)

type MessageKind string

const (
	MessageKindText       MessageKind = "text"
	MessageKindThought    MessageKind = "thought"
	MessageKindToolUse    MessageKind = "tool_use"
	MessageKindToolResult MessageKind = "tool_result"
	MessageKindSummary    MessageKind = "summary"
)

type ContentPartType string

const (
	ContentPartText       ContentPartType = "text"
	ContentPartThought    ContentPartType = "thought"
	ContentPartToolUse    ContentPartType = "tool_use"
	ContentPartToolResult ContentPartType = "tool_result"
)

type ContentPart struct {
	Type    ContentPartType `json:"type"`
	Text    string          `json:"text,omitempty"`
	CallID  string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
	Ok      bool            `json:"ok,omitempty"`
	Content string          `json:"content,omitempty"`
}

type Message struct {
	ID        string        `json:"id"`
	SessionID string        `json:"sessionID"`
	TurnID    string        `json:"turnID"`
	Role      Role          `json:"role"`
	Kind      MessageKind   `json:"kind"`
	Text      string        `json:"text"`
	Parts     []ContentPart `json:"parts"`
	TurnIndex int           `json:"turnIndex"`
	// ClientMessageID 只在 user message 上有值,承载 submit 幂等与前端 overlay 对账。
	ClientMessageID string `json:"clientMessageID,omitempty"`
	// Interrupted 标记 cancel / failed 时保留的半截 assistant 输出
	// (开放问题的当前倾向:保留进 canonical context)。
	Interrupted bool      `json:"interrupted,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func TextPart(text string) []ContentPart {
	if text == "" {
		return nil
	}
	return []ContentPart{{Type: ContentPartText, Text: text}}
}

func CloneContentParts(parts []ContentPart) []ContentPart {
	if parts == nil {
		return nil
	}
	out := make([]ContentPart, 0, len(parts))
	for _, part := range parts {
		cp := part
		if part.Args != nil {
			cp.Args = append(json.RawMessage(nil), part.Args...)
		}
		out = append(out, cp)
	}
	return out
}

func TextFromParts(parts []ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		if part.Type == ContentPartText {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

func MessageTextFromParts(parts []ContentPart) string {
	var b strings.Builder
	for _, part := range parts {
		switch part.Type {
		case ContentPartText, ContentPartThought:
			b.WriteString(part.Text)
		case ContentPartToolUse:
			if part.Name != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(part.Name)
			}
		case ContentPartToolResult:
			if part.Content != "" {
				if b.Len() > 0 {
					b.WriteByte('\n')
				}
				b.WriteString(part.Content)
			}
		}
	}
	return b.String()
}

func NormalizeContentParts(parts []ContentPart) []ContentPart {
	out := make([]ContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Type == "" {
			part.Type = ContentPartText
		}
		switch part.Type {
		case ContentPartText, ContentPartThought:
			if part.Text == "" {
				continue
			}
			part.CallID, part.Name, part.Args, part.Content = "", "", nil, ""
			part.Ok = false
		case ContentPartToolUse:
			if part.CallID == "" && part.Name == "" && len(part.Args) == 0 {
				continue
			}
			if len(part.Args) > 0 && !json.Valid(part.Args) {
				part.Args = nil
			}
			part.Text, part.Content = "", ""
			part.Ok = false
		case ContentPartToolResult:
			if part.CallID == "" && part.Content == "" {
				continue
			}
			part.Text, part.Args = "", nil
		default:
			continue
		}
		out = append(out, CloneContentParts([]ContentPart{part})[0])
	}
	return out
}

type AssistantOutputSegment struct {
	Role  Role
	Kind  MessageKind
	Text  string
	Parts []ContentPart
}

func FinishAssistantOutputSegments(in FinishTurnInput) []AssistantOutputSegment {
	parts := NormalizeContentParts(in.AssistantParts)
	if len(parts) == 0 && in.Status == TurnCompleted {
		return []AssistantOutputSegment{{
			Role:  RoleAssistant,
			Kind:  MessageKindText,
			Text:  "",
			Parts: []ContentPart{},
		}}
	}
	if len(parts) == 0 {
		return nil
	}
	out := make([]AssistantOutputSegment, 0, len(parts))
	for _, part := range parts {
		segment := AssistantOutputSegment{
			Role:  RoleAssistant,
			Kind:  messageKindForPart(part.Type),
			Text:  MessageTextFromParts([]ContentPart{part}),
			Parts: []ContentPart{part},
		}
		if part.Type == ContentPartToolResult {
			segment.Role = RoleTool
		}
		out = append(out, segment)
	}
	return out
}

func messageKindForPart(part ContentPartType) MessageKind {
	switch part {
	case ContentPartThought:
		return MessageKindThought
	case ContentPartToolUse:
		return MessageKindToolUse
	case ContentPartToolResult:
		return MessageKindToolResult
	case ContentPartText:
		fallthrough
	default:
		return MessageKindText
	}
}

type MessagePage struct {
	Messages []*Message
	HasMore  bool
}

type QueuedInputStatus string

const (
	QueuedInputQueued    QueuedInputStatus = "queued"
	QueuedInputEditing   QueuedInputStatus = "editing"
	QueuedInputCancelled QueuedInputStatus = "cancelled"
	QueuedInputPromoted  QueuedInputStatus = "promoted"
)

type QueuedInput struct {
	SessionID       string            `json:"sessionID"`
	ClientMessageID string            `json:"clientMessageID"`
	Text            string            `json:"text"`
	Status          QueuedInputStatus `json:"status"`
	Provider        string            `json:"provider,omitempty"`
	Model           string            `json:"model,omitempty"`
	ModelConfig     json.RawMessage   `json:"modelConfig,omitempty"`
	TurnID          string            `json:"turnID,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type QueueInputInput struct {
	SessionID       string
	ClientMessageID string
	Text            string
	Provider        string
	Model           string
	ModelConfig     json.RawMessage
}

type QueueInputResult struct {
	Duplicate    bool
	Input        *QueuedInput
	ExistingTurn *Turn
	QueuedEvent  *event.Event
}

type UpdateQueuedInputInput struct {
	SessionID       string
	ClientMessageID string
	Text            *string
	Status          *QueuedInputStatus
}

type UpdateQueuedInputResult struct {
	Input *QueuedInput
	Event *event.Event
}

type PromoteQueuedInputInput struct {
	SessionID     string
	TurnID        string
	UserMessageID string
}

type PromoteQueuedInputResult struct {
	Input        *QueuedInput
	Turn         *Turn
	UserMessage  *Message
	StartedEvent *event.Event
}

type ConversationTurn struct {
	ID              string     `json:"id"`
	SessionID       string     `json:"sessionID"`
	ClientMessageID string     `json:"clientMessageID"`
	Status          TurnStatus `json:"status"`
	Provider        string     `json:"provider,omitempty"`
	Model           string     `json:"model,omitempty"`
	Error           string     `json:"error,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	Messages        []*Message `json:"messages"`
}

type TurnPage struct {
	Turns   []*ConversationTurn
	HasMore bool
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
	// AssistantParts 为空时:completed 落一个空 assistant message 保持事件可定位;
	// failed/cancelled 不落 assistant message。
	AssistantParts []ContentPart
	Interrupted    bool
	Error          string
}

type FinishTurnResult struct {
	AssistantMessage  *Message // failed/cancelled 无产出时为 nil;多 segment 时指向第一条输出消息
	AssistantMessages []*Message
	FinalEvent        *event.Event
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
	// QueueInput 持久化等待发送的用户输入。Duplicate 表示同一
	// clientMessageID 已存在于 queued_inputs 或 turns,不重复写入。
	QueueInput(ctx context.Context, in QueueInputInput) (*QueueInputResult, error)
	ListQueuedInputs(ctx context.Context, sessionID string) ([]*QueuedInput, error)
	HasQueuedInputs(ctx context.Context, sessionID string) (bool, error)
	UpdateQueuedInput(ctx context.Context, in UpdateQueuedInputInput) (*UpdateQueuedInputResult, error)
	PromoteNextQueuedInput(ctx context.Context, in PromoteQueuedInputInput) (*PromoteQueuedInputResult, error)
	QueuedSessions(ctx context.Context) ([]string, error)
	// FinishTurn:更新 turn 状态 + 落 assistant message(如有)+ 落 final 事件。
	FinishTurn(ctx context.Context, in FinishTurnInput) (*FinishTurnResult, error)
	// RunningTurn 返回 session 当前 running 的 turn,无则 ErrNotFound。
	RunningTurn(ctx context.Context, sessionID string) (*Turn, error)
	// RunningTurns 返回所有 session 的 running turn,服务 daemon 启动恢复。
	RunningTurns(ctx context.Context) ([]*Turn, error)

	// ListMessages 按时间升序返回最近 limit 条;limit <= 0 表示全部。
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*Message, error)
	// ListMessagesPage 按时间升序返回一页 messages。beforeMessageID 为空时返回
	// 最近 limit 条;非空时返回该 message 之前的 limit 条。limit <= 0 表示不分页。
	ListMessagesPage(ctx context.Context, sessionID string, beforeMessageID string, limit int) (*MessagePage, error)
	// ListTurnsPage 按 turn 创建时间升序返回一页完整 turn。beforeTurnID 为空时
	// 返回最近 limit 个 turn;非空时返回该 turn 之前的 limit 个 turn。
	ListTurnsPage(ctx context.Context, sessionID string, beforeTurnID string, limit int) (*TurnPage, error)
	// GetConversationTurn 返回单个完整 turn,用于 lifecycle 终态后精确对账。
	GetConversationTurn(ctx context.Context, sessionID string, turnID string) (*ConversationTurn, error)
	// EventsAfter 返回 seq > afterSeq 的 lifecycle 事件,按 seq 升序,
	// 承载 SSE Last-Event-ID 续传;limit <= 0 表示全部。
	EventsAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]event.Event, error)
	// LatestSeq 返回 session 当前最大事件 seq(无事件为 0),
	// 服务无续传位点的全新 SSE 连接从尾部开始(tail)。
	LatestSeq(ctx context.Context, sessionID string) (int64, error)

	Close() error
}

// Package store 定义持久层契约。messages 是 LLM context 的唯一事实源,
// turns 承载 turn 状态,events 只存 lifecycle 事件(docs/technology-decisions.md 第 6 节)。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/event"
)

var (
	ErrNotFound = errors.New("store: not found")
	// ErrTurnRunning:同一 session 已有 running turn。第一阶段不允许并发 turn,
	// API 层映射为 409(docs/technology-decisions.md 第 14 节)。
	ErrTurnRunning              = errors.New("store: session has a running turn")
	ErrInvalidSession           = errors.New("store: session provider and model are required")
	ErrInvalidProject           = errors.New("store: invalid project")
	ErrQueueBlocked             = errors.New("store: queued input is editing")
	ErrInvalidCanvas            = errors.New("store: invalid canvas item")
	ErrCanvasConflict           = errors.New("store: saved canvas item changed")
	ErrInvalidBrowserState      = errors.New("store: invalid browser state")
	ErrInvalidBrowserHistory    = errors.New("store: invalid browser history")
	ErrInvalidComputerAppGrant  = errors.New("store: invalid Computer Use app grant")
	ErrHistorySearchUnavailable = errors.New("store: history search unavailable")
	ErrTurnFileChangeConflict   = errors.New("store: turn file change state conflict")
)

// EventsRetainPerSession 是每个 session 的 lifecycle 事件保留条数。
// 窗口只需覆盖 SSE 断线续传;更早的事件随写入事务滚动清理,
// 超窗的缺口由客户端收到 lifecycle 后 refetch messages 兜底。
const EventsRetainPerSession = 1000

type Session struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Provider 是 provider profile 名;会话创建时必须显式写入。
	Provider          string    `json:"provider"`
	Model             string    `json:"model"`
	ReasoningEffort   string    `json:"reasoningEffort,omitempty"`
	ReasoningModelKey string    `json:"reasoningModelKey,omitempty"`
	ActiveMode        AgentMode `json:"activeMode"`
	ModeLease         ModeLease `json:"modeLease"`
	// ProjectID 指向项目目录与审批设置的唯一事实源。
	ProjectID string `json:"projectID,omitempty"`
	// LoadedAppIDs 是当前会话已经加载、会向后续 turn 提供工具的 App。
	// 对客户端只读；写入仍只允许通过显式的 session App 接口。
	LoadedAppIDs []string  `json:"loadedAppIDs,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	// LastActivityAt 只描述会话内容活动时间:用户提交 / assistant 收尾推进。
	// 列表排序和"最近"时间显示使用它,避免 rename / 改模型把会话顶到最上面。
	LastActivityAt time.Time  `json:"lastActivityAt"`
	Pinned         bool       `json:"pinned"`
	PinnedOrder    int64      `json:"pinnedOrder"`
	ArchivedAt     *time.Time `json:"archivedAt,omitempty"`
	// Running 是读取时从 turns 派生的运行态(不落库,turns 仍是唯一事实源),
	// 服务会话栏"哪个 session 正在干活"的指示。
	Running bool `json:"running"`
	// BackgroundProcessCount 由 daemon 内存中的 session process manager 派生,
	// 不写入 SQLite。
	BackgroundProcessCount int `json:"backgroundProcessCount"`
}

type SessionUpdate struct {
	Title           *string    `json:"title"`
	Provider        *string    `json:"provider"`
	Model           *string    `json:"model"`
	ReasoningEffort *string    `json:"reasoningEffort"`
	ActiveMode      *AgentMode `json:"activeMode"`
	ModeLease       *ModeLease `json:"modeLease"`
	ProjectID       *string    `json:"projectID"`
	LoadedAppIDs    *[]string  `json:"-"`
	Pinned          *bool      `json:"pinned"`
	// PinnedOrder 仅描述 pinned 组内手动排序,不改变最近会话排序。
	PinnedOrder *int64 `json:"pinnedOrder"`
}

// CloneSessionInput creates an independent session from canonical history up to
// and including ThroughMessageID. AttachmentReplacements is keyed by the source
// attachment key and must point at the copied blob owned by TargetSessionID.
type CloneSessionInput struct {
	SourceSessionID        string
	ThroughMessageID       string
	TargetSessionID        string
	TitleSuffix            string
	AttachmentReplacements map[string]Attachment
}

func NormalizeCloneSessionInput(in *CloneSessionInput) error {
	if in == nil {
		return ErrInvalidSession
	}
	in.SourceSessionID = strings.TrimSpace(in.SourceSessionID)
	in.ThroughMessageID = strings.TrimSpace(in.ThroughMessageID)
	in.TargetSessionID = strings.TrimSpace(in.TargetSessionID)
	if in.SourceSessionID == "" || in.ThroughMessageID == "" || in.TargetSessionID == "" || strings.TrimSpace(in.TitleSuffix) == "" || in.SourceSessionID == in.TargetSessionID {
		return ErrInvalidSession
	}
	return nil
}

func CloneSessionTitle(sourceTitle, suffix string) string {
	return sourceTitle + suffix
}

type SessionListScope string

const (
	SessionListActive   SessionListScope = "active"
	SessionListArchived SessionListScope = "archived"
	SessionListAll      SessionListScope = "all"
)

type SessionListOptions struct {
	Scope SessionListScope
	Query string
}

func ResolveSessionListOptions(options []SessionListOptions) SessionListOptions {
	resolved := SessionListOptions{Scope: SessionListActive}
	if len(options) > 0 {
		resolved = options[0]
		if resolved.Scope == "" {
			resolved.Scope = SessionListActive
		}
	}
	resolved.Query = strings.TrimSpace(resolved.Query)
	return resolved
}

type ApprovalMode string

const (
	ApprovalAsk  ApprovalMode = "ask"
	ApprovalAuto ApprovalMode = "auto"
	ApprovalFull ApprovalMode = "full"
)

type Project struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// RootDirs preserves user order; the first entry is the primary project directory.
	RootDirs     []string     `json:"rootDirs"`
	ApprovalMode ApprovalMode `json:"approvalMode"`
	CreatedAt    time.Time    `json:"createdAt"`
	UpdatedAt    time.Time    `json:"updatedAt"`
	// LastActivityAt 由项目下会话的 LastActivityAt 最大值派生，不落 projects 表。
	LastActivityAt *time.Time `json:"lastActivityAt,omitempty"`
}

type ProjectUpdate struct {
	Name         *string       `json:"name"`
	RootDirs     *[]string     `json:"rootDirs"`
	ApprovalMode *ApprovalMode `json:"approvalMode"`
}

type AgentMode string

const (
	ModeChat AgentMode = "chat"
	ModeWork AgentMode = "work"
	ModeCode AgentMode = "code"
)

type ModeLease string

const (
	ModeLeaseNone    ModeLease = "none"
	ModeLeaseSession ModeLease = "session"
)

// ProviderProfile 描述一个 LLM 端点实例。新的事实源是 config/profiles.yaml;
// store 里保留 provider 配置契约是为了让 registry / API / 测试共用。
// APIKey 存在本地配置中,API 设置视图会显式映射为 apiKey 用于编辑回显。
type ProviderProfile struct {
	ID          string          `json:"id" yaml:"-"`
	DisplayName string          `json:"displayName" yaml:"display_name,omitempty"`
	Brand       string          `json:"brand,omitempty" yaml:"brand,omitempty"`
	Group       string          `json:"group,omitempty" yaml:"group,omitempty"`
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
	s.ReasoningEffort = strings.TrimSpace(s.ReasoningEffort)
	s.ReasoningModelKey = strings.TrimSpace(s.ReasoningModelKey)
	s.ProjectID = strings.TrimSpace(s.ProjectID)
	if s.ProjectID != "" {
		if s.ActiveMode == "" {
			s.ActiveMode = ModeCode
		}
		if s.ModeLease == "" {
			s.ModeLease = ModeLeaseSession
		}
	}
	if s.ActiveMode == "" {
		s.ActiveMode = ModeChat
	}
	s.ActiveMode = NormalizeAgentMode(s.ActiveMode)
	if s.ModeLease == "" {
		s.ModeLease = ModeLeaseNone
	}
	if !ValidAgentMode(s.ActiveMode) || !ValidModeLease(s.ModeLease) {
		return ErrInvalidSession
	}
	if s.ModeLease == ModeLeaseNone {
		s.ActiveMode = ModeChat
	}
	s.LoadedAppIDs = NormalizeAppIDs(s.LoadedAppIDs)
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
	if upd.ReasoningEffort != nil {
		effort := strings.TrimSpace(*upd.ReasoningEffort)
		upd.ReasoningEffort = &effort
	}
	if upd.ActiveMode != nil {
		mode := NormalizeAgentMode(*upd.ActiveMode)
		if !ValidAgentMode(mode) {
			return ErrInvalidSession
		}
		upd.ActiveMode = &mode
	}
	if upd.ModeLease != nil {
		lease := ModeLease(strings.TrimSpace(string(*upd.ModeLease)))
		if !ValidModeLease(lease) {
			return ErrInvalidSession
		}
		upd.ModeLease = &lease
		if lease == ModeLeaseNone && upd.ActiveMode == nil {
			mode := ModeChat
			upd.ActiveMode = &mode
		}
	}
	if upd.ProjectID != nil {
		projectID := strings.TrimSpace(*upd.ProjectID)
		upd.ProjectID = &projectID
		if projectID != "" {
			mode := ModeCode
			lease := ModeLeaseSession
			upd.ActiveMode = &mode
			upd.ModeLease = &lease
		}
	}
	if upd.LoadedAppIDs != nil {
		ids := NormalizeAppIDs(*upd.LoadedAppIDs)
		upd.LoadedAppIDs = &ids
	}
	return nil
}

func NormalizeAppIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

func NormalizeProject(project *Project) error {
	if project == nil {
		return ErrInvalidProject
	}
	project.ID = strings.TrimSpace(project.ID)
	project.Name = strings.TrimSpace(project.Name)
	project.RootDirs = NormalizeProjectDirs(project.RootDirs)
	project.ApprovalMode = NormalizeApprovalMode(project.ApprovalMode)
	if project.ID == "" {
		return ErrInvalidProject
	}
	if project.Name == "" {
		if len(project.RootDirs) == 0 {
			return ErrInvalidProject
		}
		project.Name = localFolderName(project.RootDirs[0])
	}
	return nil
}

func NormalizeProjectUpdate(upd *ProjectUpdate) error {
	if upd.Name != nil {
		name := strings.TrimSpace(*upd.Name)
		upd.Name = &name
	}
	if upd.RootDirs != nil {
		dirs := NormalizeProjectDirs(*upd.RootDirs)
		upd.RootDirs = &dirs
	}
	if upd.ApprovalMode != nil {
		mode := NormalizeApprovalMode(*upd.ApprovalMode)
		upd.ApprovalMode = &mode
	}
	return nil
}

func NormalizeApprovalMode(mode ApprovalMode) ApprovalMode {
	switch ApprovalMode(strings.TrimSpace(strings.ToLower(string(mode)))) {
	case ApprovalAsk:
		return ApprovalAsk
	case "", ApprovalAuto:
		return ApprovalAuto
	case ApprovalFull:
		return ApprovalFull
	default:
		return ApprovalAuto
	}
}

func NormalizeProjectDirs(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		cleaned := filepath.Clean(dir)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

func ValidAgentMode(mode AgentMode) bool {
	return NormalizeAgentMode(mode) != ""
}

func NormalizeAgentMode(mode AgentMode) AgentMode {
	switch AgentMode(strings.TrimSpace(strings.ToLower(string(mode)))) {
	case ModeCode:
		return ModeCode
	case ModeWork:
		return ModeWork
	case ModeChat:
		return ModeChat
	default:
		return ""
	}
}

func ValidModeLease(lease ModeLease) bool {
	return lease == ModeLeaseNone || lease == ModeLeaseSession
}

func AgentModeRank(mode AgentMode) int {
	switch NormalizeAgentMode(mode) {
	case ModeCode:
		return 3
	case ModeWork:
		return 2
	case ModeChat:
		fallthrough
	default:
		return 1
	}
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
	RoleSystem    Role = "system"
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
	ContentPartText        ContentPartType = "text"
	ContentPartThought     ContentPartType = "thought"
	ContentPartToolUse     ContentPartType = "tool_use"
	ContentPartToolResult  ContentPartType = "tool_result"
	ContentPartAttachment  ContentPartType = "attachment"
	ContentPartLocalFolder ContentPartType = "local_folder"
	ContentPartProjectRef  ContentPartType = "project_reference"
	ContentPartUIContext   ContentPartType = "ui_context"
	ContentPartFormResult  ContentPartType = "form_result"
)

type ContentPart struct {
	Type                ContentPartType `json:"type"`
	Text                string          `json:"text,omitempty"`
	CallID              string          `json:"id,omitempty"`
	Name                string          `json:"name,omitempty"`
	Args                json.RawMessage `json:"args,omitempty"`
	Ok                  bool            `json:"ok,omitempty"`
	Content             string          `json:"content,omitempty"`
	SummaryKind         string          `json:"summaryKind,omitempty"`
	SummaryCount        int             `json:"summaryCount,omitempty"`
	Attachments         []Attachment    `json:"attachments,omitempty"`
	AttachmentKey       string          `json:"attachmentKey,omitempty"`
	URL                 string          `json:"url,omitempty"`
	Path                string          `json:"path,omitempty"`
	SourcePath          string          `json:"sourcePath,omitempty"`
	MIME                string          `json:"mime,omitempty"`
	Size                int64           `json:"size,omitempty"`
	Origin              string          `json:"origin,omitempty"`
	AttachmentCreatedAt string          `json:"createdAt,omitempty"`
	AudioTranscript     string          `json:"audioTranscript,omitempty"`
	Surface             string          `json:"surface,omitempty"`
	Resource            string          `json:"resource,omitempty"`
	SelectionText       string          `json:"selectionText,omitempty"`
	ResourceKind        string          `json:"kind,omitempty"`
	RootID              string          `json:"rootID,omitempty"`
	StartLine           int             `json:"startLine,omitempty"`
	StartColumn         int             `json:"startColumn,omitempty"`
	EndLine             int             `json:"endLine,omitempty"`
	EndColumn           int             `json:"endColumn,omitempty"`
	Title               string          `json:"title,omitempty"`
	Schema              json.RawMessage `json:"schema,omitempty"`
	Result              json.RawMessage `json:"result,omitempty"`
}

func (p ContentPart) MarshalJSON() ([]byte, error) {
	type contentPartJSON struct {
		Type            ContentPartType `json:"type"`
		Text            string          `json:"text,omitempty"`
		CallID          string          `json:"id,omitempty"`
		Name            string          `json:"name,omitempty"`
		Args            json.RawMessage `json:"args,omitempty"`
		Ok              *bool           `json:"ok,omitempty"`
		Content         string          `json:"content,omitempty"`
		SummaryKind     string          `json:"summaryKind,omitempty"`
		SummaryCount    *int            `json:"summaryCount,omitempty"`
		Attachments     []Attachment    `json:"attachments,omitempty"`
		AttachmentKey   string          `json:"attachmentKey,omitempty"`
		URL             string          `json:"url,omitempty"`
		Path            string          `json:"path,omitempty"`
		SourcePath      string          `json:"sourcePath,omitempty"`
		MIME            string          `json:"mime,omitempty"`
		Size            int64           `json:"size,omitempty"`
		Origin          string          `json:"origin,omitempty"`
		CreatedAt       string          `json:"createdAt,omitempty"`
		AudioTranscript string          `json:"audioTranscript,omitempty"`
		Surface         string          `json:"surface,omitempty"`
		Resource        string          `json:"resource,omitempty"`
		SelectionText   string          `json:"selectionText,omitempty"`
		ResourceKind    string          `json:"kind,omitempty"`
		RootID          string          `json:"rootID,omitempty"`
		StartLine       int             `json:"startLine,omitempty"`
		StartColumn     int             `json:"startColumn,omitempty"`
		EndLine         int             `json:"endLine,omitempty"`
		EndColumn       int             `json:"endColumn,omitempty"`
		Title           string          `json:"title,omitempty"`
		Schema          json.RawMessage `json:"schema,omitempty"`
		Result          json.RawMessage `json:"result,omitempty"`
	}
	out := contentPartJSON{
		Type:            p.Type,
		Text:            p.Text,
		CallID:          p.CallID,
		Name:            p.Name,
		Args:            p.Args,
		Content:         p.Content,
		AttachmentKey:   p.AttachmentKey,
		URL:             p.URL,
		Path:            p.Path,
		SourcePath:      p.SourcePath,
		MIME:            p.MIME,
		Size:            p.Size,
		Origin:          p.Origin,
		CreatedAt:       p.AttachmentCreatedAt,
		AudioTranscript: p.AudioTranscript,
		Surface:         p.Surface,
		Resource:        p.Resource,
		SelectionText:   p.SelectionText,
		ResourceKind:    p.ResourceKind,
		RootID:          p.RootID,
		StartLine:       p.StartLine,
		StartColumn:     p.StartColumn,
		EndLine:         p.EndLine,
		EndColumn:       p.EndColumn,
		Title:           p.Title,
		Schema:          p.Schema,
		Result:          p.Result,
	}
	if p.Type == ContentPartToolResult {
		out.Ok = &p.Ok
		out.SummaryKind = p.SummaryKind
		out.Attachments = NormalizeAttachments(p.Attachments)
		if p.SummaryKind != "" {
			out.SummaryCount = &p.SummaryCount
		}
	}
	return json.Marshal(out)
}

type Attachment struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	AttachmentKey   string `json:"attachmentKey"`
	URL             string `json:"url"`
	MIME            string `json:"mime"`
	Size            int64  `json:"size"`
	Origin          string `json:"origin,omitempty"`
	SourcePath      string `json:"sourcePath,omitempty"`
	CreatedAt       string `json:"createdAt,omitempty"`
	AudioTranscript string `json:"audioTranscript,omitempty"`
}

type AttachmentCleanupItem struct {
	SessionID  string
	Attachment Attachment
}

type AttachmentCleanupResult struct {
	Attachments      []AttachmentCleanupItem
	MessageCount     int
	QueuedInputCount int
}

type LocalFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	Origin string `json:"origin,omitempty"`
}

type ProjectReference struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Path        string `json:"path"`
	SourcePath  string `json:"sourcePath"`
	RootID      string `json:"rootID"`
	Kind        string `json:"kind"`
	StartLine   int    `json:"startLine,omitempty"`
	StartColumn int    `json:"startColumn,omitempty"`
	EndLine     int    `json:"endLine,omitempty"`
	EndColumn   int    `json:"endColumn,omitempty"`
}

type Message struct {
	ID        string          `json:"id"`
	SessionID string          `json:"sessionID"`
	TurnID    string          `json:"turnID"`
	Role      Role            `json:"role"`
	Kind      MessageKind     `json:"kind"`
	Text      string          `json:"text"`
	Parts     []ContentPart   `json:"parts"`
	TurnIndex int             `json:"turnIndex"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	// ProviderState 是 canonical message 的隐藏协议状态。它只用于向同一
	// provider/model 重放原生 reasoning、签名和 tool-call items，不暴露给 UI。
	ProviderState *ProviderState `json:"-"`
	// ClientMessageID 只在 user message 上有值,承载 submit 幂等与前端 overlay 对账。
	ClientMessageID string `json:"clientMessageID,omitempty"`
	// Interrupted 标记 cancel / failed 时保留的半截 assistant 输出
	// (开放问题的当前倾向:保留进 canonical context)。
	Interrupted bool      `json:"interrupted,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type ProviderState struct {
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Kind     string          `json:"kind"`
	Data     json.RawMessage `json:"data"`
}

func CloneProviderState(state *ProviderState) *ProviderState {
	if state == nil {
		return nil
	}
	out := *state
	out.Data = append(json.RawMessage(nil), state.Data...)
	return &out
}

const (
	providerStateMetadataKey  = "_provider_state"
	publicMetadataEnvelopeKey = "_provider_public_metadata"
)

// EncodeMessageMetadataForStorage merges hidden provider state into the
// metadata persisted by the store. Public message metadata remains unchanged.
func EncodeMessageMetadataForStorage(metadata json.RawMessage, state *ProviderState) json.RawMessage {
	public := cloneValidMetadata(metadata)
	if !ValidProviderState(state) {
		if len(public) == 0 {
			return json.RawMessage(`{}`)
		}
		return public
	}
	stateData, err := json.Marshal(state)
	if err != nil {
		if len(public) == 0 {
			return json.RawMessage(`{}`)
		}
		return public
	}
	fields, object := decodeMetadataObject(public)
	if !object {
		fields = make(map[string]json.RawMessage)
		if len(public) > 0 {
			fields[publicMetadataEnvelopeKey] = public
		}
	} else if _, stateCollision := fields[providerStateMetadataKey]; stateCollision {
		fields = map[string]json.RawMessage{publicMetadataEnvelopeKey: public}
	} else if _, envelopeCollision := fields[publicMetadataEnvelopeKey]; envelopeCollision {
		fields = map[string]json.RawMessage{publicMetadataEnvelopeKey: public}
	}
	fields[providerStateMetadataKey] = stateData
	return encodeMetadataObject(fields)
}

// DecodeMessageMetadataFromStorage separates hidden provider state from
// metadata before a Message leaves the store boundary.
func DecodeMessageMetadataFromStorage(raw json.RawMessage) (json.RawMessage, *ProviderState) {
	fields, object := decodeMetadataObject(raw)
	if !object {
		return cloneValidMetadata(raw), nil
	}
	var state ProviderState
	if data := fields[providerStateMetadataKey]; len(data) > 0 {
		_ = json.Unmarshal(data, &state)
	}
	if !ValidProviderState(&state) {
		if len(fields) == 0 {
			return nil, nil
		}
		return append(json.RawMessage(nil), raw...), nil
	}
	if public := fields[publicMetadataEnvelopeKey]; len(public) > 0 && json.Valid(public) {
		return append(json.RawMessage(nil), public...), CloneProviderState(&state)
	}
	delete(fields, providerStateMetadataKey)
	if len(fields) == 0 {
		return nil, CloneProviderState(&state)
	}
	return encodeMetadataObject(fields), CloneProviderState(&state)
}

func ValidProviderState(state *ProviderState) bool {
	return state != nil &&
		strings.TrimSpace(state.Provider) != "" &&
		strings.TrimSpace(state.Model) != "" &&
		strings.TrimSpace(state.Kind) != "" &&
		len(state.Data) > 0 &&
		json.Valid(state.Data)
}

func decodeMetadataObject(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	fields := make(map[string]json.RawMessage)
	if len(raw) == 0 || !json.Valid(raw) || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	return fields, true
}

func encodeMetadataObject(fields map[string]json.RawMessage) json.RawMessage {
	if len(fields) == 0 {
		return json.RawMessage(`{}`)
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func cloneValidMetadata(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

type MessageSearchInput struct {
	SessionID string
	Query     string
	Limit     int
	// Literal treats Query as user-entered text instead of an FTS expression.
	Literal bool
	// Exact restricts literal search to the complete case-insensitive query.
	Exact bool
	// VisibleTranscriptOnly restricts results to user/assistant text shown as transcript messages.
	VisibleTranscriptOnly bool
	// NoLimit returns every exact match. It is reserved for explicitly scoped,
	// local conversation search; broader search surfaces must remain bounded.
	NoLimit bool
}

func TextPart(text string) []ContentPart {
	if text == "" {
		return nil
	}
	return []ContentPart{{Type: ContentPartText, Text: text}}
}

func UserInputParts(text string, parts []ContentPart) []ContentPart {
	parts = NormalizeContentParts(parts)
	out := make([]ContentPart, 0, len(parts))
	if partText := TextFromParts(parts); strings.TrimSpace(partText) != "" {
		text = partText
	}
	for _, part := range parts {
		switch part.Type {
		case ContentPartAttachment, ContentPartLocalFolder, ContentPartProjectRef, ContentPartUIContext, ContentPartFormResult:
			out = append(out, part)
		}
	}
	return appendUserTextPart(out, text)
}

func UserInputPartsWithAttachments(text string, parts []ContentPart, attachments []Attachment) []ContentPart {
	parts = UserInputParts(text, parts)
	attachments = NormalizeAttachments(attachments)
	byID := make(map[string]Attachment, len(attachments))
	byKey := make(map[string]Attachment, len(attachments))
	for _, attachment := range attachments {
		byID[attachment.ID] = attachment
		byKey[attachment.AttachmentKey] = attachment
	}
	out := make([]ContentPart, 0, len(parts))
	for _, part := range parts {
		if part.Type != ContentPartAttachment {
			out = append(out, part)
			continue
		}
		attachment, ok := byID[part.CallID]
		if !ok {
			attachment, ok = byKey[part.AttachmentKey]
		}
		if ok {
			out = append(out, AttachmentPart(attachment))
		}
	}
	return out
}

func ReplaceUserInputText(parts []ContentPart, text string) []ContentPart {
	parts = UserInputParts("", parts)
	out := make([]ContentPart, 0, len(parts)+1)
	for _, part := range parts {
		if part.Type != ContentPartText {
			out = append(out, part)
		}
	}
	return appendUserTextPart(out, text)
}

func AttachmentPart(attachment Attachment) ContentPart {
	return ContentPart{
		Type:                ContentPartAttachment,
		CallID:              attachment.ID,
		Name:                attachment.Name,
		AttachmentKey:       attachment.AttachmentKey,
		URL:                 attachment.URL,
		MIME:                attachment.MIME,
		Size:                attachment.Size,
		Origin:              attachment.Origin,
		SourcePath:          attachment.SourcePath,
		AttachmentCreatedAt: attachment.CreatedAt,
		AudioTranscript:     attachment.AudioTranscript,
	}
}

func LocalFolderPart(folder LocalFolder) ContentPart {
	return ContentPart{
		Type:   ContentPartLocalFolder,
		CallID: folder.ID,
		Name:   folder.Name,
		Path:   folder.Path,
		Origin: folder.Origin,
	}
}

func ProjectReferencePart(reference ProjectReference) ContentPart {
	return ContentPart{
		Type:         ContentPartProjectRef,
		CallID:       reference.ID,
		Name:         reference.Name,
		Path:         reference.Path,
		SourcePath:   reference.SourcePath,
		RootID:       reference.RootID,
		ResourceKind: reference.Kind,
		StartLine:    reference.StartLine,
		StartColumn:  reference.StartColumn,
		EndLine:      reference.EndLine,
		EndColumn:    reference.EndColumn,
	}
}

func appendUserTextPart(parts []ContentPart, text string) []ContentPart {
	if strings.TrimSpace(text) == "" {
		return parts
	}
	return append(parts, ContentPart{Type: ContentPartText, Text: text})
}

func NormalizeAttachments(attachments []Attachment) []Attachment {
	out := make([]Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		attachment.ID = strings.TrimSpace(attachment.ID)
		attachment.Name = strings.TrimSpace(attachment.Name)
		attachment.AttachmentKey = strings.TrimSpace(attachment.AttachmentKey)
		attachment.URL = strings.TrimSpace(attachment.URL)
		attachment.MIME = strings.TrimSpace(attachment.MIME)
		attachment.Origin = strings.TrimSpace(attachment.Origin)
		attachment.SourcePath = strings.TrimSpace(attachment.SourcePath)
		attachment.CreatedAt = strings.TrimSpace(attachment.CreatedAt)
		attachment.AudioTranscript = strings.TrimSpace(attachment.AudioTranscript)
		if attachment.ID == "" || attachment.Name == "" || attachment.AttachmentKey == "" || attachment.MIME == "" || attachment.Size < 0 {
			continue
		}
		out = append(out, attachment)
	}
	return out
}

func AttachmentsFromParts(parts []ContentPart) []Attachment {
	out := make([]Attachment, 0, len(parts))
	for _, part := range NormalizeContentParts(parts) {
		if part.Type == ContentPartToolResult {
			out = append(out, part.Attachments...)
		}
		if part.Type == ContentPartAttachment {
			out = append(out, Attachment{
				ID:              part.CallID,
				Name:            part.Name,
				AttachmentKey:   part.AttachmentKey,
				URL:             part.URL,
				MIME:            part.MIME,
				Size:            part.Size,
				Origin:          part.Origin,
				SourcePath:      part.SourcePath,
				CreatedAt:       part.AttachmentCreatedAt,
				AudioTranscript: part.AudioTranscript,
			})
		}
	}
	return dedupeAttachments(out)
}

func RemoveAttachmentPartsByOrigin(parts []ContentPart, origin string) ([]ContentPart, []Attachment, bool) {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return NormalizeContentParts(parts), nil, false
	}
	parts = NormalizeContentParts(parts)
	next := make([]ContentPart, 0, len(parts))
	removed := make([]Attachment, 0)
	for _, part := range parts {
		if part.Type == ContentPartToolResult {
			kept := make([]Attachment, 0, len(part.Attachments))
			for _, item := range part.Attachments {
				if item.Origin == origin {
					removed = append(removed, item)
					continue
				}
				kept = append(kept, item)
			}
			part.Attachments = kept
		}
		if part.Type == ContentPartAttachment && part.Origin == origin {
			removed = append(removed, Attachment{
				ID:              part.CallID,
				Name:            part.Name,
				AttachmentKey:   part.AttachmentKey,
				URL:             part.URL,
				MIME:            part.MIME,
				Size:            part.Size,
				Origin:          part.Origin,
				SourcePath:      part.SourcePath,
				CreatedAt:       part.AttachmentCreatedAt,
				AudioTranscript: part.AudioTranscript,
			})
			continue
		}
		next = append(next, part)
	}
	removed = dedupeAttachments(removed)
	return NormalizeContentParts(next), removed, len(removed) > 0
}

func dedupeAttachments(attachments []Attachment) []Attachment {
	attachments = NormalizeAttachments(attachments)
	out := make([]Attachment, 0, len(attachments))
	seen := make(map[string]struct{}, len(attachments))
	for _, item := range attachments {
		key := item.AttachmentKey
		if key == "" {
			key = item.URL
		}
		if key == "" {
			key = item.ID
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func NormalizeLocalFolders(folders []LocalFolder) []LocalFolder {
	out := make([]LocalFolder, 0, len(folders))
	for _, folder := range folders {
		folder.ID = strings.TrimSpace(folder.ID)
		folder.Name = strings.TrimSpace(folder.Name)
		folder.Path = strings.TrimSpace(folder.Path)
		folder.Origin = strings.TrimSpace(folder.Origin)
		if folder.ID == "" || folder.Path == "" {
			continue
		}
		if folder.Name == "" {
			folder.Name = localFolderName(folder.Path)
		}
		out = append(out, folder)
	}
	return out
}

func LocalFoldersFromParts(parts []ContentPart) []LocalFolder {
	out := make([]LocalFolder, 0, len(parts))
	for _, part := range NormalizeContentParts(parts) {
		if part.Type != ContentPartLocalFolder {
			continue
		}
		out = append(out, LocalFolder{
			ID:     part.CallID,
			Name:   part.Name,
			Path:   part.Path,
			Origin: part.Origin,
		})
	}
	return NormalizeLocalFolders(out)
}

func NormalizeProjectReferences(references []ProjectReference) []ProjectReference {
	out := make([]ProjectReference, 0, len(references))
	for _, reference := range references {
		reference.ID = strings.TrimSpace(reference.ID)
		reference.Name = strings.TrimSpace(reference.Name)
		reference.Path = strings.TrimSpace(reference.Path)
		reference.SourcePath = strings.TrimSpace(reference.SourcePath)
		reference.RootID = strings.TrimSpace(reference.RootID)
		reference.Kind = strings.TrimSpace(reference.Kind)
		if reference.ID == "" || reference.Name == "" || reference.Path == "" || reference.SourcePath == "" || reference.RootID == "" {
			continue
		}
		if !filepath.IsAbs(reference.SourcePath) {
			continue
		}
		if reference.Kind != "file" && reference.Kind != "dir" {
			continue
		}
		if !validProjectReferenceRange(reference) {
			continue
		}
		out = append(out, reference)
	}
	return out
}

func validProjectReferenceRange(reference ProjectReference) bool {
	values := []int{reference.StartLine, reference.StartColumn, reference.EndLine, reference.EndColumn}
	set := 0
	for _, value := range values {
		if value < 0 {
			return false
		}
		if value != 0 {
			set++
		}
	}
	if set == 0 {
		return true
	}
	if reference.Kind != "file" || set != len(values) {
		return false
	}
	return reference.EndLine > reference.StartLine ||
		(reference.EndLine == reference.StartLine && reference.EndColumn >= reference.StartColumn)
}

func ProjectReferencesFromParts(parts []ContentPart) []ProjectReference {
	out := make([]ProjectReference, 0, len(parts))
	for _, part := range NormalizeContentParts(parts) {
		if part.Type != ContentPartProjectRef {
			continue
		}
		out = append(out, ProjectReference{
			ID:          part.CallID,
			Name:        part.Name,
			Path:        part.Path,
			SourcePath:  part.SourcePath,
			RootID:      part.RootID,
			Kind:        part.ResourceKind,
			StartLine:   part.StartLine,
			StartColumn: part.StartColumn,
			EndLine:     part.EndLine,
			EndColumn:   part.EndColumn,
		})
	}
	return NormalizeProjectReferences(out)
}

func localFolderName(path string) string {
	normalized := strings.TrimRight(strings.ReplaceAll(strings.TrimSpace(path), "\\", "/"), "/")
	if normalized == "" {
		return path
	}
	name := filepath.Base(normalized)
	if name == "." || name == string(filepath.Separator) {
		return normalized
	}
	return name
}

type MessageMetadata struct {
	Compact *CompactMetadata `json:"compact,omitempty"`
}

type CompactMetadata struct {
	SourceMessageIDs []string `json:"source_message_ids,omitempty"`
	TailMessageIDs   []string `json:"tail_message_ids,omitempty"`
	SourceTurnCount  int      `json:"source_turn_count,omitempty"`
	TailTurnCount    int      `json:"tail_turn_count,omitempty"`
}

func CompactMessageMetadata(sourceIDs, tailIDs []string) json.RawMessage {
	return CompactMessageMetadataWithCounts(sourceIDs, tailIDs, 0, 0)
}

func CompactMessageMetadataWithCounts(sourceIDs, tailIDs []string, sourceTurns, tailTurns int) json.RawMessage {
	meta := MessageMetadata{Compact: &CompactMetadata{
		SourceMessageIDs: cloneStringSlice(sourceIDs),
		TailMessageIDs:   cloneStringSlice(tailIDs),
		SourceTurnCount:  sourceTurns,
		TailTurnCount:    tailTurns,
	}}
	data, err := json.Marshal(meta)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

func CompactMetadataFromMessage(msg *Message) (*CompactMetadata, bool) {
	if msg == nil || len(msg.Metadata) == 0 || !json.Valid(msg.Metadata) {
		return nil, false
	}
	var meta MessageMetadata
	if err := json.Unmarshal(msg.Metadata, &meta); err != nil || meta.Compact == nil {
		return nil, false
	}
	out := *meta.Compact
	out.SourceMessageIDs = cloneStringSlice(out.SourceMessageIDs)
	out.TailMessageIDs = cloneStringSlice(out.TailMessageIDs)
	return &out, true
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return append([]string(nil), values...)
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
		if part.Attachments != nil {
			cp.Attachments = append([]Attachment(nil), part.Attachments...)
		}
		if part.Schema != nil {
			cp.Schema = append(json.RawMessage(nil), part.Schema...)
		}
		if part.Result != nil {
			cp.Result = append(json.RawMessage(nil), part.Result...)
		}
		out = append(out, cp)
	}
	return out
}

func ReplaceContentPartAttachments(parts []ContentPart, replacements map[string]Attachment) []ContentPart {
	out := CloneContentParts(parts)
	references := make(map[string]string)
	addReferences := func(source Attachment) {
		if source.AttachmentKey == "" {
			return
		}
		target, ok := replacements[source.AttachmentKey]
		if !ok {
			return
		}
		references[source.AttachmentKey] = target.AttachmentKey
		if source.URL != "" && target.URL != "" {
			references[source.URL] = target.URL
		}
	}
	for _, part := range out {
		if part.Type == ContentPartAttachment {
			addReferences(Attachment{AttachmentKey: part.AttachmentKey, URL: part.URL})
		}
		if part.Type == ContentPartToolResult {
			for _, item := range part.Attachments {
				addReferences(item)
			}
		}
	}
	replaceReferences := func(value string) string {
		for source, target := range references {
			value = strings.ReplaceAll(value, source, target)
		}
		return value
	}
	for i := range out {
		part := &out[i]
		part.Content = replaceReferences(part.Content)
		if len(part.Args) > 0 {
			replaced := json.RawMessage(replaceReferences(string(part.Args)))
			if json.Valid(replaced) {
				part.Args = replaced
			}
		}
		if part.Type == ContentPartAttachment {
			if replacement, ok := replacements[part.AttachmentKey]; ok {
				*part = AttachmentPart(replacement)
			}
			continue
		}
		if part.Type != ContentPartToolResult {
			continue
		}
		for j := range part.Attachments {
			if replacement, ok := replacements[part.Attachments[j].AttachmentKey]; ok {
				part.Attachments[j] = replacement
			}
		}
	}
	return NormalizeContentParts(out)
}

func RemapCompactMessageMetadata(metadata json.RawMessage, messageIDs map[string]string) json.RawMessage {
	if len(metadata) == 0 || !json.Valid(metadata) {
		return append(json.RawMessage(nil), metadata...)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &fields); err != nil {
		return append(json.RawMessage(nil), metadata...)
	}
	rawCompact, ok := fields["compact"]
	if !ok {
		return append(json.RawMessage(nil), metadata...)
	}
	var compact CompactMetadata
	if err := json.Unmarshal(rawCompact, &compact); err != nil {
		return append(json.RawMessage(nil), metadata...)
	}
	remap := func(source []string) []string {
		out := make([]string, 0, len(source))
		for _, id := range source {
			if next := messageIDs[id]; next != "" {
				out = append(out, next)
			}
		}
		return out
	}
	compact.SourceMessageIDs = remap(compact.SourceMessageIDs)
	compact.TailMessageIDs = remap(compact.TailMessageIDs)
	rawCompact, err := json.Marshal(compact)
	if err != nil {
		return append(json.RawMessage(nil), metadata...)
	}
	fields["compact"] = rawCompact
	out, err := json.Marshal(fields)
	if err != nil {
		return append(json.RawMessage(nil), metadata...)
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
		case ContentPartAttachment:
			continue
		case ContentPartLocalFolder:
			continue
		case ContentPartProjectRef:
			continue
		case ContentPartUIContext:
			continue
		case ContentPartFormResult:
			continue
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
		if part.Type == ContentPartToolResult {
			part.Attachments = NormalizeAttachments(part.Attachments)
		} else {
			part.Attachments = nil
		}
		if part.Type != ContentPartUIContext {
			part.Surface, part.Resource, part.SelectionText = "", "", ""
		}
		if part.Type != ContentPartUIContext && part.Type != ContentPartProjectRef {
			part.ResourceKind, part.RootID = "", ""
		}
		if part.Type != ContentPartProjectRef {
			part.StartLine, part.StartColumn, part.EndLine, part.EndColumn = 0, 0, 0, 0
		}
		if part.Type != ContentPartFormResult {
			part.Title, part.Schema, part.Result = "", nil, nil
		}
		switch part.Type {
		case ContentPartText, ContentPartThought:
			if part.Text == "" {
				continue
			}
			part.CallID, part.Name, part.Args, part.Content = "", "", nil, ""
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
		case ContentPartToolUse:
			if part.CallID == "" && part.Name == "" && len(part.Args) == 0 {
				continue
			}
			if len(part.Args) > 0 && !json.Valid(part.Args) {
				part.Args = nil
			}
			part.Text, part.Content = "", ""
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
		case ContentPartToolResult:
			if part.CallID == "" && part.Content == "" {
				continue
			}
			part.Text, part.Args = "", nil
		case ContentPartAttachment:
			attachment := NormalizeAttachments([]Attachment{{
				ID:              part.CallID,
				Name:            part.Name,
				AttachmentKey:   part.AttachmentKey,
				URL:             part.URL,
				MIME:            part.MIME,
				Size:            part.Size,
				Origin:          part.Origin,
				SourcePath:      part.SourcePath,
				CreatedAt:       part.AttachmentCreatedAt,
				AudioTranscript: part.AudioTranscript,
			}})
			if len(attachment) == 0 {
				continue
			}
			part.CallID = attachment[0].ID
			part.Name = attachment[0].Name
			part.AttachmentKey = attachment[0].AttachmentKey
			part.URL = attachment[0].URL
			part.MIME = attachment[0].MIME
			part.Size = attachment[0].Size
			part.Origin = attachment[0].Origin
			part.SourcePath = attachment[0].SourcePath
			part.AttachmentCreatedAt = attachment[0].CreatedAt
			part.AudioTranscript = attachment[0].AudioTranscript
			part.Text, part.Args, part.Content = "", nil, ""
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
		case ContentPartLocalFolder:
			folder := NormalizeLocalFolders([]LocalFolder{{
				ID:     part.CallID,
				Name:   part.Name,
				Path:   part.Path,
				Origin: part.Origin,
			}})
			if len(folder) == 0 {
				continue
			}
			part.CallID = folder[0].ID
			part.Name = folder[0].Name
			part.Path = folder[0].Path
			part.Origin = folder[0].Origin
			part.Text, part.Args, part.Content = "", nil, ""
			part.AttachmentKey, part.URL, part.MIME = "", "", ""
			part.SourcePath = ""
			part.Size = 0
			part.AttachmentCreatedAt, part.AudioTranscript = "", ""
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
		case ContentPartProjectRef:
			reference := NormalizeProjectReferences([]ProjectReference{{
				ID:          part.CallID,
				Name:        part.Name,
				Path:        part.Path,
				SourcePath:  part.SourcePath,
				RootID:      part.RootID,
				Kind:        part.ResourceKind,
				StartLine:   part.StartLine,
				StartColumn: part.StartColumn,
				EndLine:     part.EndLine,
				EndColumn:   part.EndColumn,
			}})
			if len(reference) == 0 {
				continue
			}
			part.CallID = reference[0].ID
			part.Name = reference[0].Name
			part.Path = reference[0].Path
			part.SourcePath = reference[0].SourcePath
			part.RootID = reference[0].RootID
			part.ResourceKind = reference[0].Kind
			part.StartLine = reference[0].StartLine
			part.StartColumn = reference[0].StartColumn
			part.EndLine = reference[0].EndLine
			part.EndColumn = reference[0].EndColumn
			part.Text, part.Args, part.Content = "", nil, ""
			part.AttachmentKey, part.URL, part.MIME, part.Origin = "", "", "", ""
			part.Size = 0
			part.AttachmentCreatedAt, part.AudioTranscript = "", ""
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
		case ContentPartUIContext:
			part.Surface = strings.TrimSpace(part.Surface)
			switch part.Surface {
			case "project", "canvas", "browser", "terminal", "file_preview":
			default:
				continue
			}
			if part.Surface != "browser" {
				part.SelectionText = ""
			}
			part.Resource = strings.TrimSpace(part.Resource)
			switch part.Resource {
			case "", "project_file", "project_diff", "canvas_item", "browser_tab", "terminal", "file":
			default:
				part.Resource = ""
			}
			part.CallID = strings.TrimSpace(part.CallID)
			part.Name = strings.TrimSpace(part.Name)
			part.Path = strings.TrimSpace(part.Path)
			part.URL = strings.TrimSpace(part.URL)
			part.SelectionText = strings.TrimSpace(part.SelectionText)
			if selection := []rune(part.SelectionText); len(selection) > 16*1024 {
				part.SelectionText = string(selection[:16*1024])
			}
			part.ResourceKind = strings.TrimSpace(part.ResourceKind)
			part.RootID = strings.TrimSpace(part.RootID)
			part.Text, part.Args, part.Content = "", nil, ""
			part.AttachmentKey, part.SourcePath, part.MIME, part.Origin = "", "", "", ""
			part.Size = 0
			part.AttachmentCreatedAt, part.AudioTranscript = "", ""
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
		case ContentPartFormResult:
			part.Title = strings.TrimSpace(part.Title)
			var schemaValue, resultValue map[string]any
			if part.Title == "" || json.Unmarshal(part.Schema, &schemaValue) != nil || schemaValue == nil || json.Unmarshal(part.Result, &resultValue) != nil || resultValue == nil {
				continue
			}
			part.Text, part.CallID, part.Name, part.Args, part.Content = "", "", "", nil, ""
			part.AttachmentKey, part.URL, part.Path, part.SourcePath, part.MIME, part.Origin = "", "", "", "", "", ""
			part.AttachmentCreatedAt, part.AudioTranscript = "", ""
			part.Size = 0
			part.Surface, part.Resource, part.ResourceKind, part.RootID = "", "", "", ""
			part.StartLine, part.StartColumn, part.EndLine, part.EndColumn = 0, 0, 0, 0
			part.Ok = false
			part.SummaryKind, part.SummaryCount = "", 0
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

func AssistantOutputSegments(parts []ContentPart) []AssistantOutputSegment {
	parts = NormalizeContentParts(parts)
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

// EnsureProviderStateAssistantSegment adds a protocol-only assistant anchor
// when provider-native state follows output that contains no assistant segment
// (for example, a tool result followed by a state-only model response).
func EnsureProviderStateAssistantSegment(segments []AssistantOutputSegment, state *ProviderState) []AssistantOutputSegment {
	if !ValidProviderState(state) {
		return segments
	}
	for _, segment := range segments {
		if segment.Role == RoleAssistant {
			return segments
		}
	}
	return append(segments, AssistantOutputSegment{
		Role: RoleAssistant,
		Kind: MessageKindText,
	})
}

// IsProtocolOnlyMessage reports whether a message exists only to preserve
// provider-native continuation state. These messages remain canonical context
// but are omitted from the user-facing conversation turn projection.
func IsProtocolOnlyMessage(message *Message) bool {
	return message != nil &&
		message.Role == RoleAssistant &&
		strings.TrimSpace(message.Text) == "" &&
		len(message.Parts) == 0 &&
		ValidProviderState(message.ProviderState)
}

// MessagesThroughBoundary returns the canonical prefix represented by one
// visible message node. Protocol-only messages immediately following that node
// in the same turn belong to the node and must travel with it.
func MessagesThroughBoundary(messages []*Message, throughMessageID string) ([]*Message, bool) {
	throughMessageID = strings.TrimSpace(throughMessageID)
	for i, message := range messages {
		if message == nil || message.ID != throughMessageID {
			continue
		}
		end := i + 1
		for end < len(messages) && messages[end].TurnID == message.TurnID && IsProtocolOnlyMessage(messages[end]) {
			end++
		}
		return messages[:end], true
	}
	return nil, false
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
	return AssistantOutputSegments(parts)
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
	case ContentPartAttachment:
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
	Parts           []ContentPart     `json:"parts,omitempty"`
	Status          QueuedInputStatus `json:"status"`
	Provider        string            `json:"provider,omitempty"`
	Model           string            `json:"model,omitempty"`
	Mode            AgentMode         `json:"mode,omitempty"`
	ModelConfig     json.RawMessage   `json:"modelConfig,omitempty"`
	TurnID          string            `json:"turnID,omitempty"`
	CreatedAt       time.Time         `json:"createdAt"`
	UpdatedAt       time.Time         `json:"updatedAt"`
}

type QueueInputInput struct {
	SessionID       string
	ClientMessageID string
	Text            string
	Parts           []ContentPart
	Provider        string
	Model           string
	Mode            AgentMode
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

type SteerQueuedInputInput struct {
	SessionID       string
	TurnID          string
	ClientMessageID string
	UserMessageID   string
}

type SteerQueuedInputResult struct {
	Duplicate    bool
	Input        *QueuedInput
	UserMessage  *Message
	UpdatedEvent *event.Event
	SteeredEvent *event.Event
}

type ConversationTurn struct {
	ID              string              `json:"id"`
	SessionID       string              `json:"sessionID"`
	ClientMessageID string              `json:"clientMessageID"`
	Status          TurnStatus          `json:"status"`
	Provider        string              `json:"provider,omitempty"`
	Model           string              `json:"model,omitempty"`
	Mode            AgentMode           `json:"mode,omitempty"`
	Error           string              `json:"error,omitempty"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	Messages        []*Message          `json:"messages"`
	FileChanges     []*TurnFileChange   `json:"fileChanges,omitempty"`
	FileChangeState TurnFileChangeState `json:"fileChangeState,omitempty"`
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
	Mode        AgentMode       `json:"mode,omitempty"`
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
	UserParts       []ContentPart
	// Provider / Model 由 engine 在提交时刻解析后传入,随 turn 落库。
	Provider    string
	Model       string
	Mode        AgentMode
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

type BeginSystemTurnInput struct {
	SessionID       string
	TurnID          string
	SystemMessageID string
	ClientMessageID string
	Text            string
	// Provider / Model 由 engine 在提交时刻解析后传入,随 turn 落库。
	Provider    string
	Model       string
	Mode        AgentMode
	ModelConfig json.RawMessage
}

type BeginSystemTurnResult struct {
	Duplicate     bool
	Turn          *Turn
	SystemMessage *Message
	StartedEvent  *event.Event // 已分配 seq 并落库;Duplicate 时为 nil
}

type FinishTurnInput struct {
	TurnID string
	Status TurnStatus // completed | failed | cancelled
	Mode   AgentMode
	// AssistantParts 为空时:completed 落一个空 assistant message 保持事件可定位;
	// failed/cancelled 不落 assistant message。
	AssistantParts []ContentPart
	Interrupted    bool
	Error          string
	FileChanges    []TurnFileChangeInput
}

type FileChangeKind string
type FileChangeOrigin string
type TurnFileChangeState string

const (
	TurnFileChangesApplied TurnFileChangeState = "applied"
	TurnFileChangesUndone  TurnFileChangeState = "undone"
)

const (
	FileChangeAdded    FileChangeKind = "added"
	FileChangeModified FileChangeKind = "modified"
	FileChangeDeleted  FileChangeKind = "deleted"
	FileChangeRenamed  FileChangeKind = "renamed"

	FileChangeOriginStructured FileChangeOrigin = "structured"
	// FileChangeOriginCommandObserved marks changes observed around a foreground
	// command whose explicit write targets were statically identifiable.
	FileChangeOriginCommandObserved FileChangeOrigin = "command_observed"
)

func NormalizeFileChangeOrigin(origin FileChangeOrigin) FileChangeOrigin {
	if origin == FileChangeOriginCommandObserved {
		return origin
	}
	return FileChangeOriginStructured
}

type TurnFileChangeInput struct {
	RootPath        string
	Path            string
	OriginalPath    string
	Kind            FileChangeKind
	Origin          FileChangeOrigin
	Additions       int
	Deletions       int
	Binary          bool
	TooLarge        bool
	OldSize         int64
	NewSize         int64
	OldContent      string
	NewContent      string
	SnapshotVersion int
	OldDigest       string
	NewDigest       string
	OldMode         uint32
	NewMode         uint32
	OldType         string
	NewType         string
	OldBinary       bool
	NewBinary       bool
	OldData         []byte
	NewData         []byte
}

type TurnFileChange struct {
	ID              string           `json:"id"`
	SessionID       string           `json:"sessionID"`
	TurnID          string           `json:"turnID"`
	RootPath        string           `json:"rootPath"`
	Path            string           `json:"path"`
	OriginalPath    string           `json:"originalPath,omitempty"`
	Kind            FileChangeKind   `json:"kind"`
	Origin          FileChangeOrigin `json:"origin"`
	Additions       int              `json:"additions"`
	Deletions       int              `json:"deletions"`
	Binary          bool             `json:"binary"`
	TooLarge        bool             `json:"tooLarge"`
	OldSize         int64            `json:"oldSize"`
	NewSize         int64            `json:"newSize"`
	OldContent      string           `json:"oldContent,omitempty"`
	NewContent      string           `json:"newContent,omitempty"`
	SnapshotVersion int              `json:"snapshotVersion"`
	Reversible      bool             `json:"reversible"`
	OldDigest       string           `json:"-"`
	NewDigest       string           `json:"-"`
	OldMode         uint32           `json:"-"`
	NewMode         uint32           `json:"-"`
	OldType         string           `json:"-"`
	NewType         string           `json:"-"`
	OldBinary       bool             `json:"-"`
	NewBinary       bool             `json:"-"`
	OldData         []byte           `json:"-"`
	NewData         []byte           `json:"-"`
	CreatedAt       time.Time        `json:"createdAt"`
}

// MarkUnsafeTurnFileChangeLayouts disables whole-turn replay when one tracked
// file path is an ancestor of, or collides with, another endpoint. Those
// layouts represent file/directory transformations that cannot be restored by
// a regular-file-only transaction.
func MarkUnsafeTurnFileChangeLayouts(changes []*TurnFileChange) {
	type endpoint struct {
		path string
	}
	endpoints := make([]endpoint, 0, len(changes)*2)
	for _, change := range changes {
		if change == nil {
			continue
		}
		root := filepath.Clean(change.RootPath)
		endpoints = append(endpoints, endpoint{path: root + "\x00" + filepath.ToSlash(filepath.Clean(change.Path))})
		if change.Kind == FileChangeRenamed && strings.TrimSpace(change.OriginalPath) != "" {
			endpoints = append(endpoints, endpoint{path: root + "\x00" + filepath.ToSlash(filepath.Clean(change.OriginalPath))})
		}
	}
	unsafe := false
	for i := range endpoints {
		for j := i + 1; j < len(endpoints); j++ {
			left, right := endpoints[i].path, endpoints[j].path
			if left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/") {
				unsafe = true
				break
			}
		}
		if unsafe {
			break
		}
	}
	if unsafe {
		for _, change := range changes {
			if change != nil {
				change.Reversible = false
			}
		}
	}
}

type AppendTurnOutputInput struct {
	TurnID        string
	Parts         []ContentPart
	ProviderState *ProviderState
	Interrupted   bool
}

type AppendTurnOutputResult struct {
	Messages []*Message
}

type AppendTurnSteerInput struct {
	SessionID       string
	TurnID          string
	UserMessageID   string
	ClientMessageID string
	UserText        string
	UserParts       []ContentPart
}

type AppendTurnSteerResult struct {
	Duplicate   bool
	UserMessage *Message
	Event       *event.Event
}

type ApplyTurnSteersInput struct {
	TurnID     string
	MessageIDs []string
	// Events 与 MessageIDs 一一对应，在消息移动到安全采样边界时才分配
	// seq 并落库，避免尚未发布的事件提前占用 SSE 序号。
	Events []*event.Event
}

type AppendCompactSummaryInput struct {
	SessionID       string
	TurnID          string
	MessageID       string
	ClientMessageID string
	Provider        string
	Model           string
	Mode            AgentMode
	ModelConfig     json.RawMessage
	Text            string
	Metadata        json.RawMessage
}

type AppendCompactSummaryResult struct {
	Turn       *Turn
	Message    *Message
	FinalEvent *event.Event
}

type FinishTurnResult struct {
	AssistantMessage  *Message // failed/cancelled 无产出时为 nil;多 segment 时指向第一条输出消息
	AssistantMessages []*Message
	FinalEvent        *event.Event
}

type UsageRecordInput struct {
	OccurredAt            time.Time
	Provider              string
	Model                 string
	EstimatedInputTokens  int
	RequestCount          int
	InputUncachedTokens   int
	InputCachedTokens     int
	CacheCreationTokens   int
	OutputContentTokens   int
	OutputReasoningTokens int
}

type UsageHourlyStat struct {
	HourStartAt           time.Time `json:"hourStartAt"`
	Model                 string    `json:"model"`
	RequestCount          int       `json:"requestCount"`
	InputUncachedTokens   int       `json:"inputUncachedTokens"`
	InputCachedTokens     int       `json:"inputCachedTokens"`
	CacheCreationTokens   int       `json:"cacheCreationTokens"`
	OutputContentTokens   int       `json:"outputContentTokens"`
	OutputReasoningTokens int       `json:"outputReasoningTokens"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

func (s UsageHourlyStat) TotalTokens() int {
	return s.InputUncachedTokens + s.InputCachedTokens + s.CacheCreationTokens + s.OutputContentTokens + s.OutputReasoningTokens
}

type SessionUsageStat struct {
	SessionID                       string    `json:"sessionID"`
	RequestCount                    int       `json:"requestCount"`
	LastProvider                    string    `json:"lastProvider"`
	LastModel                       string    `json:"lastModel"`
	LastEstimatedInputTokens        int       `json:"lastEstimatedInputTokens"`
	LastInputUncachedTokens         int       `json:"lastInputUncachedTokens"`
	LastInputCachedTokens           int       `json:"lastInputCachedTokens"`
	LastCacheCreationTokens         int       `json:"lastCacheCreationTokens"`
	LastOutputContentTokens         int       `json:"lastOutputContentTokens"`
	LastOutputReasoningTokens       int       `json:"lastOutputReasoningTokens"`
	CumulativeInputUncachedTokens   int       `json:"cumulativeInputUncachedTokens"`
	CumulativeInputCachedTokens     int       `json:"cumulativeInputCachedTokens"`
	CumulativeCacheCreationTokens   int       `json:"cumulativeCacheCreationTokens"`
	CumulativeOutputContentTokens   int       `json:"cumulativeOutputContentTokens"`
	CumulativeOutputReasoningTokens int       `json:"cumulativeOutputReasoningTokens"`
	UpdatedAt                       time.Time `json:"updatedAt"`
}

func (s SessionUsageStat) LastInputTokens() int {
	return s.LastInputUncachedTokens + s.LastInputCachedTokens + s.LastCacheCreationTokens
}

func (s SessionUsageStat) LastOutputTokens() int {
	return s.LastOutputContentTokens + s.LastOutputReasoningTokens
}

func (s SessionUsageStat) CumulativeInputTokens() int {
	return s.CumulativeInputUncachedTokens + s.CumulativeInputCachedTokens + s.CumulativeCacheCreationTokens
}

func (s SessionUsageStat) CumulativeOutputTokens() int {
	return s.CumulativeOutputContentTokens + s.CumulativeOutputReasoningTokens
}

func (s SessionUsageStat) CumulativeTotalTokens() int {
	return s.CumulativeInputTokens() + s.CumulativeOutputTokens()
}

const DefaultCanvasID = "default"
const ClosedCanvasDefaultLimit = 20
const ClosedCanvasMaxLimit = 20
const ClosedCanvasKeepLimit = 20

type CanvasItem struct {
	ID                 string          `json:"id"`
	SessionID          string          `json:"sessionID"`
	CanvasID           string          `json:"canvasID"`
	SourceSessionID    string          `json:"sourceSessionID,omitempty"`
	CreatedBySessionID string          `json:"createdBySessionID,omitempty"`
	UpdatedBySessionID string          `json:"updatedBySessionID,omitempty"`
	Kind               string          `json:"kind"`
	Title              string          `json:"title,omitempty"`
	Item               json.RawMessage `json:"item"`
	Window             json.RawMessage `json:"window,omitempty"`
	SourceSavedItemID  string          `json:"sourceSavedItemID,omitempty"`
	BaseSavedRevision  int64           `json:"baseSavedRevision,omitempty"`
	SavedDirty         bool            `json:"savedDirty,omitempty"`
	Visible            bool            `json:"visible"`
	CreatedAt          time.Time       `json:"createdAt"`
	UpdatedAt          time.Time       `json:"updatedAt"`
}

type CanvasItemInput struct {
	ID                string
	CanvasID          string
	ActorSessionID    string
	SourceSessionID   string
	Kind              string
	Title             string
	Item              json.RawMessage
	Window            json.RawMessage
	SourceSavedItemID string
	BaseSavedRevision int64
}

type CanvasItemWindowPatch struct {
	CanvasID       string
	ActorSessionID string
	ItemID         string
	Window         json.RawMessage
}

type ClosedCanvasItem struct {
	ID             string          `json:"id"`
	SessionID      string          `json:"sessionID"`
	SourceItemID   string          `json:"sourceItemID"`
	ActorSessionID string          `json:"actorSessionID,omitempty"`
	Kind           string          `json:"kind"`
	Title          string          `json:"title,omitempty"`
	Item           json.RawMessage `json:"item"`
	Window         json.RawMessage `json:"window,omitempty"`
	ClosedAt       time.Time       `json:"closedAt"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

type SavedCanvasItem struct {
	ID              string          `json:"id"`
	SourceSessionID string          `json:"sourceSessionID,omitempty"`
	SourceItemID    string          `json:"sourceItemID,omitempty"`
	Kind            string          `json:"kind"`
	Title           string          `json:"title,omitempty"`
	Item            json.RawMessage `json:"item"`
	Window          json.RawMessage `json:"window,omitempty"`
	Revision        int64           `json:"revision"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type CanvasSaveResult struct {
	Item      *CanvasItem      `json:"item"`
	SavedItem *SavedCanvasItem `json:"savedItem"`
}

type ClosedCanvasItemInput struct {
	ID             string
	SourceItemID   string
	ActorSessionID string
	Kind           string
	Title          string
	Item           json.RawMessage
	Window         json.RawMessage
	ClosedAt       time.Time
}

type BrowserState struct {
	SessionID  string    `json:"sessionID"`
	TabID      string    `json:"tabID,omitempty"`
	URL        string    `json:"url,omitempty"`
	Title      string    `json:"title,omitempty"`
	FaviconURL string    `json:"faviconURL,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type BrowserStateInput struct {
	SessionID  string
	TabID      string
	URL        string
	Title      string
	FaviconURL string
	Mode       string
}

const (
	BrowserHistoryDefaultLimit = 20
	BrowserHistoryMaxLimit     = 100
	BrowserHistoryRetainLimit  = 1000
)

type BrowserHistoryEntry struct {
	ID         string    `json:"id"`
	URL        string    `json:"url"`
	Title      string    `json:"title,omitempty"`
	FaviconURL string    `json:"faviconURL,omitempty"`
	VisitedAt  time.Time `json:"visitedAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type BrowserHistoryInput struct {
	URL        string
	Title      string
	FaviconURL string
	VisitedAt  time.Time
}

func NormalizeCanvasItemInput(in *CanvasItemInput) error {
	if in == nil {
		return ErrInvalidCanvas
	}
	in.ID = strings.TrimSpace(in.ID)
	in.CanvasID = normalizeCanvasID(in.CanvasID)
	in.ActorSessionID = strings.TrimSpace(in.ActorSessionID)
	in.SourceSessionID = strings.TrimSpace(in.SourceSessionID)
	in.SourceSavedItemID = strings.TrimSpace(in.SourceSavedItemID)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Title = strings.TrimSpace(in.Title)
	if in.ID == "" || in.ActorSessionID == "" || in.Kind == "" || in.BaseSavedRevision < 0 || len(in.Item) == 0 || !json.Valid(in.Item) {
		return ErrInvalidCanvas
	}
	if len(in.Window) > 0 && !json.Valid(in.Window) {
		return ErrInvalidCanvas
	}
	in.Item = append(json.RawMessage(nil), in.Item...)
	in.Window = append(json.RawMessage(nil), in.Window...)
	return nil
}

func NormalizeCanvasItemWindowPatch(patch *CanvasItemWindowPatch) error {
	if patch == nil {
		return ErrInvalidCanvas
	}
	patch.CanvasID = normalizeCanvasID(patch.CanvasID)
	patch.ActorSessionID = strings.TrimSpace(patch.ActorSessionID)
	patch.ItemID = strings.TrimSpace(patch.ItemID)
	if patch.ActorSessionID == "" || patch.ItemID == "" || len(patch.Window) == 0 || !json.Valid(patch.Window) {
		return ErrInvalidCanvas
	}
	patch.Window = append(json.RawMessage(nil), patch.Window...)
	return nil
}

func NormalizeClosedCanvasItemInput(in *ClosedCanvasItemInput) error {
	if in == nil {
		return ErrInvalidCanvas
	}
	in.ID = strings.TrimSpace(in.ID)
	in.SourceItemID = strings.TrimSpace(in.SourceItemID)
	in.ActorSessionID = strings.TrimSpace(in.ActorSessionID)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Title = strings.TrimSpace(in.Title)
	if in.ID == "" || in.SourceItemID == "" || in.ActorSessionID == "" || in.Kind == "" || len(in.Item) == 0 || !json.Valid(in.Item) {
		return ErrInvalidCanvas
	}
	if len(in.Window) > 0 && !json.Valid(in.Window) {
		return ErrInvalidCanvas
	}
	if in.ClosedAt.IsZero() {
		in.ClosedAt = time.Now()
	}
	in.Item = append(json.RawMessage(nil), in.Item...)
	in.Window = append(json.RawMessage(nil), in.Window...)
	return nil
}

func NormalizeBrowserStateInput(in *BrowserStateInput) error {
	if in == nil {
		return ErrInvalidBrowserState
	}
	in.SessionID = strings.TrimSpace(in.SessionID)
	in.TabID = strings.TrimSpace(in.TabID)
	in.URL = strings.TrimSpace(in.URL)
	in.Title = strings.TrimSpace(in.Title)
	in.FaviconURL = strings.TrimSpace(in.FaviconURL)
	in.Mode = strings.TrimSpace(in.Mode)
	if in.SessionID == "" || in.TabID == "" || in.URL == "" {
		return ErrInvalidBrowserState
	}
	return nil
}

func NormalizeBrowserHistoryInput(in *BrowserHistoryInput) error {
	if in == nil {
		return ErrInvalidBrowserHistory
	}
	in.URL = strings.TrimSpace(in.URL)
	in.Title = strings.TrimSpace(in.Title)
	in.FaviconURL = strings.TrimSpace(in.FaviconURL)
	if in.URL == "" {
		return ErrInvalidBrowserHistory
	}
	if in.VisitedAt.IsZero() {
		in.VisitedAt = time.Now().UTC()
	}
	return nil
}

func NormalizeBrowserHistoryLimit(limit int) int {
	if limit <= 0 {
		return BrowserHistoryDefaultLimit
	}
	if limit > BrowserHistoryMaxLimit {
		return BrowserHistoryMaxLimit
	}
	return limit
}

func normalizeCanvasID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return DefaultCanvasID
	}
	return id
}

// Store 的每个方法是一个完整事务。BeginTurn 与 FinishTurn 内部必须把
// message、turns 状态、lifecycle event 写在同一事务里(AGENTS.md 硬约束 15);
// 事件 seq 由 Store 在事务内按 session 单调分配。
// SQLite 实现要求 WAL + 单 writer;schema 契约见 schema.sql。
type Store interface {
	CreateProject(ctx context.Context, p *Project) error
	GetProject(ctx context.Context, id string) (*Project, error)
	ListProjects(ctx context.Context) ([]*Project, error)
	UpdateProject(ctx context.Context, id string, upd ProjectUpdate) (*Project, error)
	DeleteProject(ctx context.Context, id string) error

	CreateSession(ctx context.Context, s *Session) error
	CloneSession(ctx context.Context, in CloneSessionInput) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	// ListSessions 默认只返回 active session；内部维护任务必须显式使用 all。
	ListSessions(ctx context.Context, options ...SessionListOptions) ([]*Session, error)
	UpdateSession(ctx context.Context, id string, upd SessionUpdate) (*Session, error)
	ArchiveSession(ctx context.Context, id string) (*Session, error)
	RestoreSession(ctx context.Context, id string) (*Session, error)
	ListExpiredArchivedSessionIDs(ctx context.Context, cutoff time.Time) ([]string, error)
	DeleteSession(ctx context.Context, id string) error
	HasComputerAppGrant(ctx context.Context, sessionID, appID string) (bool, error)
	GrantComputerApp(ctx context.Context, sessionID, appID string) error

	// BeginTurn:幂等检查(clientMessageID 重复则返回 Duplicate)→ 校验无
	// running turn(否则 ErrTurnRunning)→ 落 user message + running turn
	// + turn.started 事件。
	BeginTurn(ctx context.Context, in BeginTurnInput) (*BeginTurnResult, error)
	// BeginSystemTurn 创建无 user message 的 running turn。用于 /summary
	// 这类 system reminder:触发模型回复,但不在 transcript 里冒用户气泡。
	BeginSystemTurn(ctx context.Context, in BeginSystemTurnInput) (*BeginSystemTurnResult, error)
	// QueueInput 持久化等待发送的用户输入。Duplicate 表示同一
	// clientMessageID 已存在于 queued_inputs 或 turns,不重复写入。
	QueueInput(ctx context.Context, in QueueInputInput) (*QueueInputResult, error)
	ListQueuedInputs(ctx context.Context, sessionID string) ([]*QueuedInput, error)
	HasQueuedInputs(ctx context.Context, sessionID string) (bool, error)
	UpdateQueuedInput(ctx context.Context, in UpdateQueuedInputInput) (*UpdateQueuedInputResult, error)
	PromoteNextQueuedInput(ctx context.Context, in PromoteQueuedInputInput) (*PromoteQueuedInputResult, error)
	// SteerQueuedInput 把一条仍在排队的输入原子地提升为当前 running turn
	// 的引导消息，并持久化队列更新；input.steered 由 ApplyTurnSteers
	// 在安全采样边界落库。
	SteerQueuedInput(ctx context.Context, in SteerQueuedInputInput) (*SteerQueuedInputResult, error)
	QueuedSessions(ctx context.Context) ([]string, error)
	// AppendTurnOutput 在 turn 仍 running 时追加已经完整的 assistant/tool
	// output message;token delta 不走这里。
	AppendTurnOutput(ctx context.Context, in AppendTurnOutputInput) (*AppendTurnOutputResult, error)
	// AppendTurnSteer 在指定 turn 仍 running 时追加用户引导。
	// input.steered 在 ApplyTurnSteers 的安全采样边界落库。
	AppendTurnSteer(ctx context.Context, in AppendTurnSteerInput) (*AppendTurnSteerResult, error)
	// ApplyTurnSteers 把流式期间已接收的引导移动到当前完整输出之后，
	// 并在同一事务中持久化对应的 input.steered 事件。
	ApplyTurnSteers(ctx context.Context, in ApplyTurnSteersInput) error
	// AppendCompactSummary 追加一条 completed summary turn,作为后续上下文压缩边界。
	AppendCompactSummary(ctx context.Context, in AppendCompactSummaryInput) (*AppendCompactSummaryResult, error)
	// FinishTurn:更新 turn 状态 + 落 assistant message(如有)+ 落 final 事件。
	FinishTurn(ctx context.Context, in FinishTurnInput) (*FinishTurnResult, error)
	// RecordUsage 把一次或一批 provider usage delta 累加进全局 UTC 小时桶。
	RecordUsage(ctx context.Context, in UsageRecordInput) (*UsageHourlyStat, error)
	// UsageHourlyStats 返回 [from, to) 的全局小时统计;to 为零值表示无上界。
	UsageHourlyStats(ctx context.Context, from, to time.Time) ([]*UsageHourlyStat, error)
	// RecordSessionUsage 把一次 provider request 的原始预估与实测用量写入
	// session 统计；二者必须来自同一个 provider payload。
	RecordSessionUsage(ctx context.Context, sessionID string, in UsageRecordInput) (*SessionUsageStat, error)
	// SessionUsage 返回 session 最近一次与累计 token 用量;无用量时返回零统计。
	SessionUsage(ctx context.Context, sessionID string) (*SessionUsageStat, error)
	// RunningTurn 返回 session 当前 running 的 turn,无则 ErrNotFound。
	RunningTurn(ctx context.Context, sessionID string) (*Turn, error)
	// RunningTurns 返回所有 session 的 running turn,服务 daemon 启动恢复。
	RunningTurns(ctx context.Context) ([]*Turn, error)

	// ListMessages 按时间升序返回最近 limit 条;limit <= 0 表示全部。
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*Message, error)
	// ListMessagesPage 按时间升序返回一页 messages。beforeMessageID 为空时返回
	// 最近 limit 条;非空时返回该 message 之前的 limit 条。limit <= 0 表示不分页。
	ListMessagesPage(ctx context.Context, sessionID string, beforeMessageID string, limit int) (*MessagePage, error)
	// GetMessage 按 id 读取单条 canonical message;必须显式带 sessionID。
	GetMessage(ctx context.Context, sessionID string, messageID string) (*Message, error)
	// SearchMessages 在单个 session 的 canonical message text 上做全文检索。
	// SQLite 正式实现走 FTS5;未启用 FTS5 时返回 ErrHistorySearchUnavailable。
	SearchMessages(ctx context.Context, in MessageSearchInput) ([]*Message, error)
	// RemoveAttachmentsByOrigin 从指定 session 的 canonical messages 和 queued inputs 中
	// 移除指定 origin 的附件 parts,返回被移除的附件供调用方清理 blob 文件。
	// 不会改动用户文本。
	RemoveAttachmentsByOrigin(ctx context.Context, sessionID, origin string) (*AttachmentCleanupResult, error)
	// ListTurnsPage 按 turn 创建时间升序返回一页完整 turn。beforeTurnID 为空时
	// 返回最近 limit 个 turn;非空时返回该 turn 之前的 limit 个 turn。
	ListTurnsPage(ctx context.Context, sessionID string, beforeTurnID string, limit int) (*TurnPage, error)
	// GetConversationTurn 返回单个完整 turn,用于 lifecycle 终态后精确对账。
	GetConversationTurn(ctx context.Context, sessionID string, turnID string) (*ConversationTurn, error)
	// GetTurnFileChange returns one historical file diff and always verifies the
	// explicit session and turn scope before exposing snapshot content.
	GetTurnFileChange(ctx context.Context, sessionID string, turnID string, changeID string) (*TurnFileChange, error)
	UpdateTurnFileChangeState(ctx context.Context, sessionID string, turnID string, expected, next TurnFileChangeState) error
	// EventsAfter 返回 seq > afterSeq 的 lifecycle 事件,按 seq 升序,
	// 承载 SSE Last-Event-ID 续传;limit <= 0 表示全部。
	EventsAfter(ctx context.Context, sessionID string, afterSeq int64, limit int) ([]event.Event, error)
	// LatestSeq 返回 session 当前最大事件 seq(无事件为 0),
	// 服务无续传位点的全新 SSE 连接从尾部开始(tail)。
	LatestSeq(ctx context.Context, sessionID string) (int64, error)

	ListCanvasItems(ctx context.Context, actorSessionID string) ([]*CanvasItem, error)
	PutCanvasItem(ctx context.Context, in CanvasItemInput) (*CanvasItem, error)
	UpdateCanvasItemWindow(ctx context.Context, patch CanvasItemWindowPatch) (*CanvasItem, error)
	DeleteCanvasItem(ctx context.Context, actorSessionID, itemID string) error
	ListSavedCanvasItems(ctx context.Context, actorSessionID string) ([]*SavedCanvasItem, error)
	SaveCanvasItem(ctx context.Context, actorSessionID, itemID, savedItemID string) (*CanvasSaveResult, error)
	OpenSavedCanvasItem(ctx context.Context, actorSessionID, savedItemID, itemID string) (*CanvasItem, error)
	DeleteSavedCanvasItem(ctx context.Context, actorSessionID, savedItemID string) error
	ListClosedCanvasItems(ctx context.Context, actorSessionID string, limit int) ([]*ClosedCanvasItem, error)
	PutClosedCanvasItem(ctx context.Context, in ClosedCanvasItemInput, keepLimit int) (*ClosedCanvasItem, error)
	DeleteClosedCanvasItem(ctx context.Context, actorSessionID, id string) error
	ClearClosedCanvasItems(ctx context.Context, actorSessionID string) error

	GetBrowserState(ctx context.Context, sessionID string) (*BrowserState, error)
	GetBrowserTabState(ctx context.Context, sessionID, tabID string) (*BrowserState, error)
	ListBrowserStates(ctx context.Context, sessionID string) ([]*BrowserState, error)
	PutBrowserState(ctx context.Context, in BrowserStateInput) (*BrowserState, error)
	DeleteBrowserState(ctx context.Context, sessionID, tabID string) error
	ClearBrowserState(ctx context.Context, sessionID string) error
	ListBrowserHistory(ctx context.Context, query string, limit int) ([]*BrowserHistoryEntry, error)
	PutBrowserHistory(ctx context.Context, in BrowserHistoryInput) (*BrowserHistoryEntry, error)
	UpdateBrowserHistoryMetadata(ctx context.Context, in BrowserHistoryInput) error
	DeleteBrowserHistory(ctx context.Context, historyID string) error
	ClearBrowserHistory(ctx context.Context) error

	Close() error
}

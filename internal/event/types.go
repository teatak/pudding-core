// Package event 定义 session-scoped 事件协议(docs/technology-decisions.md 第 8 节)。
// 本文件是事件契约的 Go 侧唯一来源;web 侧镜像在 web/contracts/events.ts,
// 字段名必须一一对应,对照表见 docs/contracts-checklist.md。
package event

import "encoding/json"

type Kind string

const (
	TurnStarted       Kind = "turn.started"
	TurnDelta         Kind = "turn.delta"
	TurnTool          Kind = "turn.tool"
	TurnCompleted     Kind = "turn.completed"
	TurnFailed        Kind = "turn.failed"
	TurnCancelled     Kind = "turn.cancelled"
	InputQueued       Kind = "input.queued"
	InputUpdated      Kind = "input.updated"
	AudioBindings     Kind = "audio.bindings"
	AudioInputLevel   Kind = "audio.input_level"
	ApprovalRequested Kind = "approval.requested"
	ApprovalResolved  Kind = "approval.resolved"
	ProcessStarted    Kind = "process.started"
	ProcessFinished   Kind = "process.finished"
	ProcessStopped    Kind = "process.stopped"
	ProcessRemoved    Kind = "process.removed"
	// SessionTitled:自动标题写回(provisional 与 LLM 正式标题各发一次),
	// 前端据此刷新会话列表。不落库:标题事实源是 sessions 表,丢事件由
	// sessions 轮询兜底。
	SessionTitled Kind = "session.titled"
	Ping          Kind = "ping"
)

// Event 的字段按 Kind 选填:
//
//	turn.started   seq, turnID, clientMessageID, userMessageID, text
//	turn.delta     turnID, part, delta      (不落库,无 seq)
//	turn.tool      turnID, callID, name, phase, argsDelta/stream/content/ok/summaryKind/summaryCount/attachments (不落库,无 seq)
//	turn.completed seq, turnID, assistantMessageID
//	turn.failed    seq, turnID, error       (有部分输出时附 assistantMessageID + interrupted)
//	turn.cancelled seq, turnID              (有部分输出时附 assistantMessageID + interrupted)
//	input.queued   seq, clientMessageID, text, status
//	input.updated  seq, clientMessageID, text, status
//	audio.bindings inputOwner, inputMode, outputOwner, inputLevel
//	audio.input_level inputLevel
//	approval.requested turnID, callID, approvalID, approvalKind, title, reason, risk, payload
//	approval.resolved  turnID, callID, approvalID, approvalKind, status
//	process.*         turnID, callID, payload(background process snapshot)
//	ping           —                        (心跳,不落库,无 seq)
type Event struct {
	// Seq 是 per-session 单调递增序号,仅落库的 lifecycle 事件持有(>0),
	// 同时作为 SSE 的 id 字段承载 Last-Event-ID 续传。
	Seq                int64           `json:"seq,omitempty"`
	SessionID          string          `json:"sessionID"`
	Kind               Kind            `json:"kind"`
	TurnID             string          `json:"turnID,omitempty"`
	ClientMessageID    string          `json:"clientMessageID,omitempty"`
	UserMessageID      string          `json:"userMessageID,omitempty"`
	Part               string          `json:"part,omitempty"` // turn.delta:text|thought
	Delta              string          `json:"delta,omitempty"`
	Text               string          `json:"text,omitempty"`
	Status             string          `json:"status,omitempty"`
	InputOwner         string          `json:"inputOwner,omitempty"`
	InputMode          string          `json:"inputMode,omitempty"`
	OutputOwner        string          `json:"outputOwner,omitempty"`
	InputLevel         *float64        `json:"inputLevel,omitempty"`
	CallID             string          `json:"callID,omitempty"` // turn.tool 专用
	Name               string          `json:"name,omitempty"`
	Phase              string          `json:"phase,omitempty"`
	ArgsDelta          string          `json:"argsDelta,omitempty"`
	Stream             string          `json:"stream,omitempty"`
	Ok                 *bool           `json:"ok,omitempty"`
	Content            string          `json:"content,omitempty"`
	SummaryKind        string          `json:"summaryKind,omitempty"`
	SummaryCount       int             `json:"summaryCount,omitempty"`
	Attachments        []Attachment    `json:"attachments,omitempty"`
	ApprovalID         string          `json:"approvalID,omitempty"`
	ApprovalKind       string          `json:"approvalKind,omitempty"`
	Title              string          `json:"title,omitempty"`
	Reason             string          `json:"reason,omitempty"`
	Risk               string          `json:"risk,omitempty"`
	Payload            json.RawMessage `json:"payload,omitempty"`
	AssistantMessageID string          `json:"assistantMessageID,omitempty"`
	Interrupted        bool            `json:"interrupted,omitempty"`
	Error              string          `json:"error,omitempty"`
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

// Persistent 报告该事件是否属于落库的 lifecycle 事件;
// turn.delta、session.titled 与 ping 只走 SSE 不落 events 表。
func (e Event) Persistent() bool {
	return e.Kind != TurnDelta &&
		e.Kind != TurnTool &&
		e.Kind != ApprovalRequested &&
		e.Kind != ApprovalResolved &&
		e.Kind != ProcessStarted &&
		e.Kind != ProcessFinished &&
		e.Kind != ProcessStopped &&
		e.Kind != ProcessRemoved &&
		e.Kind != AudioBindings &&
		e.Kind != AudioInputLevel &&
		e.Kind != Ping &&
		e.Kind != SessionTitled
}

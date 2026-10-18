package event

import (
	"strings"
	"sync"
	"time"
)

// Hub 把事件按 session 扇出给所有订阅者;daemon 不跟踪"哪个客户端在看",
// 同一 session 多客户端订阅是显式支持的能力。
//
// 投递是非阻塞的:订阅者缓冲打满时 Hub 摘除并关闭它的 channel,不能丢掉事件后继续投递。
// 继续投递会让后续事件推进 SSE 的 Last-Event-ID,被丢的持久事件再也补不回来;关闭后
// SSE 断流,客户端带 Last-Event-ID 重连补齐落库事件,未落库的 delta 由 turn 收尾后的
// refetch 兜底。内部订阅者遇到关闭需要重新订阅。
type Hub struct {
	mu          sync.Mutex
	subs        map[string]map[chan Event]struct{}
	noteStreams map[string]*noteArgumentStream
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan Event]struct{}), noteStreams: make(map[string]*noteArgumentStream)}
}

const subscriberBuffer = 64

// Subscribe 返回该 session 的事件 channel 与取消函数;取消或缓冲打满后 channel 关闭。
func (h *Hub) Subscribe(sessionID string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	set, ok := h.subs[sessionID]
	if !ok {
		set = make(map[chan Event]struct{})
		h.subs[sessionID] = set
	}
	set[ch] = struct{}{}
	h.pruneNoteStreams()
	for _, stream := range h.noteStreams {
		if stream.event.SessionID == sessionID && !stream.overflow {
			snapshot := stream.event
			snapshot.ArgsDelta = ""
			snapshot.ArgsText = stream.arguments.String()
			ch <- snapshot
		}
	}
	h.mu.Unlock()

	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.remove(sessionID, ch)
	}
	return ch, cancel
}

func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.captureNoteStream(ev)
	for ch := range h.subs[ev.SessionID] {
		select {
		case ch <- ev:
		default:
			h.remove(ev.SessionID, ch)
		}
	}
}

// remove 须持锁调用;已被摘除的订阅者不再重复关闭。
func (h *Hub) remove(sessionID string, ch chan Event) {
	set := h.subs[sessionID]
	if _, ok := set[ch]; !ok {
		return
	}
	delete(set, ch)
	if len(set) == 0 {
		delete(h.subs, sessionID)
	}
	close(ch)
}

// Only an in-progress note commit needs replay before a complete canonical tool
// call exists. This is transient view state, not document content or history.
// Bound both memory and age; no polling goroutine and no per-token persistence.
type noteArgumentStream struct {
	event     Event
	arguments strings.Builder
	updated   time.Time
	overflow  bool
}

const maxNoteStreams = 32
const maxNoteArgumentBytes = 256 * 1024

func (h *Hub) pruneNoteStreams() {
	for key, stream := range h.noteStreams {
		if time.Since(stream.updated) > 2*time.Minute {
			delete(h.noteStreams, key)
		}
	}
}
func (h *Hub) captureNoteStream(ev Event) {
	if ev.Kind == TurnCompleted || ev.Kind == TurnFailed || ev.Kind == TurnCancelled {
		for key, stream := range h.noteStreams {
			if stream.event.SessionID == ev.SessionID && stream.event.TurnID == ev.TurnID {
				delete(h.noteStreams, key)
			}
		}
		return
	}
	if ev.Kind != TurnTool {
		return
	}
	key := ev.SessionID + "\x00" + ev.TurnID + "\x00" + ev.CallID
	if ev.Phase != "streaming_args" {
		delete(h.noteStreams, key)
		return
	}
	stream := h.noteStreams[key]
	if stream == nil {
		if ev.Name != "builtin_page_edit" && ev.Name != "builtin_note_edit" && ev.Name != "builtin_doc_edit" {
			return
		}
		h.pruneNoteStreams()
		if len(h.noteStreams) >= maxNoteStreams {
			oldest := ""
			var at time.Time
			for k, s := range h.noteStreams {
				if oldest == "" || s.updated.Before(at) {
					oldest, at = k, s.updated
				}
			}
			delete(h.noteStreams, oldest)
		}
		stream = &noteArgumentStream{event: Event{Kind: ev.Kind, SessionID: ev.SessionID, TurnID: ev.TurnID, CallID: ev.CallID, Name: ev.Name, Phase: ev.Phase}}
		h.noteStreams[key] = stream
	}
	stream.updated = time.Now()
	if stream.overflow {
		return
	}
	if stream.arguments.Len()+len(ev.ArgsDelta) > maxNoteArgumentBytes {
		stream.overflow = true
		stream.arguments.Reset()
		return
	}
	stream.arguments.WriteString(ev.ArgsDelta)
}

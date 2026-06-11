package event

import "sync"

// Hub 把事件按 session 扇出给所有订阅者;daemon 不跟踪"哪个客户端在看",
// 同一 session 多客户端订阅是显式支持的能力。
//
// 投递是非阻塞的:订阅者缓冲打满直接丢弃。丢失的 lifecycle 事件由 SSE 的
// Last-Event-ID 续传补齐,丢失的 delta 由 turn.completed 后 refetch 兜底,
// 因此 Hub 不需要可靠投递语义。
type Hub struct {
	mu   sync.Mutex
	subs map[string]map[chan Event]struct{}
}

func NewHub() *Hub {
	return &Hub{subs: make(map[string]map[chan Event]struct{})}
}

const subscriberBuffer = 64

// Subscribe 返回该 session 的事件 channel 与取消函数;取消后 channel 关闭。
func (h *Hub) Subscribe(sessionID string) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	set, ok := h.subs[sessionID]
	if !ok {
		set = make(map[chan Event]struct{})
		h.subs[sessionID] = set
	}
	set[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs[sessionID], ch)
			if len(h.subs[sessionID]) == 0 {
				delete(h.subs, sessionID)
			}
			h.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[ev.SessionID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

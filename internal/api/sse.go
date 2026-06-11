package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/event"
)

const pingInterval = 15 * time.Second

// sessionEvents 实现 /sessions/{id}/events 的 SSE 流
// (docs/technology-decisions.md 第 8 节):
//   - lifecycle 事件带 seq 作为 SSE id,承载 Last-Event-ID 续传;
//   - 断线重连先从 events 表补发缺口,再接 live 流;
//   - turn.delta 只在 live 流里出现,丢失由 turn.completed 后 refetch 兜底。
func (s *Server) sessionEvents(c *cart.Context) error {
	id, _ := c.Param("id")
	if _, err := s.store.GetSession(c.Request.Context(), id); err != nil {
		return s.fail(c, err)
	}
	after := lastEventID(c.Request)

	w := c.Response
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// 先订阅再补发,避免补发窗口与 live 流之间漏事件;
	// 重叠部分用 seq 去重(lastSent 单调推进)。
	live, unsub := s.hub.Subscribe(id)
	defer unsub()

	backlog, err := s.store.EventsAfter(c.Request.Context(), id, after, 0)
	if err != nil {
		return nil // 头已发出,只能断流
	}
	var lastSent int64 = after
	for _, ev := range backlog {
		writeSSE(w, ev)
		lastSent = ev.Seq
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()
	done := c.Request.Context().Done()
	for {
		select {
		case <-done:
			return nil
		case <-ping.C:
			writeSSE(w, event.Event{SessionID: id, Kind: event.Ping})
		case ev, ok := <-live:
			if !ok {
				return nil
			}
			if ev.Seq > 0 && ev.Seq <= lastSent {
				continue
			}
			writeSSE(w, ev)
			if ev.Seq > 0 {
				lastSent = ev.Seq
			}
		}
	}
}

type flushWriter interface {
	http.ResponseWriter
	Flush()
}

func writeSSE(w flushWriter, ev event.Event) {
	if ev.Seq > 0 {
		fmt.Fprintf(w, "id: %d\n", ev.Seq)
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Kind, data)
	w.Flush()
}

// lastEventID 解析续传起点:标准 Last-Event-ID header 优先,
// ?after= 供手动调试。
func lastEventID(r *http.Request) int64 {
	v := r.Header.Get("Last-Event-ID")
	if v == "" {
		v = r.URL.Query().Get("after")
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

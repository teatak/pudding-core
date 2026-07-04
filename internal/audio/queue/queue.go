// Package queue provides a tiny in-memory playback queue.
package queue

import (
	"context"
	"strings"
	"sync"
)

type Item struct {
	SessionID string
	TurnID    string
	SegmentID string
	Text      string
}

type Queue struct {
	mu     sync.Mutex
	items  []Item
	notify chan struct{}
	closed bool
}

func New() *Queue {
	return &Queue{notify: make(chan struct{})}
}

func (q *Queue) Enqueue(item Item) bool {
	if strings.TrimSpace(item.Text) == "" {
		return false
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return false
	}
	q.items = append(q.items, item)
	q.wakeLocked()
	return true
}

func (q *Queue) Next(ctx context.Context) (Item, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			item := q.items[0]
			copy(q.items, q.items[1:])
			q.items = q.items[:len(q.items)-1]
			q.mu.Unlock()
			return item, true
		}
		if q.closed {
			q.mu.Unlock()
			return Item{}, false
		}
		notify := q.notify
		q.mu.Unlock()

		select {
		case <-ctx.Done():
			return Item{}, false
		case <-notify:
		}
	}
}

func (q *Queue) ClearTurn(turnID string) int {
	turnID = strings.TrimSpace(turnID)
	if turnID == "" {
		return 0
	}
	return q.filter(func(item Item) bool { return item.TurnID != turnID })
}

func (q *Queue) ClearSession(sessionID string) int {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0
	}
	return q.filter(func(item Item) bool { return item.SessionID != sessionID })
}

func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

func (q *Queue) Close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.closed = true
	q.items = nil
	q.wakeLocked()
}

func (q *Queue) filter(keep func(Item) bool) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	removed := 0
	out := q.items[:0]
	for _, item := range q.items {
		if keep(item) {
			out = append(out, item)
			continue
		}
		removed++
	}
	q.items = out
	if removed > 0 {
		q.wakeLocked()
	}
	return removed
}

func (q *Queue) wakeLocked() {
	close(q.notify)
	q.notify = make(chan struct{})
}

package lsp

import "sync"

type byteRing struct {
	mu    sync.Mutex
	limit int
	data  []byte
}

func newByteRing(limit int) *byteRing {
	return &byteRing{limit: limit}
}

func (r *byteRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	if r.limit <= 0 {
		return n, nil
	}
	if len(p) >= r.limit {
		r.data = append(r.data[:0], p[len(p)-r.limit:]...)
		return n, nil
	}
	overflow := len(r.data) + len(p) - r.limit
	if overflow > 0 {
		copy(r.data, r.data[overflow:])
		r.data = r.data[:len(r.data)-overflow]
	}
	r.data = append(r.data, p...)
	return n, nil
}

func (r *byteRing) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return string(append([]byte(nil), r.data...))
}

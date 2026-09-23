package api

import "context"

// lockSessionLifecycle serializes admission changes and cleanup for one
// session group. In particular, restore cannot reopen admission while a
// cancelled foreground tool is still being joined by archive or deletion.
func (s *Server) lockSessionLifecycle(ctx context.Context, id string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := s.store.ParentSessionID(ctx, id)
	if err != nil {
		return nil, err
	}
	if owner != "" {
		id = owner
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		s.sessionLifecycleMu.Lock()
		active := s.sessionLifecycles[id]
		if active == nil {
			active = make(chan struct{})
			if s.sessionLifecycles == nil {
				s.sessionLifecycles = make(map[string]chan struct{})
			}
			s.sessionLifecycles[id] = active
			s.sessionLifecycleMu.Unlock()
			return func() {
				s.sessionLifecycleMu.Lock()
				delete(s.sessionLifecycles, id)
				close(active)
				s.sessionLifecycleMu.Unlock()
			}, nil
		}
		s.sessionLifecycleMu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-active:
		}
	}
}

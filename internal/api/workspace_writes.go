package api

import (
	"context"
	"errors"
	"time"
)

const defaultWorkspaceWriteWait = 2 * time.Second

var errWorkspaceBusy = errors.New("workspace busy")

func (s *Server) acquireWorkspaceWrite(ctx context.Context, paths []string) (func(), error) {
	// Bound only the wait. Once admitted, the mutation uses the request context.
	wait, cancel := context.WithTimeoutCause(ctx, s.workspaceWriteWait, errWorkspaceBusy)
	defer cancel()
	release, err := s.engine.AcquireWorkspaceWrite(wait, paths)
	if errors.Is(err, context.DeadlineExceeded) && context.Cause(wait) == errWorkspaceBusy {
		return nil, errWorkspaceBusy
	}
	return release, err
}

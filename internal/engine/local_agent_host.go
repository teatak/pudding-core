package engine

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"sync"

	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

// A secret authorizes one owned ACP connection, never the daemon's general API.
// Its active turn is replaced only after the previous prompt and tools finish.
type localAgentHost struct {
	sessionID string
	name      string
	turn      *LocalHostTurn // protected by Engine.localMu
}
type LocalHostTurn struct {
	SessionID string
	TurnID    string
	Context   context.Context
	mu        *sync.Mutex // serializes canonical output with ACP updates
	active    *activeTurn
	state     externalTurn
	parts     *turnPartAccumulator
	wg        sync.WaitGroup
}

func (e *Engine) SetLocalHostEndpoint(endpoint string) { e.localHostEndpoint = endpoint }
func (e *Engine) newLocalHost(id string) (string, []acp.MCPServer, error) {
	if e.localHostEndpoint == "" {
		return "", nil, nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	secret := rand.Text()
	name := store.NewID("pudding_host")
	e.localMu.Lock()
	if e.localHosts == nil {
		e.localHosts = map[string]*localAgentHost{}
	}
	e.localHosts[secret] = &localAgentHost{sessionID: id, name: name}
	e.localMu.Unlock()
	return secret, []acp.MCPServer{{Name: name, Command: executable, Args: []string{"mcp", "--host"}, Env: []acp.EnvVar{{Name: "PUDDING_MCP_HOST_URL", Value: e.localHostEndpoint}, {Name: "PUDDING_MCP_HOST_TOKEN", Value: secret}}}}, nil
}
func (e *Engine) revokeLocalHost(secret string) {
	e.localMu.Lock()
	delete(e.localHosts, secret)
	e.localMu.Unlock()
}
func (e *Engine) HasLocalHost(secret string) bool {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	return secret != "" && e.localHosts[secret] != nil
}
func (e *Engine) startLocalHost(secret string, turn *LocalHostTurn) {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	if host := e.localHosts[secret]; host != nil {
		host.turn = turn
	}
}
func (e *Engine) finishLocalHost(secret string, turn *LocalHostTurn) {
	e.localMu.Lock()
	if host := e.localHosts[secret]; host != nil && host.turn == turn {
		host.turn = nil
	}
	e.localMu.Unlock()
	// No new acquisitions can enter this turn after the pointer is cleared.
	turn.wg.Wait()
}
func (e *Engine) AcquireLocalHost(secret string) (*LocalHostTurn, func(), error) {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	host := e.localHosts[secret]
	if host == nil {
		return nil, nil, errors.New("ACP host connection is closed")
	}
	turn := host.turn
	if turn == nil || turn.Context.Err() != nil {
		return nil, nil, ErrTurnNotActive
	}
	turn.wg.Add(1)
	return turn, turn.wg.Done, nil
}
func (e *Engine) LocalHostMode(turn *LocalHostTurn, sessionMode store.AgentMode) store.AgentMode {
	e.mu.Lock()
	defer e.mu.Unlock()
	return turn.state.effectiveMode(sessionMode)
}
func (e *Engine) CallLocalHost(ctx context.Context, turn *LocalHostTurn, name string, args json.RawMessage) (ExternalToolReply, error) {
	// pin the turn context before waiting for another call; cancelled calls cannot
	// acquire the next turn merely because a queued user message has started.
	callCtx, cancel := context.WithCancel(turn.Context)
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	defer cancel()
	turn.mu.Lock()
	defer turn.mu.Unlock()
	if err := callCtx.Err(); err != nil {
		return ExternalToolReply{}, err
	}
	if name == tool.CommandRun {
		callCtx = tool.WithCommandTurnContext(callCtx, turn.Context)
	}
	reply, _, err := e.executeSessionTool(callCtx, turn.SessionID, turn.active, &turn.state, turn.parts, externalRequest{name: name, args: args}, "")
	return reply, err
}

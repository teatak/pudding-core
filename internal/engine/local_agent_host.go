package engine

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
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

// Resolve live instructions before the model runs, so refreshing them does not
// require a catalog round trip. Tool discovery and schemas remain on demand.
func (e *Engine) localHostPrompt(ctx context.Context, sessionID, turnID, hostName string) (string, error) {
	sess, err := e.store.GetSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	rules, err := e.ExternalProjectRules(ctx, sess, turnID)
	if err != nil {
		return "", err
	}
	guides, err := e.ExternalGuides(ctx, sess, sess.LoadedPluginIDs, nil, true)
	if err != nil {
		return "", err
	}
	current, err := json.Marshal(map[string]any{
		"mode": sess.ActiveMode, "projectInstructions": rules, "instructions": guides,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("You are running inside Pudding. Use the %s MCP server for file, command, browser, widget and plugin operations so changes, approvals and results appear in this conversation. The current project rules and selected Skill/plugin instructions are supplied below; they replace earlier snapshots, including any old instruction to call pudding_tools at the start of every turn. Do not call tools for greetings or other requests that need no tools. Use pudding_tools only when you need to discover a tool, plugin or Skill, or lack a tool's exact inputSchema (including after context compaction). Reuse schemas already in context across calls and turns; query again if a tool is unavailable or its arguments are rejected. Call pudding_call directly when you already know the required schema. Pudding manages connection and Turn IDs; do not connect/begin/end manually. Read refreshed instructions in tool results. Use native tools only when Pudding cannot provide the needed operation, and explain that fallback. Other configured MCP servers may expose older Pudding tools requiring binding IDs; do not use them for this conversation.\n\nCurrent Pudding context:\n%s", hostName, current), nil
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

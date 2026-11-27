package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
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
	sessionID        string
	name             string
	turn             *LocalHostTurn // protected by Engine.localMu
	projectRulesSent bool           // protected by Engine.localMu
	lastProjectRules string         // protected by Engine.localMu
}
type LocalHostTurn struct {
	SessionID    string
	TurnID       string
	Context      context.Context
	mu           *sync.Mutex // serializes canonical output with ACP updates
	active       *activeTurn
	state        externalTurn
	parts        *turnPartAccumulator
	wg               sync.WaitGroup
	guideMu          sync.Mutex
	guideDigests     map[string]string
	projectRulesSent bool
	lastProjectRules string
}

// Resolve live instructions before the model runs, so refreshing them does not
// require a catalog round trip. Tool discovery and schemas remain on demand.
func (e *Engine) localHostPrompt(ctx context.Context, turn *LocalHostTurn, hostName string) (string, error) {
	sess, err := e.store.GetSession(ctx, turn.SessionID)
	if err != nil {
		return "", err
	}
	rules, err := e.ExternalProjectRules(ctx, sess, turn.TurnID)
	if err != nil {
		return "", err
	}
	guides, err := e.ExternalGuides(ctx, sess, sess.LoadedPluginIDs, nil, true)
	if err != nil {
		return "", err
	}
	contextPayload := map[string]any{"mode": sess.ActiveMode, "loadedPlugins": sess.LoadedPluginIDs}
	if err := turn.FilterGuides(guides, false); err != nil {
		return "", err
	}
	if len(guides) > 0 {
		contextPayload["instructions"] = guides
	}
	if turn.SyncProjectRules(rules) {
		contextPayload["projectInstructions"] = rules
	}
	current, err := json.Marshal(contextPayload)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("You are running inside Pudding. Use the %s MCP server for file, command, browser, widget and plugin operations so changes, approvals and results appear in this conversation. Current projectInstructions are supplied initially and whenever they change (omitted when unchanged); Skill and plugin instructions are supplied on first use and when changed. Keep earlier instructions for active plugins when omitted. Do not call tools for greetings or other requests that need no tools. Use pudding_tools only when you need to discover a tool, plugin or Skill, or lack a tool's exact inputSchema; call pudding_tools with no query after context compaction to recover the catalog and latest rules. Reuse schemas already in context across calls and turns; query again if a tool is unavailable or its arguments are rejected. Call pudding_call directly when you already know the required schema. Pudding manages connection and Turn IDs; do not connect/begin/end manually. Read refreshed instructions in tool results. Use native tools only when Pudding cannot provide the needed operation, and explain that fallback. Other configured MCP servers may expose older Pudding tools requiring binding IDs; do not use them for this conversation.\n\nCurrent Pudding context:\n%s", hostName, current), nil
}

// IncludeGuide compares the selected document, not the load operation's status or account metadata.
func (t *LocalHostTurn) IncludeGuide(key string, payload json.RawMessage, full bool) (bool, error) {
	var guide struct {
		Reference               tool.SkillReference
		Content                 *string
		Name, Description, Path string
	}
	if err := json.Unmarshal(payload, &guide); err != nil {
		return false, err
	}
	t.guideMu.Lock()
	defer t.guideMu.Unlock()
	if guide.Content == nil {
		delete(t.guideDigests, key)
		return true, nil // Preserve unloaded, unavailable and permission status.
	}
	body, err := json.Marshal(guide)
	if err != nil {
		return false, err
	}
	hash := sha256.Sum256(body)
	digest := hex.EncodeToString(hash[:])
	changed := t.guideDigests[key] != digest
	if t.guideDigests == nil {
		t.guideDigests = make(map[string]string)
	}
	t.guideDigests[key] = digest
	return full || changed, nil
}

func (t *LocalHostTurn) FilterGuides(guides map[string]json.RawMessage, full bool) error {
	for key, payload := range guides {
		include, err := t.IncludeGuide(key, payload, full)
		if err != nil {
			return err
		}
		if !include {
			delete(guides, key)
		}
	}
	return nil
}

// SyncProjectRules 记录本轮本地宿主已下发的最新项目规则，并返回内容是否发生变化。
func (t *LocalHostTurn) SyncProjectRules(rules string) bool {
	t.guideMu.Lock()
	defer t.guideMu.Unlock()
	if t.projectRulesSent && t.lastProjectRules == rules {
		return false
	}
	t.projectRulesSent = true
	t.lastProjectRules = rules
	return true
}

func (e *Engine) localHostProjectRulesSnapshot(secret string) (bool, string) {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	host := e.localHosts[secret]
	if host == nil {
		return false, ""
	}
	return host.projectRulesSent, host.lastProjectRules
}

func (e *Engine) commitLocalHostProjectRules(secret string, turn *LocalHostTurn) {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	host := e.localHosts[secret]
	if host == nil {
		return
	}
	host.projectRulesSent = turn.projectRulesSent
	host.lastProjectRules = turn.lastProjectRules
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

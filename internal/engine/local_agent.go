package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/acp"
	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

const RequestSourceHeader = "X-Pudding-Request-Source"

type requestSourceKey struct{}

func WithRequestSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, requestSourceKey{}, source)
}
func IsDesktopRequest(ctx context.Context) bool { return ctx.Value(requestSourceKey{}) == "desktop" }

var ErrLocalAgent = errors.New("local agent unavailable")
var ErrLocalAgentDesktop = errors.New("local Agent conversations require an interactive desktop request")

func (e *Engine) LocalAgentCatalog(ctx context.Context) (acp.Catalog, error) {
	if !IsDesktopRequest(ctx) {
		return acp.Catalog{}, ErrLocalAgentDesktop
	}
	root := filepath.Join(e.attachmentHome, "local-agents")
	if err := os.MkdirAll(root, 0700); err != nil {
		return acp.Catalog{}, err
	}
	cwd, err := os.MkdirTemp(root, "catalog-")
	if err != nil {
		return acp.Catalog{}, err
	}
	defer os.RemoveAll(cwd)
	paths := acp.Candidates()
	if e.localAgentPaths != nil {
		paths = e.localAgentPaths
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	return acp.Discover(ctx, paths, cwd)
}

type LocalAgentHandoffRequired struct {
	Text string `json:"text"`
}

func (*LocalAgentHandoffRequired) Error() string {
	return "Review the handoff before starting a new Antigravity context"
}

type localAgentRestoreError struct{ cause error }

func (e *localAgentRestoreError) Error() string {
	return "restore Antigravity session: " + e.cause.Error()
}

type localAgentRuntime struct {
	mu         sync.Mutex // preparation and the active prompt share a single ACP session
	client     *acp.Client
	state      *store.LocalAgentState
	hostSecret string
	hostName   string
}

func (e *Engine) localRuntime(id string) *localAgentRuntime {
	e.localMu.Lock()
	defer e.localMu.Unlock()
	if e.localAgents == nil {
		e.localAgents = map[string]*localAgentRuntime{}
	}
	if e.localAgents[id] == nil {
		e.localAgents[id] = &localAgentRuntime{}
	}
	return e.localAgents[id]
}
func (e *Engine) closeLocalAgents(id string) {
	e.localMu.Lock()
	var targets []*localAgentRuntime
	for key, r := range e.localAgents {
		if id == "" || id == key {
			targets = append(targets, r)
			delete(e.localAgents, key)
		}
	}
	e.localMu.Unlock()
	for _, r := range targets {
		r.mu.Lock()
		if r.client != nil {
			e.revokeLocalHost(r.hostSecret)
			r.client.Close()
			r.client = nil
		}
		r.mu.Unlock()
	}
}
func (e *Engine) localCWD(ctx context.Context, sess *store.Session) (string, error) {
	if sess.ProjectID != "" {
		project, err := e.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return "", err
		}
		if len(project.RootDirs) > 0 {
			return project.RootDirs[0], nil
		}
	}
	if e.attachmentHome == "" {
		return "", fmt.Errorf("%w: Pudding home is unavailable", ErrLocalAgent)
	}
	dir := filepath.Join(e.attachmentHome, "local-agents", "workspaces", sess.ID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}
func (e *Engine) localDuplicate(ctx context.Context, in SubmitInput) (*SubmitResult, error) {
	turn, queued, err := e.store.LookupSubmission(ctx, in.SessionID, in.ClientMessageID)
	if err != nil {
		return nil, err
	}
	if turn != nil {
		return &SubmitResult{Duplicate: true, TurnID: turn.ID}, nil
	}
	if queued != nil {
		return &SubmitResult{Duplicate: true, Queued: true, ClientMessageID: in.ClientMessageID, Status: string(queued.Status)}, nil
	}
	return nil, nil
}

// Admission only checks ownership and reviewed context changes. CLI startup,
// configuration and prompt preparation belong to the accepted turn, just like
// an API provider request: failures must not turn a sent message back into a draft.
func (e *Engine) preflightLocal(ctx context.Context, in *SubmitInput, sess *store.Session, resolved *resolvedModel) (*SubmitResult, error) {
	if !IsDesktopRequest(ctx) || (in.Kind == "system" && in.retryOfTurnID == "") {
		return nil, ErrLocalAgentDesktop
	}
	parent, err := e.store.ParentSessionID(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	if parent != "" {
		return nil, ErrLocalAgentDesktop
	}
	if out, err := e.localDuplicate(ctx, *in); out != nil || err != nil {
		return out, err
	}
	cwd, err := e.localCWD(ctx, sess)
	if err != nil {
		return nil, err
	}
	resolved.config.LocalAgent = &provider.LocalAgentConfig{Source: "desktop", CWD: cwd, ReasoningEffort: activeReasoningEffort(in.ReasoningEffort, sess, resolved)}
	if err = e.checkLocalHandoff(ctx, in, cwd); err != nil {
		return nil, err
	}
	resolved.config.LocalAgent.NewContext = in.LocalAgentHandoff != nil
	if err = resolved.refreshConfigJSON(); err != nil {
		return nil, err
	}
	running, err := e.store.RunningTurn(ctx, sess.ID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	queued, err := e.store.HasQueuedInputs(ctx, sess.ID)
	if err != nil {
		return nil, err
	}
	if running != nil || queued {
		if in.retryOfTurnID != "" {
			return nil, ErrTurnRunning
		}
		out, err := e.queueSubmit(ctx, *in, resolved)
		return out, err
	}
	return nil, nil
}
func (e *Engine) checkLocalHandoff(ctx context.Context, in *SubmitInput, cwd string) error {
	state, err := e.store.GetLocalAgentState(ctx, in.SessionID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	page, err := e.store.ListTurnsPage(ctx, in.SessionID, "", 1)
	if err != nil {
		return err
	}
	needs := state != nil && (state.CWD != cwd || state.NeedsHandoff)
	if len(page.Turns) > 0 {
		last := page.Turns[len(page.Turns)-1]
		// Failed ACP preparation is still canonical history, and is supplied on
		// the next prompt. Only changing inference contexts requires a handoff.
		needs = needs || (last.Provider != acp.Provider && (state == nil || last.ID != state.LastTurnID))
	}
	if !needs && in.LocalAgentHandoff == nil {
		return nil
	}
	if in.LocalAgentHandoff == nil {
		handoff, err := e.localHandoff(ctx, in.SessionID)
		if err != nil {
			return err
		}
		return handoff
	}

	// An explicit empty handoff means the user chose a fresh context.
	if text := strings.TrimSpace(*in.LocalAgentHandoff); text != "" {
		in.Parts = append([]store.ContentPart{{Type: store.ContentPartText, Text: "Previous conversation handoff (reviewed by the user):\n" + text + "\n\n"}}, store.UserInputParts(in.Text, in.Parts)...)
		in.Text = store.TextFromParts(in.Parts)
	}
	// The next preparation creates a fresh agent context. Retain the previous
	// mapping until creation succeeds; queued requests carry the reset choice.
	return nil
}
func (e *Engine) localHandoff(ctx context.Context, id string) (*LocalAgentHandoffRequired, error) {
	messages, err := e.store.ListMessages(ctx, id, 12)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	for _, m := range messages {
		text := store.TextFromParts(m.Parts)
		if text == "" {
			continue
		}
		runes := []rune(text)
		if len(runes) > 1500 {
			text = string(runes[:1500]) + "…"
		}
		fmt.Fprintf(&b, "%s: %s\n\n", m.Role, text)
	}
	return &LocalAgentHandoffRequired{Text: b.String()}, nil
}

func (e *Engine) prepareLocal(ctx context.Context, id, turnID string, resolved *resolvedModel, r *localAgentRuntime) error {
	if err := e.auxCtx.Err(); err != nil {
		return err
	}
	cfg := resolved.config.LocalAgent
	if cfg == nil || cfg.Source != "desktop" || resolved.providerName != acp.Provider {
		return ErrLocalAgentDesktop
	}
	if r.state == nil {
		state, err := e.store.GetLocalAgentState(ctx, id)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		r.state = state
	}
	if r.client != nil && (!r.client.Alive() || cfg.NewContext || (r.state != nil && r.state.CWD != cfg.CWD)) {
		e.revokeLocalHost(r.hostSecret)
		r.client.Close()
		r.client = nil
	}
	if r.client == nil {
		paths := acp.Candidates()
		if e.localAgentPaths != nil {
			paths = e.localAgentPaths
		}
		c, err := acp.Open(ctx, paths, cfg.CWD)
		if err != nil {
			return err
		}
		agentID := ""
		if r.state != nil && r.state.CWD == cfg.CWD && !cfg.NewContext {
			agentID = r.state.AgentSessionID
		}
		secret, servers, err := e.newLocalHost(id)
		if err != nil {
			c.Close()
			return err
		}
		session, err := c.Session(ctx, cfg.CWD, agentID, servers...)
		if err != nil {
			e.revokeLocalHost(secret)
			c.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if agentID != "" {
				r.state.NeedsHandoff = true
				if saveErr := e.store.PutLocalAgentState(context.Background(), r.state); saveErr != nil {
					return saveErr
				}
				return &localAgentRestoreError{cause: err}
			}
			return fmt.Errorf("create Antigravity session: %w", err)
		}
		state := store.LocalAgentState{SessionID: id, AgentSessionID: session.SessionID, Executable: c.Path, CWD: cfg.CWD, Image: c.Init.Capabilities.Prompt.Image, Audio: c.Init.Capabilities.Prompt.Audio, EmbeddedContext: c.Init.Capabilities.Prompt.EmbeddedContext}
		if r.state != nil && agentID != "" {
			state.LastTurnID = r.state.LastTurnID
			state.ContextUsed = r.state.ContextUsed
			state.ContextSize = r.state.ContextSize
		}
		if cfg.NewContext {
			// A reviewed handoff replaces the old context. Do not append the
			// omitted history again, even if this new turn fails before prompting.
			previous, err := e.store.ListTurnsPage(ctx, id, turnID, 1)
			if err != nil {
				e.revokeLocalHost(secret)
				c.Close()
				return err
			}
			if len(previous.Turns) > 0 {
				state.LastTurnID = previous.Turns[len(previous.Turns)-1].ID
			}
		}
		if err = e.store.PutLocalAgentState(ctx, &state); err != nil {
			e.revokeLocalHost(secret)
			c.Close()
			return err
		}
		r.client = c
		r.hostSecret = secret
		if len(servers) > 0 {
			r.hostName = servers[0].Name
		}
		r.state = &state
		cfg.NewContext = false
	}
	_, err := r.client.Configure(ctx, r.state.AgentSessionID, resolved.model, cfg.ReasoningEffort)
	if err != nil {
		e.revokeLocalHost(r.hostSecret)
		r.client.Close()
		r.client = nil
	}
	return err
}
func (e *Engine) localPrompt(sessionID string, parts []store.ContentPart, text string, state *store.LocalAgentState) ([]acp.Content, error) {
	parts = store.UserInputParts(text, parts)
	out := make([]acp.Content, 0, len(parts))
	for _, part := range parts {
		if part.Type == store.ContentPartText {
			out = append(out, acp.Content{Type: "text", Text: part.Text})
			continue
		}
		if part.Type == store.ContentPartUIContext {
			if text := contextbuilder.UIContextText(part); text != "" {
				out = append(out, acp.Content{Type: "text", Text: text})
			}
			continue
		}
		if part.Type == store.ContentPartLocalFolder {
			out = append(out, acp.Content{Type: "text", Text: contextbuilder.LocalFoldersText(store.LocalFoldersFromParts([]store.ContentPart{part}))})
			continue
		}
		if part.Type == store.ContentPartProjectRef {
			out = append(out, acp.Content{Type: "text", Text: contextbuilder.ProjectReferencesText(store.ProjectReferencesFromParts([]store.ContentPart{part}))})
			continue
		}
		if part.Type != store.ContentPartAttachment {
			return nil, fmt.Errorf("unsupported ACP input: %s", part.Type)
		}
		path, ok, err := attachment.NewService(e.attachmentHome).Path(sessionID, part.AttachmentKey)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("attachment is not available in this conversation")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		switch {
		case strings.HasPrefix(part.MIME, "image/") && state.Image:
			out = append(out, acp.Content{Type: "image", MIMEType: part.MIME, Data: base64.StdEncoding.EncodeToString(data)})
		case strings.HasPrefix(part.MIME, "audio/") && state.Audio:
			out = append(out, acp.Content{Type: "audio", MIMEType: part.MIME, Data: base64.StdEncoding.EncodeToString(data)})
		case (strings.HasPrefix(part.MIME, "text/") || part.MIME == "application/json") && state.EmbeddedContext && utf8.Valid(data):
			out = append(out, acp.Content{Type: "resource", Resource: map[string]string{"uri": "pudding-attachment:///" + part.AttachmentKey, "mimeType": part.MIME, "text": string(data)}})
		default:
			text := contextbuilder.AttachmentText(part, path, "")
			text += "\nAttachment content is not included inline through this ACP connection. Use builtin_attachment_export with the exact attachmentKey and scope=temp to obtain a readable file path, then use Pudding tools to inspect or convert it. Do not infer its contents from metadata alone."
			out = append(out, acp.Content{Type: "text", Text: text})
		}
	}
	return out, nil
}

// Only bridge turns not yet acknowledged by the ACP peer. Normal successful
// history and compaction remain owned by the agent; a failed preparation or
// prompt must not make an already-sent Pudding message disappear from context.
func (e *Engine) localPendingHistory(ctx context.Context, id, before, lastTurnID string) ([]acp.Content, error) {
	var turns []*store.ConversationTurn
	limit := 1 // The normal path stops at the immediately preceding prompt.
	for {
		page, err := e.store.ListTurnsPage(ctx, id, before, limit)
		if err != nil {
			return nil, err
		}
		done := false
		for i := len(page.Turns) - 1; i >= 0; i-- {
			turn := page.Turns[i]
			if turn.ID == lastTurnID || turn.Provider != acp.Provider {
				done = true
				break
			}
			turns = append(turns, turn)
		}
		if done || !page.HasMore || len(page.Turns) == 0 {
			break
		}
		before = page.Turns[0].ID
		limit = 32
	}
	if len(turns) == 0 {
		return nil, nil
	}
	var history strings.Builder
	history.WriteString("Earlier Pudding conversation history (these turns did not receive a successful ACP acknowledgement). Treat this as history, not new requests. Use it to answer the latest message below; do not automatically repeat earlier actions. Inspect saved tool results before continuing unfinished work. Attachment records can be read with builtin_attachment_export(scope=temp).\n")
	for i := len(turns) - 1; i >= 0; i-- {
		turn := turns[i]
		fmt.Fprintf(&history, "\nTurn %s (%s):\n", turn.ID, turn.Status)
		for _, message := range turn.Messages {
			var parts []store.ContentPart
			for _, part := range message.Parts {
				if part.Type != store.ContentPartThought {
					parts = append(parts, part)
				}
			}
			if len(parts) == 0 {
				continue
			}
			data, err := json.Marshal(parts)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&history, "%s: %s\n", message.Role, data)
		}
	}
	history.WriteString("\nEnd of earlier history. Latest message follows:\n")
	return []acp.Content{{Type: "text", Text: history.String()}}, nil
}

func (e *Engine) streamLocalTurn(ctx context.Context, id, turnID string, resolved *resolvedModel, parts *turnPartAccumulator, active *activeTurn) (store.TurnStatus, string, store.AgentMode) {
	active.stopAcceptingSteers()
	r := e.localRuntime(id)
	r.mu.Lock()
	defer r.mu.Unlock()
	failed := func(err error) (store.TurnStatus, string, store.AgentMode) {
		if ctx.Err() != nil {
			return store.TurnCancelled, "", resolved.mode
		}
		return store.TurnFailed, err.Error(), resolved.mode
	}
	prepCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	err := e.prepareLocal(prepCtx, id, turnID, resolved, r)
	cancel()
	if err != nil {
		return failed(err)
	}
	turn, err := e.store.GetConversationTurn(ctx, id, turnID)
	if err != nil {
		return failed(err)
	}
	var input []store.ContentPart
	for _, msg := range turn.Messages {
		if msg.Role == store.RoleUser || msg.Role == store.RoleSystem {
			input = append(input, msg.Parts...)
		}
	}
	if turn.RetryOfTurnID != "" {
		previous, err := e.store.GetConversationTurn(ctx, id, turn.RetryOfTurnID)
		if err != nil {
			return failed(err)
		}
		input = append(input, store.ContentPart{Type: store.ContentPartText, Text: "Original request for this retry (inspect saved results before repeating actions):"})
		for _, message := range previous.Messages {
			if message.Role == store.RoleUser || message.Role == store.RoleSystem {
				input = append(input, message.Parts...)
			}
		}
	}
	prompt, err := e.localPrompt(id, input, "", r.state)
	if err != nil {
		return failed(err)
	}
	history, err := e.localPendingHistory(ctx, id, turnID, r.state.LastTurnID)
	if err != nil {
		return failed(err)
	}
	prompt = append(history, prompt...)
	if r.hostSecret != "" {
		hostPrompt, err := e.localHostPrompt(ctx, id, turnID, r.hostSecret, r.hostName)
		if err != nil {
			return failed(err)
		}
		prompt = append([]acp.Content{{Type: "text", Text: hostPrompt}}, prompt...)
	}
	var outputErr error
	var outputMu sync.Mutex
	calls := map[string]tool.Call{}
	callOrder := []string{}
	finished := map[string]bool{}
	// Keep the tool use at its start position. ACP may replace its input later
	// or finish a background command after the answer has already streamed.
	completeACPTool := func(call tool.Call, result tool.Result) error {
		if finished[call.CallID] {
			return nil
		}
		finished[call.CallID] = true
		return e.completeToolCall(id, turnID, nil, parts, call, result)
	}
	promptCtx, stop := context.WithCancel(ctx)
	defer stop()
	hostTurn := &LocalHostTurn{SessionID: id, TurnID: turnID, Context: promptCtx, mu: &outputMu, active: active, parts: parts}
	e.startLocalHost(r.hostSecret, hostTurn)
	result, err := r.client.Prompt(promptCtx, r.state.AgentSessionID, prompt, acp.Handlers{
		Ready: func() {
			active.mu.Lock()
			defer active.mu.Unlock()
			if promptCtx.Err() == nil {
				active.acceptingSteers = true
				active.checkLocalSteer = r.client.CheckSteer
			}
		},
		Steering: active.steerNotify,
		NextPrompt: func(seal bool) ([]acp.Content, error) {
			outputMu.Lock()
			defer outputMu.Unlock()
			var steers []pendingSteer
			if seal {
				steers = active.consumeSteersOrSeal()
			} else {
				steers = active.consumeSteers()
			}
			if len(steers) == 0 {
				return nil, nil
			}
			if err := e.commitTurnParts(turnID, parts, true); err != nil {
				return nil, err
			}
			if err := e.applyTurnSteers(turnID, steers); err != nil {
				return nil, err
			}
			var input []store.ContentPart
			for _, steer := range steers {
				var message []store.ContentPart
				if err := json.Unmarshal(steer.event.Parts, &message); err != nil {
					return nil, err
				}
				input = append(input, message...)
			}
			return e.localPrompt(id, input, "", r.state)
		},
		Update: func(update acp.Update) {
			outputMu.Lock()
			defer outputMu.Unlock()
			if outputErr != nil {
				return
			}
			switch update.SessionUpdate {
			case "agent_message_chunk", "agent_thought_chunk":
				var content acp.Content
				if json.Unmarshal(update.Content, &content) != nil || content.Type != "text" {
					return
				}
				part := provider.PartText
				if update.SessionUpdate == "agent_thought_chunk" {
					part = provider.PartThought
				}
				parts.AppendDelta(part, content.Text)
				outputErr = e.commitTurnParts(turnID, parts, false)
				if outputErr != nil {
					stop()
					return
				}
				e.hub.Publish(event.Event{SessionID: id, TurnID: turnID, Kind: event.TurnDelta, Part: string(part), Delta: content.Text})
			case "usage_update":
				r.state.ContextUsed = update.Used
				r.state.ContextSize = update.Size
			case "tool_call", "tool_call_update":
				if update.ToolCallID == "" {
					return
				}
				call, exists := calls[update.ToolCallID]
				if !exists {
					name := update.Name
					if name == "" {
						name = update.Title
					}
					if name == "" {
						name = update.Kind
					}
					args, ok := acpToolRawInput(update.RawInput)
					if !ok {
						args = json.RawMessage(`{}`)
					}
					call = tool.Call{CallID: "acp:" + update.ToolCallID, Name: "antigravity." + name, Args: args}
					calls[update.ToolCallID] = call
					callOrder = append(callOrder, update.ToolCallID)
					parts.AppendTool(provider.ToolCallChunk{Index: len(parts.parts), CallID: call.CallID, Name: call.Name, ArgsDelta: string(call.Args)})
					if outputErr = e.commitTurnParts(turnID, parts, true); outputErr != nil {
						stop()
						return
					}
					e.hub.Publish(event.Event{SessionID: id, TurnID: turnID, Kind: event.TurnTool, CallID: call.CallID, Name: call.Name, Phase: "running", ArgsText: string(call.Args)})
				} else if !finished[call.CallID] {
					if args, ok := acpToolRawInput(update.RawInput); ok {
						changed := !bytes.Equal(call.Args, args)
						call.Args = args
						calls[update.ToolCallID] = call
						if changed {
							parts.ReplaceToolArguments(call.CallID, args)
							if outputErr = e.store.UpdateTurnToolArguments(context.Background(), turnID, call.CallID, args); outputErr != nil {
								stop()
								return
							}
							e.hub.Publish(event.Event{SessionID: id, TurnID: turnID, Kind: event.TurnTool, CallID: call.CallID, Name: call.Name, Phase: "running", ArgsText: string(call.Args)})
						}
					}
				}
				if !finished[call.CallID] && (update.Status == "completed" || update.Status == "failed") {
					raw, _ := json.Marshal(update)
					toolResult := tool.Result{CallID: call.CallID, Name: call.Name, Ok: update.Status == "completed", Content: string(raw)}
					// agy reports in-flight tools as failed in response to session/cancel.
					// Preserve the raw update, but distinguish this from an earlier failure.
					if update.Status == "failed" && ctx.Err() != nil {
						toolResult.SummaryKind = tool.SummaryCancelled
					}
					outputErr = completeACPTool(call, toolResult)
					if outputErr != nil {
						stop()
					}
				}

			}
		}, Permission: func(permissionCtx context.Context, p acp.Permission) acp.Outcome {
			return e.requestLocalPermission(permissionCtx, id, turnID, r.hostSecret, p)
		}})
	active.stopAcceptingSteers()
	stop()
	e.finishLocalHost(r.hostSecret, hostTurn)
	if !r.client.Alive() {
		e.revokeLocalHost(r.hostSecret)
	}

	outputMu.Lock()
	for _, toolID := range callOrder {
		call := calls[toolID]
		if !finished[call.CallID] {
			reason := "Antigravity ended before this tool returned"
			summary := ""
			if ctx.Err() != nil {
				reason = "cancelled"
				summary = tool.SummaryCancelled
			}
			if closeErr := completeACPTool(call, tool.Result{CallID: call.CallID, Name: call.Name, Ok: false, Content: reason, SummaryKind: summary}); closeErr != nil && outputErr == nil {
				outputErr = closeErr
			}
		}
	}
	outputMu.Unlock()
	// An RPC failure does not establish that the agent saved the input. Keep
	// those turns in the next prompt as labelled history, never as fresh actions.
	if err == nil {
		r.state.LastTurnID = turnID
	}
	if saveErr := e.store.PutLocalAgentState(context.Background(), r.state); saveErr != nil {
		return failed(saveErr)
	}
	if result.Usage != nil {
		u := result.Usage
		e.recordUsage(context.Background(), id, resolved.providerName, resolved.model, 0, provider.UsageInfo{InputUncachedTokens: u.Input, InputCachedTokens: u.CachedRead, CacheCreationTokens: u.CachedWrite, OutputContentTokens: u.Output, OutputReasoningTokens: u.Thought}, result.Requests)
	}
	if outputErr != nil {
		return failed(fmt.Errorf("save ACP output: %w", outputErr))
	}
	if ctx.Err() != nil || result.StopReason == "cancelled" {
		return store.TurnCancelled, "", resolved.mode
	}
	if err != nil {
		return failed(err)
	}
	if result.StopReason != "end_turn" {
		return failed(fmt.Errorf("Antigravity stopped: %s", result.StopReason))
	}
	session, readErr := e.store.GetSession(context.Background(), id)
	if readErr != nil {
		return failed(readErr)
	}
	return store.TurnCompleted, "", e.LocalHostMode(hostTurn, session.ActiveMode)
}

func acpToolRawInput(raw json.RawMessage) (json.RawMessage, bool) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, false
	}
	return append(json.RawMessage(nil), trimmed...), true
}
func (e *Engine) localUsage(ctx context.Context, id string) (*SessionUsageInfo, error) {
	stat, err := e.store.SessionUsage(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &SessionUsageInfo{SessionID: id, RequestCount: stat.RequestCount,
		LastPromptTokens: stat.LastInputTokens(), LastInputUncachedTokens: stat.LastInputUncachedTokens, LastInputCachedTokens: stat.LastInputCachedTokens, LastCacheCreationTokens: stat.LastCacheCreationTokens, LastOutputContentTokens: stat.LastOutputContentTokens, LastOutputReasoningTokens: stat.LastOutputReasoningTokens, LastOutputTokens: stat.LastOutputTokens(), CumulativeInputUncachedTokens: stat.CumulativeInputUncachedTokens, CumulativeInputCachedTokens: stat.CumulativeInputCachedTokens, CumulativeCacheCreationTokens: stat.CumulativeCacheCreationTokens, CumulativeOutputContentTokens: stat.CumulativeOutputContentTokens, CumulativeOutputReasoningTokens: stat.CumulativeOutputReasoningTokens, CumulativeInputTokens: stat.CumulativeInputTokens(), CumulativeOutputTokens: stat.CumulativeOutputTokens(), CumulativeTotalTokens: stat.CumulativeTotalTokens(), UpdatedAt: stat.UpdatedAt}
	state, err := e.store.GetLocalAgentState(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	out.ContextWindow = state.ContextSize
	out.ContextEstimatedTokens = state.ContextUsed
	out.ContextRawEstimatedTokens = state.ContextUsed
	return out, nil
}
func (e *Engine) auxiliaryModel(ctx context.Context) (*resolvedModel, error) {
	profiles, err := e.config.ListProviderProfiles(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if len(p.Models) > 0 {
			return resolvedModelFromParts(p.ProfileID(), p.Brand, p.Protocol, p.Models[0].ID, modelConfigFromEntry(p.Models[0]))
		}
	}
	return nil, ErrNoModel
}

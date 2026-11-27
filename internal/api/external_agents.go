package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/teatak/cart/v3"
	"github.com/teatak/pudding-core/internal/engine"
	"github.com/teatak/pudding-core/internal/plugin"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func agentHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func (s *Server) inviteAgent(c *cart.Context) error {
	sid, _ := c.Param("id")
	ctx := c.Request.Context()
	if _, err := s.store.GetSession(ctx, sid); err != nil {
		return s.failAgent(c, err)
	}
	code := rand.Text()
	b := store.ExternalAgentBinding{ID: store.NewID("agent"), SessionID: sid, RuntimeID: plugin.RuntimeIDFromContext(ctx)}
	expires := time.Now().Add(10 * time.Minute)
	if err := s.store.CreateAgentInvite(ctx, b, agentHash(code), expires.UnixMilli()); err != nil {
		return s.failAgentHandoff(c, err)
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusCreated, map[string]any{"invite": code, "expiresAt": expires})
	return nil
}

func (s *Server) listAgents(c *cart.Context) error {
	sid, _ := c.Param("id")
	bindings, err := s.store.ListAgentBindings(c.Request.Context(), sid)
	if err != nil {
		return s.failAgent(c, err)
	}
	c.JSON(200, map[string]any{"bindings": bindings})
	return nil
}

func (s *Server) removeAgent(c *cart.Context) error {
	sid, _ := c.Param("id")
	id, _ := c.Param("binding")
	return s.disconnectAgent(c, sid, id)
}

func (s *Server) disconnectAgent(c *cart.Context, sid, id string) error {
	if err := s.store.RemoveAgentBinding(c.Request.Context(), sid, id); err != nil {
		return s.failAgent(c, err)
	}
	s.engine.CancelExternalBinding(sid, id)
	c.JSON(200, map[string]bool{"ok": true})
	return nil
}

type agentRequest struct {
	Invite        string           `json:"invite"`
	Name          string           `json:"name"`
	Title         string           `json:"title"`
	IconPNGBase64 string           `json:"iconPNGBase64"`
	Task          string           `json:"task"`
	TurnID        string           `json:"turnID"`
	Tool          string           `json:"tool"`
	Query         string           `json:"query"`
	Queries       []string         `json:"queries"`
	NamesOnly     bool             `json:"namesOnly"`
	Arguments     json.RawMessage  `json:"arguments"`
	Status        store.TurnStatus `json:"status"`
	Summary       string           `json:"summary"`
}

func externalAgentIcon(encoded string) string {
	if len(encoded) == 0 || len(encoded) > 64*1024 {
		return ""
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	if _, err := png.DecodeConfig(bytes.NewReader(data)); err != nil {
		return ""
	}
	return encoded
}

func (s *Server) agentCommand(c *cart.Context) error {
	action, _ := c.Param("action")
	var in agentRequest
	if err := decode(c, &in); err != nil {
		return badRequest(c, "invalid agent request")
	}
	ctx := c.Request.Context()
	c.Header("Cache-Control", "no-store")
	if action == "connect" {
		name := strings.TrimSpace(in.Name)
		if name == "" || len(name) > 100 || strings.ContainsAny(name, "\r\n\x00") || in.Invite == "" {
			return badRequest(c, "agent name and invitation required")
		}
		title := strings.TrimSpace(in.Title)
		if len(title) > 200 || strings.ContainsAny(title, "\r\n\x00") {
			title = ""
		}
		identity := store.ExternalAgentIdentity{Name: name, Title: title, IconPNGBase64: externalAgentIcon(in.IconPNGBase64)}
		credential := c.Request.Header.Get("X-Pudding-Binding")
		if credential == "" {
			credential = rand.Text()
		} else if decoded, err := hex.DecodeString(credential); err != nil || len(decoded) != 32 {
			return badRequest(c, "invalid binding credential")
		}
		b, err := s.engine.RedeemExternalAgentInvite(ctx, agentHash(in.Invite), agentHash(credential), identity)
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "Invitation expired, already redeemed, or unavailable. Create a new invitation in Pudding."})
			return nil
		}
		if err != nil {
			return s.failAgentHandoff(c, err)
		}
		c.JSON(200, map[string]any{"bindingID": b.ID, "credential": credential})
		return nil
	}
	credential := c.Request.Header.Get("X-Pudding-Binding")
	if credential == "" {
		return badRequest(c, "binding required")
	}
	hash := agentHash(credential)
	b, err := s.store.AgentBinding(ctx, hash)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "This binding is no longer available. Ask for a new invitation in the Pudding conversation."})
		return nil
	}
	if err != nil {
		return s.failAgent(c, err)
	}
	if action == "disconnect" {
		return s.disconnectAgent(c, b.SessionID, b.ID)
	}
	ctx = plugin.WithRuntimeID(ctx, b.RuntimeID)
	sess, err := s.store.GetSession(ctx, b.SessionID)
	if errors.Is(err, store.ErrNotFound) {
		c.JSON(http.StatusNotFound, map[string]string{"error": "The bound conversation is archived or unavailable."})
		return nil
	}
	if err != nil {
		return s.failAgent(c, err)
	}
	switch action {
	case "tools":
		if len(in.Queries) > maxAgentCatalogQueries {
			return badRequest(c, "queries accepts at most 16 items")
		}
		catalog, err := s.agentCatalog(ctx, sess, b, normalizeCatalogQueries(in.Query, in.Queries), in.NamesOnly)
		if err != nil {
			return s.failAgent(c, err)
		}
		c.JSON(200, catalog)
	case "status":
		id, mode := s.engine.ExternalTurnContext(b, sess.ActiveMode)
		state := map[string]any{"session": sess.Title, "mode": mode, "activeTurnID": id}
		if in.TurnID != "" {
			t, err := s.store.GetConversationTurn(ctx, b.SessionID, in.TurnID)
			if err != nil {
				return s.failAgent(c, err)
			}
			if t.ClientMessageID != "external-mcp:"+b.ID+":"+in.TurnID {
				return s.fail(c, store.ErrNotFound)
			}
			state["status"] = t.Status
			var messages []string
			for _, m := range t.Messages {
				if m.Role == store.RoleUser && m.ClientMessageID != t.ClientMessageID {
					messages = append(messages, m.Text)
				}
			}
			state["userMessages"] = messages
		}
		c.JSON(200, state)
	case "begin":
		task := strings.TrimSpace(in.Task)
		if task == "" || len(task) > 2000 {
			return badRequest(c, "provide a short task")
		}
		dirs, err := s.engine.ExternalProjectDirs(ctx, sess, "")
		if err != nil {
			return s.failAgent(c, err)
		}
		id, err := s.engine.BeginExternalTurn(ctx, hash, task)
		if err != nil {
			if errors.Is(err, store.ErrTurnRunning) {
				if activeID, _ := s.engine.ExternalTurnContext(b, sess.ActiveMode); activeID != "" {
					c.JSON(http.StatusConflict, map[string]any{
						"error":        "This binding already has an active turn (" + activeID + "). Reuse that turnID, inspect it with pudding_session_status, or call pudding_turn_end before starting another turn.",
						"activeTurnID": activeID,
					})
					return nil
				}
				c.JSON(http.StatusConflict, map[string]string{
					"error": "Another task is running in this Pudding conversation. Wait for it to finish or stop it in Pudding, then retry pudding_turn_begin.",
				})
				return nil
			}
			return s.failAgent(c, err)
		}
		reply := map[string]any{"turnID": id, "session": sess.Title, "mode": sess.ActiveMode}
		if len(dirs) > 0 {
			reply["projectDirectories"] = dirs
		}
		c.JSON(200, reply)
	case "call":
		if in.Tool == "" || in.TurnID == "" {
			return badRequest(c, "turnID and tool are required")
		}
		if len(in.Arguments) == 0 {
			in.Arguments = json.RawMessage("{}")
		}
		if !debugMCPObject(in.Arguments) {
			return badRequest(c, "arguments must be an object")
		}
		reply, err := s.engine.CallExternalTool(ctx, b, in.TurnID, in.Tool, in.Arguments)
		if err != nil {
			return s.failAgent(c, err)
		}
		out, err := s.agentToolReply(ctx, sess, in.TurnID, reply, nil)
		if err != nil {
			return s.failAgent(c, err)
		}
		c.JSON(200, out)
	case "end":
		if in.Status == "" {
			in.Status = store.TurnCompleted
		}
		if !slices.Contains([]store.TurnStatus{store.TurnCompleted, store.TurnFailed, store.TurnCancelled}, in.Status) || len(in.Summary) > 4000 {
			return badRequest(c, "invalid turn outcome")
		}
		if err := s.engine.EndExternalTurn(ctx, b, in.TurnID, in.Status, in.Summary); err != nil {
			return s.failAgent(c, err)
		}
		c.JSON(200, map[string]any{"turnID": in.TurnID, "status": in.Status})
	default:
		return badRequest(c, "unknown agent operation")
	}
	return nil
}

func (s *Server) agentToolReply(ctx context.Context, sess *store.Session, turnID string, reply engine.ExternalToolReply, local *engine.LocalHostTurn) (any, error) {
	var err error
	result := reply.Result
	if ref, ok := tool.ReferencedSkill(result.Name, result.Ok, result.Content); ok {
		sess, err = s.store.GetSession(ctx, sess.ID)
		if err != nil {
			return nil, err
		}
		result, err = s.engine.ExternalSkillResult(ctx, sess.ID, reply.Mode, result)
		if err != nil {
			return nil, err
		}
		if local != nil && result.Ok {
			// A fresh load and an explicit read both need their document immediately.
			var load struct{ NewlyLoaded bool }
			if err := json.Unmarshal([]byte(reply.Result.Content), &load); err != nil {
				return nil, err
			}
			include, err := local.IncludeGuide(ref.Key(), json.RawMessage(result.Content), load.NewlyLoaded || result.Name == tool.SkillRead)
			if err != nil {
				return nil, err
			}
			if !include {
				result.Content = reply.Result.Content
			}
		}
		// The primary result already handles this guide. SkillRefs is a per-reply copy.
		delete(reply.SkillRefs, ref.Key())
	}
	out, err := s.externalMCPToolResult(sess.ID, result)
	if err != nil {
		return nil, err
	}
	for _, message := range reply.UserMessages {
		out.Content = append(out.Content, map[string]any{"type": "text", "text": "User in Pudding: " + message})
	}
	var projectInstructions *string
	if reply.RefreshProjectRules {
		sess.ActiveMode = reply.Mode
		rules, err := s.engine.ExternalProjectRules(ctx, sess, turnID)
		if err != nil {
			out.Content = append(out.Content, map[string]any{"type": "text", "text": "Could not read project instructions: " + err.Error()})
		} else {
			projectInstructions = &rules
		}
	}
	var guides map[string]json.RawMessage
	if reply.PluginID != "" || len(reply.SkillRefs) > 0 {
		var plugins []string
		if reply.PluginID != "" {
			plugins = []string{reply.PluginID}
		}
		sess.ActiveMode = reply.Mode
		guides, err = s.engine.ExternalGuides(ctx, sess, plugins, reply.SkillRefs, true)
		if err != nil {
			// The tool may have already made changes. Preserve its outcome.
			out.Content = append(out.Content, map[string]any{"type": "text", "text": "Could not refresh skill instructions: " + err.Error()})
		}
	}
	return struct {
		debugMCPResult
		Instructions        map[string]json.RawMessage `json:"instructions,omitempty"`
		ProjectInstructions *string                    `json:"projectInstructions,omitempty"`
	}{out, guides, projectInstructions}, nil
}

const maxAgentCatalogQueries = 16

func normalizeCatalogQueries(query string, queries []string) []string {
	out := make([]string, 0, 1+len(queries))
	seen := map[string]bool{}
	add := func(item string) {
		q := strings.ToLower(strings.TrimSpace(item))
		if q == "" || seen[q] {
			return
		}
		seen[q] = true
		out = append(out, q)
	}
	add(query)
	for _, item := range queries {
		add(item)
	}
	return out
}

func (s *Server) agentCatalog(ctx context.Context, sess *store.Session, b *store.ExternalAgentBinding, queries []string, namesOnly bool) (map[string]any, error) {
	turnID, mode := s.engine.ExternalTurnContext(b, sess.ActiveMode)
	return s.sessionToolCatalog(ctx, sess, turnID, mode, queries, namesOnly)
}

func (s *Server) sessionToolCatalog(ctx context.Context, sess *store.Session, turnID string, mode store.AgentMode, queries []string, namesOnly bool) (map[string]any, error) {
	sess.ActiveMode = mode
	defs, err := s.engine.ToolDefinitions(ctx, sess.ID, mode)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"session": sess.Title, "mode": mode, "activeTurnID": turnID}
	hasQuery := len(queries) > 0
	if !hasQuery || namesOnly {
		names := make([]string, 0, len(defs))
		for _, d := range defs {
			names = append(names, d.Name)
		}
		out["tools"] = names
		if namesOnly {
			return out, nil
		}
	}
	matches := []debugMCPTool{}
	plugins := []map[string]any{}
	needsProjectRules := !hasQuery
	var guideIDs []string
	endpointKinds := map[string]bool{}
	seenMatches := map[string]bool{}
	for _, q := range queries {
		candidates := defs
		if i := slices.IndexFunc(defs, func(d provider.ToolDef) bool { return strings.EqualFold(d.Name, q) }); i >= 0 {
			candidates = defs[i : i+1]
		}
		terms := strings.Fields(q)
		for _, d := range candidates {
			searchable := strings.ToLower(d.Name + " " + d.Description)
			if slices.ContainsFunc(terms, func(term string) bool { return !strings.Contains(searchable, term) }) {
				continue
			}
			if seenMatches[d.Name] || len(matches) >= 30 {
				continue
			}
			seenMatches[d.Name] = true
			matches = append(matches, debugMCPTool{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
			if !needsProjectRules && mode == store.ModeCode && engine.IsExternalCodeTool(d) {
				needsProjectRules = true
			}
			if kind := tool.PluginAPIEndpointKind(d.Name); kind != "" {
				endpointKinds[kind] = true
			}
			id := d.PluginID
			if id == "" {
				id, _ = tool.BuiltinPluginIDForTool(d.Name)
			}
			if id != "" && !slices.Contains(guideIDs, id) {
				guideIDs = append(guideIDs, id)
			}
		}
	}
	if s.plugins != nil && (!hasQuery || len(endpointKinds) > 0) {
		list, err := s.plugins.ListDefinitions(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			if p == nil || !p.Enabled {
				continue
			}
			loaded := slices.Contains(sess.LoadedPluginIDs, p.ID)
			if !hasQuery {
				plugins = append(plugins, map[string]any{"id": p.ID, "description": p.Description, "requiredMode": p.RequiredMode, "loaded": loaded})
			}
			if loaded && len(endpointKinds) > 0 && !slices.Contains(guideIDs, p.ID) {
				for _, endpoint := range p.Endpoints {
					if endpointKinds[endpoint.Kind] {
						guideIDs = append(guideIDs, p.ID)
						break
					}
				}
			}
		}
	}
	if hasQuery {
		out["matches"] = matches
	} else {
		out["plugins"] = plugins
	}
	if !hasQuery && s.skills != nil {
		skills, err := s.skills.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		out["skills"] = skills
	}
	if !hasQuery {
		guideIDs = sess.LoadedPluginIDs
	}
	guides, err := s.engine.ExternalGuides(ctx, sess, guideIDs, nil, !hasQuery)
	if err != nil {
		return nil, err
	}
	if needsProjectRules {
		rules, err := s.engine.ExternalProjectRules(ctx, sess, turnID)
		if err != nil {
			return nil, err
		}
		if rules != "" {
			out["projectInstructions"] = rules
		}
	}
	out["instructions"] = guides
	return out, nil
}

func (s *Server) failAgentHandoff(c *cart.Context, err error) error {
	if errors.Is(err, store.ErrTurnRunning) {
		c.JSON(http.StatusConflict, map[string]string{"error": "Wait for the current task to finish or stop it in Pudding, then retry the agent handoff."})
		return nil
	}
	return s.failAgent(c, err)
}

func (s *Server) failAgent(c *cart.Context, err error) error {
	if errors.Is(err, engine.ErrTurnNotActive) {
		c.JSON(http.StatusConflict, map[string]string{
			"error": "Turn is not active (it may have ended or timed out). Check pudding_session_status or start a new turn with pudding_turn_begin.",
		})
		return nil
	}
	if errors.Is(err, store.ErrTurnRunning) {
		c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
		return nil
	}
	return s.fail(c, err)
}

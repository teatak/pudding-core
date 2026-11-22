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
		return s.failAgent(c, err)
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
		credential := rand.Text()
		b, err := s.store.RedeemAgentInvite(ctx, agentHash(in.Invite), agentHash(credential), identity, time.Now().UnixMilli())
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusNotFound, map[string]string{"error": "Invitation expired, already redeemed, or unavailable. Create a new invitation in Pudding."})
			return nil
		}
		if err != nil {
			return s.failAgent(c, err)
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
		catalog, err := s.agentCatalog(ctx, sess, b, in.Query)
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
		rules, err := s.engine.ExternalProjectRules(ctx, sess, "")
		if err != nil {
			return s.failAgent(c, err)
		}
		id, err := s.engine.BeginExternalTurn(ctx, hash, task)
		if err != nil {
			return s.failAgent(c, err)
		}
		c.JSON(200, map[string]any{"turnID": id, "session": sess.Title, "mode": sess.ActiveMode, "projectInstructions": rules})
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
		result := reply.Result
		if _, ok := tool.ReferencedSkill(result.Name, result.Ok, result.Content); ok {
			sess, err = s.store.GetSession(ctx, b.SessionID)
			if err != nil {
				return s.failAgent(c, err)
			}
			result, err = s.engine.ExternalSkillResult(ctx, sess.ID, reply.Mode, result)
			if err != nil {
				return s.failAgent(c, err)
			}
		}
		out, err := s.externalMCPToolResult(b.SessionID, result)
		if err != nil {
			return s.failAgent(c, err)
		}
		for _, message := range reply.UserMessages {
			out.Content = append(out.Content, map[string]any{"type": "text", "text": "User in Pudding: " + message})
		}
		if reply.RefreshProjectRules {
			sess.ActiveMode = reply.Mode
			rules, err := s.engine.ExternalProjectRules(ctx, sess, in.TurnID)
			if err != nil {
				rules = "Could not read project instructions: " + err.Error()
			}
			if rules != "" {
				out.Content = append(out.Content, map[string]any{"type": "text", "text": rules})
			}
		}
		var guides map[string]json.RawMessage
		if reply.PluginID != "" || len(reply.SkillRefs) > 0 {
			var plugins []string
			if reply.PluginID != "" {
				plugins = []string{reply.PluginID}
			}
			sess.ActiveMode = reply.Mode
			guides, err = s.engine.ExternalGuides(ctx, sess, plugins, reply.SkillRefs)
			if err != nil {
				// The tool may have already made changes. Preserve its outcome.
				out.Content = append(out.Content, map[string]any{"type": "text", "text": "Could not refresh skill instructions: " + err.Error()})
			}
		}
		c.JSON(200, struct {
			debugMCPResult
			Instructions map[string]json.RawMessage `json:"instructions,omitempty"`
		}{out, guides})
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

func (s *Server) agentCatalog(ctx context.Context, sess *store.Session, b *store.ExternalAgentBinding, query string) (map[string]any, error) {
	turnID, mode := s.engine.ExternalTurnContext(b, sess.ActiveMode)
	sess.ActiveMode = mode
	defs, err := s.engine.ToolDefinitions(ctx, sess.ID, mode)
	if err != nil {
		return nil, err
	}
	names := []string{}
	matches := []debugMCPTool{}
	plugins := []map[string]any{}
	q := strings.ToLower(strings.TrimSpace(query))
	if q != "" {
		if i := slices.IndexFunc(defs, func(d provider.ToolDef) bool { return strings.EqualFold(d.Name, q) }); i >= 0 {
			defs = defs[i : i+1]
		}
	}
	var guideIDs []string
	for _, d := range defs {
		names = append(names, d.Name)
		if q == "" || !strings.Contains(strings.ToLower(d.Name+" "+d.Description), q) {
			continue
		}
		if len(matches) >= 30 {
			continue
		}
		matches = append(matches, debugMCPTool{Name: d.Name, Description: d.Description, InputSchema: d.InputSchema})
		id := d.PluginID
		if id == "" {
			id, _ = tool.BuiltinPluginIDForTool(d.Name)
		}
		if id != "" && !slices.Contains(guideIDs, id) {
			guideIDs = append(guideIDs, id)
		}
	}
	if s.plugins != nil {
		list, err := s.plugins.ListDefinitions(ctx)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			if p != nil && p.Enabled {
				plugins = append(plugins, map[string]any{"id": p.ID, "description": p.Description, "requiredMode": p.RequiredMode, "loaded": slices.Contains(sess.LoadedPluginIDs, p.ID)})
			}
		}
	}
	out := map[string]any{"session": sess.Title, "mode": sess.ActiveMode, "tools": names, "matches": matches, "plugins": plugins, "activeTurnID": turnID}
	if s.skills != nil {
		skills, err := s.skills.ListSkills(ctx)
		if err != nil {
			return nil, err
		}
		out["skills"] = skills
	}
	if q == "" {
		guideIDs = sess.LoadedPluginIDs
	}
	guides, err := s.engine.ExternalGuides(ctx, sess, guideIDs, nil)
	if err != nil {
		return nil, err
	}
	if q == "" {
		rules, err := s.engine.ExternalProjectRules(ctx, sess, turnID)
		if err != nil {
			return nil, err
		}
		out["projectInstructions"] = rules
	}
	out["instructions"] = guides
	return out, nil
}

func (s *Server) failAgent(c *cart.Context, err error) error {
	if errors.Is(err, engine.ErrTurnNotActive) || errors.Is(err, store.ErrTurnRunning) {
		c.JSON(http.StatusConflict, map[string]string{"error": err.Error()})
		return nil
	}
	return s.fail(c, err)
}

package contextbuilder

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/teatak/pudding-core/internal/app"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type AppSkillSource interface {
	ListDefinitions(context.Context) ([]*app.Definition, error)
	ReadSkill(context.Context, string, string) (*app.SkillDetail, error)
}

type SkillSource interface {
	ReadSkill(context.Context, string) (*skill.Document, error)
}

func WithSkillSources(apps AppSkillSource, skills SkillSource) Option {
	return func(b *Builder) { b.apps, b.skills = apps, skills }
}

// ResolveSkillReferences is the last context projection before sending a model
// request (including its current-turn tool results). It never mutates the
// canonical history, the unresolved request, or a provider's prior request.
func (b *Builder) ResolveSkillReferences(ctx context.Context, sessionID, mode string, req provider.Request) (provider.Request, error) {
	type position struct{ message, part int }
	latest := make(map[string]position)
	hasApp := false
	for mi, message := range req.Messages {
		for pi, part := range message.Parts {
			if part.Type != provider.PartToolResult {
				continue
			}
			if ref, ok := tool.ReferencedSkill(part.Name, part.Ok, part.Content); ok {
				latest[ref.Key()] = position{mi, pi}
				hasApp = hasApp || ref.Kind == tool.AppSkillReference
			}
		}
	}
	if len(latest) == 0 {
		return req, nil
	}
	loaded := make(map[string]bool)
	definitions := make(map[string]*app.Definition)
	var appError error
	if hasApp {
		session, err := b.store.GetSession(ctx, sessionID)
		if err != nil {
			return provider.Request{}, err
		}
		for _, id := range session.LoadedAppIDs {
			loaded[id] = true
		}
		if b.apps == nil {
			appError = errors.New("App skill source is unavailable")
		} else {
			var list []*app.Definition
			list, appError = b.apps.ListDefinitions(ctx)
			for _, definition := range list {
				if definition != nil {
					definitions[definition.ID] = definition
				}
			}
		}
	}
	currentMode := store.NormalizeAgentMode(store.AgentMode(mode))
	req.Messages = append([]provider.Message(nil), req.Messages...)
	for mi := range req.Messages {
		message := &req.Messages[mi]
		message.Parts = append([]provider.Part(nil), message.Parts...)
		for pi := range message.Parts {
			part := &message.Parts[pi]
			if part.Type != provider.PartToolResult {
				continue
			}
			ref, found := tool.ReferencedSkill(part.Name, part.Ok, part.Content)
			if !found {
				continue
			}
			fields := map[string]any{"instructionStatus": "current"}
			var err error
			switch {
			case latest[ref.Key()] != (position{mi, pi}):
				fields["instructionStatus"] = "superseded"
			case strings.TrimSpace(ref.SkillID) == "":
				err = errors.New("skill reference has no registered skill ID")
			case part.Name == tool.AppLoad && ref.Kind == tool.AppSkillReference && ref.AppID != "":
				definition := definitions[ref.AppID]
				switch {
				case !loaded[ref.AppID]:
					fields["instructionStatus"] = "unloaded"
				case appError != nil:
					err = appError
				case definition == nil || !definition.Enabled:
					err = fmt.Errorf("App %q is disabled or unavailable in this runtime", ref.AppID)
				default:
					required := store.NormalizeAgentMode(store.AgentMode(definition.RequiredMode))
					if required == "" {
						required = store.ModeWork
					}
					if store.AgentModeRank(currentMode) < store.AgentModeRank(required) {
						fields["instructionStatus"] = "capability_required"
					} else {
						var doc *app.SkillDetail
						doc, err = b.apps.ReadSkill(ctx, ref.AppID, ref.SkillID)
						if err == nil && doc == nil {
							err = errors.New("App skill source returned no document")
						}
						if err == nil {
							fields["content"], fields["name"], fields["description"], fields["path"] = doc.Content, doc.Name, doc.Description, doc.Path
						}
					}
				}
			case part.Name == tool.SkillRead && ref.Kind == tool.GlobalSkillReference && ref.AppID == "":
				if b.skills == nil {
					err = errors.New("global skill source is unavailable")
				} else {
					var doc *skill.Document
					doc, err = b.skills.ReadSkill(ctx, ref.SkillID)
					if err == nil && doc == nil {
						err = errors.New("skill source returned no document")
					}
					if err == nil {
						fields["content"], fields["name"], fields["description"], fields["path"] = doc.Content, doc.Name, doc.Description, doc.Path
						fields["scope"], fields["source"] = doc.Scope, doc.Source
					}
				}
			default:
				err = errors.New("invalid skill reference for this tool")
			}
			if ctx.Err() != nil {
				return provider.Request{}, ctx.Err()
			}
			if err != nil {
				part.Ok = false
				fields["ok"] = false
				fields["instructionStatus"] = "unavailable"
				fields["instructionError"] = err.Error()
			}
			if part.Name == tool.AppLoad {
				fields["instructionsLoaded"] = fields["instructionStatus"] == "current"
			}
			part.Content = tool.SkillReferencePayload(part.Content, ref, fields)
		}
	}
	return req, nil
}

// Keep the latest reference per global skill / App even when its original turn
// was compacted. Canonical messages remain the sole source, including for old
// summaries that did not explicitly preserve references. Only the original
// call/result pair is retained, never the surrounding old conversation.
func messagesWithSkillReferences(all, effective []*store.Message, loadedAppIDs []string) []*store.Message {
	if len(all) == len(effective) {
		return effective
	}
	loaded := make(map[string]bool)
	for _, id := range loadedAppIDs {
		loaded[id] = true
	}
	type entry struct {
		message *store.Message
		part    store.ContentPart
		call    store.ContentPart
	}
	latest := make(map[string]entry)
	calls := make(map[string]store.ContentPart)
	for _, message := range all {
		if message.Role != store.RoleAssistant && message.Role != store.RoleTool {
			continue
		}
		for _, part := range message.Parts {
			callKey := message.TurnID + ":" + part.CallID
			if part.Type == store.ContentPartToolUse {
				calls[callKey] = part
			}
			if part.Type != store.ContentPartToolResult {
				continue
			}
			if ref, ok := tool.ReferencedSkill(part.Name, part.Ok, part.Content); ok {
				if ref.Kind == tool.AppSkillReference && !loaded[ref.AppID] {
					continue
				}
				latest[ref.Key()] = entry{message, part, calls[callKey]}
			}
		}
	}
	visible := make(map[string]bool)
	for _, message := range effective {
		visible[message.ID] = true
	}
	var retained []*store.Message
	// Preserve source ordering, independently of map iteration order.
	for _, message := range all {
		if visible[message.ID] {
			continue
		}
		for _, part := range message.Parts {
			if part.Type != store.ContentPartToolResult {
				continue
			}
			ref, ok := tool.ReferencedSkill(part.Name, part.Ok, part.Content)
			if !ok {
				continue
			}
			item, ok := latest[ref.Key()]
			if !ok || item.message.ID != message.ID || item.part.CallID != part.CallID || item.call.Type != store.ContentPartToolUse {
				continue
			}
			part.Content = tool.SkillReferenceOnly(part.Name, part.Ok, part.Content)
			retained = append(retained, &store.Message{ID: message.ID, SessionID: message.SessionID, TurnID: message.TurnID, Role: store.RoleAssistant,
				Parts: []store.ContentPart{item.call, part}})
		}
	}
	if len(retained) == 0 {
		return effective
	}
	// The summary is a task-history record. Current instructions follow it.
	out := append([]*store.Message(nil), effective[:1]...)
	out = append(out, retained...)
	return append(out, effective[1:]...)
}

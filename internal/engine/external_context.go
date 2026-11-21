package engine

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/sessionworkspace"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

func (e *Engine) ExternalProjectRules(ctx context.Context, sess *store.Session, turnID string) (string, error) {
	if sess.ActiveMode != store.ModeCode {
		return "", nil
	}
	var workspace sessionworkspace.Workspace
	var err error
	if turnID == "" {
		// Discovery outside our own turn inherits only persistent directory grants.
		workspace, err = sessionworkspace.Resolve(ctx, e.store, e.attachmentHome, sess.ID, sessionworkspace.ScratchExisting)
	} else {
		workspace, err = e.SessionWorkspace(ctx, sess.ID, turnID, sessionworkspace.ScratchExisting)
	}
	if err != nil {
		return "", err
	}
	return contextbuilder.AppendProjectContext("", workspace.RootDirs), nil
}

// Track selections, never file contents. A running turn can reuse these references
// while resolving their current bodies from disk or the bound desktop runtime.
func (e *Engine) externalSkillRefs(ctx context.Context, sessionID string) (map[string]tool.SkillReference, error) {
	messages, err := e.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	refs := make(map[string]tool.SkillReference)
	for _, message := range store.SubagentContextMessages(messages) {
		for _, p := range message.Parts {
			if p.Type == store.ContentPartToolResult {
				if ref, ok := tool.ReferencedSkill(p.Name, p.Ok, p.Content); ok {
					refs[ref.Key()] = ref
				}
			}
		}
	}
	return refs, nil
}

func (e *Engine) ExternalGuides(ctx context.Context, sess *store.Session, plugins []string, refs map[string]tool.SkillReference) (map[string]json.RawMessage, error) {
	var err error
	if refs == nil {
		refs, err = e.externalSkillRefs(ctx, sess.ID)
		if err != nil {
			return nil, err
		}
	}
	selected := make(map[string]tool.SkillReference)
	for key, ref := range refs {
		if ref.Kind == tool.GlobalSkillReference {
			selected[key] = ref
		}
	}
	if len(plugins) > 0 && e.plugins != nil {
		defs, err := e.plugins.ListDefinitions(ctx)
		if err != nil {
			return nil, err
		}
		for _, d := range defs {
			if d == nil || !d.Enabled || !slices.Contains(plugins, d.ID) || !slices.Contains(sess.LoadedPluginIDs, d.ID) {
				continue
			}
			ref := tool.SkillReference{Kind: tool.PluginSkillReference, PluginID: d.ID, SkillID: defaultPluginSkillID(d)}
			if saved, ok := refs[ref.Key()]; ok {
				ref = saved
			}
			if ref.SkillID != "" {
				selected[ref.Key()] = ref
			}
		}
	}
	guides := make(map[string]json.RawMessage, len(selected))
	if len(selected) == 0 {
		return guides, nil
	}
	keys := make([]string, 0, len(selected))
	parts := make([]provider.Part, 0, len(selected))
	for key, ref := range selected {
		name := tool.SkillRead
		if ref.Kind == tool.PluginSkillReference {
			name = tool.PluginLoad
		}
		keys = append(keys, key)
		parts = append(parts, provider.Part{Type: provider.PartToolResult, Name: name, Ok: true, Content: tool.SkillReferencePayload("{}", ref, nil)})
	}
	req := provider.Request{Messages: []provider.Message{{Parts: parts}}}
	live, err := e.builder.ResolveSkillReferences(ctx, sess.ID, string(sess.ActiveMode), req)
	if err != nil {
		return nil, err
	}
	for i, part := range live.Messages[0].Parts {
		guides[keys[i]] = json.RawMessage(part.Content)
	}
	return guides, nil
}

// Package contextbuilder 把 canonical messages 组装为 provider 输入。
// context 只来自 canonical messages(AGENTS.md 硬约束 8);thought 只给用户历史回看,
// 跨 turn 组装时剥离,不把陈旧推理喂回模型。
package contextbuilder

import (
	"context"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const defaultSystemPrompt = "You are Pudding, a helpful local-first assistant."

type Builder struct {
	store    store.Store
	settings SettingsSource
}

type SettingsSource interface {
	Settings(ctx context.Context) (map[string]string, error)
}

func New(s store.Store, settings SettingsSource) *Builder {
	return &Builder{store: s, settings: settings}
}

// Build 在 user message 已落库之后调用,因此 current input 已包含在
// canonical messages 里,不需要单独拼接。
func (b *Builder) Build(ctx context.Context, sessionID, model string) (provider.Request, error) {
	msgs, err := b.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return provider.Request{}, err
	}
	settings, err := b.settings.Settings(ctx)
	if err != nil {
		return provider.Request{}, err
	}
	system := settings[store.SettingSystemPrompt]
	if system == "" {
		system = defaultSystemPrompt
	}
	req := provider.Request{
		Model:    model,
		System:   system,
		Messages: make([]provider.Message, 0, len(msgs)),
	}
	for _, m := range msgs {
		var role provider.Role
		switch m.Role {
		case store.RoleUser:
			role = provider.RoleUser
		case store.RoleAssistant:
			role = provider.RoleAssistant
		default:
			continue
		}
		req.Messages = append(req.Messages, provider.Message{Role: role, Text: m.Text, Parts: providerParts(m.Parts)})
	}
	return req, nil
}

func providerParts(parts []store.ContentPart) []provider.Part {
	out := make([]provider.Part, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case store.ContentPartText:
			if part.Text != "" {
				out = append(out, provider.Part{Type: provider.PartText, Text: part.Text})
			}
		case store.ContentPartThought:
			continue
		case store.ContentPartToolUse:
			out = append(out, provider.Part{
				Type:   provider.PartToolUse,
				CallID: part.CallID,
				Name:   part.Name,
				Args:   append([]byte(nil), part.Args...),
			})
		case store.ContentPartToolResult:
			out = append(out, provider.Part{
				Type:    provider.PartToolResult,
				CallID:  part.CallID,
				Ok:      part.Ok,
				Content: part.Content,
			})
		}
	}
	return out
}

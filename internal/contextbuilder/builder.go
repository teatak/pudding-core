// Package contextbuilder 把 canonical messages 组装为 provider 输入。
// context 只来自 canonical messages(AGENTS.md 硬约束 8);第一阶段 text-only、
// 不做裁剪与 compact,产物形状先行(docs/technology-decisions.md 第 6 节)。
package contextbuilder

import (
	"context"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

const defaultSystemPrompt = "You are Pudding, a helpful local-first assistant."

type Builder struct {
	store store.Store
}

func New(s store.Store) *Builder { return &Builder{store: s} }

// Build 在 user message 已落库之后调用,因此 current input 已包含在
// canonical messages 里,不需要单独拼接。
func (b *Builder) Build(ctx context.Context, sessionID, model string) (provider.Request, error) {
	msgs, err := b.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return provider.Request{}, err
	}
	settings, err := b.store.Settings(ctx)
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
		req.Messages = append(req.Messages, provider.Message{Role: role, Text: m.Text})
	}
	return req, nil
}

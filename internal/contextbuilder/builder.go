// Package contextbuilder 把 canonical messages 组装为 provider 输入。
// context 只来自 canonical messages(AGENTS.md 硬约束 8);thought 只给用户历史回看,
// 跨 turn 组装时剥离,不把陈旧推理喂回模型。
package contextbuilder

import (
	"context"

	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

type Builder struct {
	store   store.Store
	prompts PromptSource
}

type PromptSource interface {
	Prompt(ctx context.Context) (prompt.Output, error)
}

func New(s store.Store, prompts PromptSource) *Builder {
	if prompts == nil {
		prompts = staticPrompt{}
	}
	return &Builder{store: s, prompts: prompts}
}

// Build 在 user message 已落库之后调用,因此 current input 已包含在
// canonical messages 里,不需要单独拼接。
func (b *Builder) Build(ctx context.Context, sessionID, model string) (provider.Request, error) {
	msgs, err := b.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return provider.Request{}, err
	}
	system, err := b.prompts.Prompt(ctx)
	if err != nil {
		return provider.Request{}, err
	}
	req := provider.Request{
		Model:    model,
		System:   system.SystemInstruction,
		Messages: make([]provider.Message, 0, len(msgs)),
	}
	var assistantParts []provider.Part
	flushAssistant := func() {
		if len(assistantParts) == 0 {
			return
		}
		req.Messages = append(req.Messages, provider.Message{
			Role:  provider.RoleAssistant,
			Text:  textFromProviderParts(assistantParts),
			Parts: cloneProviderParts(assistantParts),
		})
		assistantParts = nil
	}
	for _, m := range msgs {
		switch m.Role {
		case store.RoleUser:
			flushAssistant()
			parts := providerParts(m.Parts)
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleUser, Text: textFromProviderParts(parts), Parts: parts})
		case store.RoleAssistant, store.RoleTool:
			assistantParts = append(assistantParts, providerParts(m.Parts)...)
		case store.RoleSummary:
			flushAssistant()
			parts := providerParts(m.Parts)
			if len(parts) > 0 {
				req.Messages = append(req.Messages, provider.Message{Role: provider.RoleAssistant, Text: textFromProviderParts(parts), Parts: parts})
			}
		default:
			continue
		}
	}
	flushAssistant()
	return req, nil
}

type staticPrompt struct{}

func (staticPrompt) Prompt(context.Context) (prompt.Output, error) {
	return prompt.Assemble(prompt.Input{}), nil
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
				Name:    part.Name,
				Ok:      part.Ok,
				Content: part.Content,
			})
		}
	}
	return out
}

func textFromProviderParts(parts []provider.Part) string {
	var text string
	for _, part := range parts {
		if part.Type == provider.PartText {
			text += part.Text
		}
	}
	return text
}

func cloneProviderParts(parts []provider.Part) []provider.Part {
	if len(parts) == 0 {
		return nil
	}
	out := make([]provider.Part, 0, len(parts))
	for _, part := range parts {
		cp := part
		if part.Args != nil {
			cp.Args = append([]byte(nil), part.Args...)
		}
		out = append(out, cp)
	}
	return out
}

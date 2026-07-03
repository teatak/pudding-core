// Package contextbuilder 把 canonical messages 组装为 provider 输入。
// context 只来自 canonical messages(AGENTS.md 硬约束 8);thought 只给用户历史回看,
// 跨 turn 组装时剥离,不把陈旧推理喂回模型。
package contextbuilder

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/prompt"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type Builder struct {
	store          store.Store
	prompts        PromptSource
	attachmentHome string
}

type Option func(*Builder)

func WithAttachmentHome(home string) Option {
	return func(b *Builder) {
		b.attachmentHome = strings.TrimSpace(home)
	}
}

type PromptSource interface {
	Prompt(ctx context.Context, mode string) (prompt.Output, error)
}

func New(s store.Store, prompts PromptSource, opts ...Option) *Builder {
	if prompts == nil {
		prompts = staticPrompt{}
	}
	b := &Builder{store: s, prompts: prompts}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Build 在 user message 已落库之后调用,因此 current input 已包含在
// canonical messages 里,不需要单独拼接。
func (b *Builder) Build(ctx context.Context, sessionID, model string, mode string, configs ...provider.ModelConfig) (provider.Request, error) {
	msgs, err := b.store.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return provider.Request{}, err
	}
	msgs = EffectiveMessages(msgs)
	system, err := b.prompts.Prompt(ctx, mode)
	if err != nil {
		return provider.Request{}, err
	}
	currentMode := store.NormalizeAgentMode(store.AgentMode(mode))
	if currentMode == "" {
		currentMode = store.ModeChat
	}
	var cfg provider.ModelConfig
	if len(configs) > 0 {
		cfg = configs[0]
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
			parts := b.providerParts(sessionID, m.Parts, currentMode, cfg)
			req.Messages = append(req.Messages, provider.Message{Role: provider.RoleUser, Text: textFromProviderParts(parts), Parts: parts})
		case store.RoleSystem:
			flushAssistant()
			text := wrapSystemReminder(store.TextFromParts(m.Parts))
			if text != "" {
				req.Messages = append(req.Messages, provider.Message{
					Role:  provider.RoleUser,
					Text:  text,
					Parts: []provider.Part{{Type: provider.PartText, Text: text}},
				})
			}
		case store.RoleAssistant, store.RoleTool:
			assistantParts = append(assistantParts, b.providerParts(sessionID, m.Parts, currentMode, cfg)...)
		case store.RoleSummary:
			flushAssistant()
			parts := b.providerParts(sessionID, m.Parts, currentMode, cfg)
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

func wrapSystemReminder(text string) string {
	text = strings.TrimSpace(escapeSystemReminderText(text))
	if text == "" {
		return ""
	}
	return "<system-reminder>\n" + text + "\n</system-reminder>"
}

func escapeSystemReminderText(text string) string {
	replacer := strings.NewReplacer(
		"<system-reminder>", "<system-reminder escaped>",
		"</system-reminder>", "</system-reminder escaped>",
	)
	return replacer.Replace(text)
}

func EffectiveMessages(msgs []*store.Message) []*store.Message {
	compactIndex := -1
	for i, msg := range msgs {
		if _, ok := store.CompactMetadataFromMessage(msg); ok {
			compactIndex = i
		}
	}
	if compactIndex < 0 {
		return msgs
	}
	compactMsg := msgs[compactIndex]
	meta, _ := store.CompactMetadataFromMessage(compactMsg)
	byID := make(map[string]*store.Message, len(msgs))
	for _, msg := range msgs {
		byID[msg.ID] = msg
	}
	seen := make(map[string]bool, len(msgs)-compactIndex)
	out := make([]*store.Message, 0, 1+len(meta.TailMessageIDs)+len(msgs)-compactIndex-1)
	appendMsg := func(msg *store.Message) {
		if msg == nil || seen[msg.ID] {
			return
		}
		seen[msg.ID] = true
		out = append(out, msg)
	}
	appendMsg(compactMsg)
	for _, id := range meta.TailMessageIDs {
		appendMsg(byID[id])
	}
	for _, msg := range msgs[compactIndex+1:] {
		appendMsg(msg)
	}
	return out
}

func SplitRecentInputTail(msgs []*store.Message, recentInputTurns int) ([]*store.Message, []*store.Message) {
	if recentInputTurns <= 0 || len(msgs) == 0 {
		return msgs, nil
	}
	split := 0
	seenInputs := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		if !IsInputTurnBoundary(msgs[i]) {
			continue
		}
		seenInputs++
		if seenInputs == recentInputTurns {
			split = i
			break
		}
	}
	if seenInputs < recentInputTurns {
		return nil, msgs
	}
	return msgs[:split], msgs[split:]
}

func IsInputTurnBoundary(msg *store.Message) bool {
	if msg == nil || msg.TurnID == "" {
		return false
	}
	return msg.Role == store.RoleUser || msg.Role == store.RoleSystem
}

type staticPrompt struct{}

func (staticPrompt) Prompt(_ context.Context, mode string) (prompt.Output, error) {
	return prompt.Assemble(prompt.Input{Mode: mode}), nil
}

func (b *Builder) providerParts(sessionID string, parts []store.ContentPart, mode store.AgentMode, cfg provider.ModelConfig) []provider.Part {
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
			if !tool.NameAllowedForMode(mode, part.Name) {
				continue
			}
			out = append(out, provider.Part{
				Type:   provider.PartToolUse,
				CallID: part.CallID,
				Name:   part.Name,
				Args:   append([]byte(nil), part.Args...),
			})
		case store.ContentPartToolResult:
			if !tool.NameAllowedForMode(mode, part.Name) {
				continue
			}
			out = append(out, provider.Part{
				Type:    provider.PartToolResult,
				CallID:  part.CallID,
				Name:    part.Name,
				Ok:      part.Ok,
				Content: part.Content,
			})
		case store.ContentPartAttachment:
			if imagePart, ok := b.imageProviderPart(sessionID, part, cfg); ok {
				out = append(out, imagePart)
			} else if audioPart, ok := b.audioProviderPart(sessionID, part, cfg); ok {
				out = append(out, audioPart)
			} else if text := attachmentProviderText(part, b.attachmentToolPath(sessionID, part)); text != "" {
				out = append(out, provider.Part{Type: provider.PartText, Text: text})
			}
		}
	}
	return out
}

func (b *Builder) imageProviderPart(sessionID string, part store.ContentPart, cfg provider.ModelConfig) (provider.Part, bool) {
	if !imageAttachmentsAllowed(cfg) || strings.TrimSpace(b.attachmentHome) == "" {
		return provider.Part{}, false
	}
	mime := strings.ToLower(strings.TrimSpace(part.MIME))
	if !strings.HasPrefix(mime, "image/") || mime == "image/svg+xml" {
		return provider.Part{}, false
	}
	path, ok, err := attachment.NewService(b.attachmentHome).Path(sessionID, part.AttachmentKey)
	if err != nil || !ok {
		return provider.Part{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return provider.Part{}, false
	}
	return provider.Part{Type: provider.PartImage, MIME: mime, Data: data}, true
}

func (b *Builder) audioProviderPart(sessionID string, part store.ContentPart, cfg provider.ModelConfig) (provider.Part, bool) {
	if !audioAttachmentsAllowed(cfg) || strings.TrimSpace(b.attachmentHome) == "" {
		return provider.Part{}, false
	}
	mime := strings.ToLower(strings.TrimSpace(part.MIME))
	if !strings.HasPrefix(mime, "audio/") {
		return provider.Part{}, false
	}
	path, ok, err := attachment.NewService(b.attachmentHome).Path(sessionID, part.AttachmentKey)
	if err != nil || !ok {
		return provider.Part{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return provider.Part{}, false
	}
	return provider.Part{Type: provider.PartAudio, MIME: mime, Data: data}, true
}

func (b *Builder) attachmentToolPath(sessionID string, part store.ContentPart) string {
	if strings.TrimSpace(b.attachmentHome) == "" {
		return ""
	}
	path, ok, err := attachment.NewService(b.attachmentHome).Path(sessionID, part.AttachmentKey)
	if err != nil || !ok {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

func imageAttachmentsAllowed(cfg provider.ModelConfig) bool {
	return cfg.Capabilities == nil || cfg.Capabilities.Image
}

func audioAttachmentsAllowed(cfg provider.ModelConfig) bool {
	return cfg.Capabilities != nil && cfg.Capabilities.Audio
}

func attachmentProviderText(part store.ContentPart, toolPath string) string {
	var b strings.Builder
	b.WriteString("[Attachment]\n")
	if part.Name != "" {
		b.WriteString("Name: ")
		b.WriteString(part.Name)
		b.WriteByte('\n')
	}
	if part.MIME != "" {
		b.WriteString("MIME: ")
		b.WriteString(part.MIME)
		b.WriteByte('\n')
	}
	if part.Size > 0 {
		b.WriteString("Size bytes: ")
		b.WriteString(strconv.FormatInt(part.Size, 10))
		b.WriteByte('\n')
	}
	if toolPath != "" {
		b.WriteString("Local path for tools: ")
		b.WriteString(toolPath)
		b.WriteByte('\n')
	}
	if part.AudioTranscript != "" {
		b.WriteString("Audio transcript: ")
		b.WriteString(part.AudioTranscript)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
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
		if part.Data != nil {
			cp.Data = append([]byte(nil), part.Data...)
		}
		out = append(out, cp)
	}
	return out
}

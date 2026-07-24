// Package contextbuilder 把 canonical messages 组装为 provider 输入。
// context 只来自 canonical messages(AGENTS.md 硬约束 8);thought 只给用户历史回看,
// 跨 turn 组装时剥离,不把陈旧推理喂回模型。
package contextbuilder

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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

type loadedAppsPromptSource interface {
	PromptWithLoadedApps(ctx context.Context, mode string, loadedAppIDs []string) (prompt.Output, error)
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
	sess, err := b.store.GetSession(ctx, sessionID)
	if err != nil {
		return provider.Request{}, err
	}
	currentMode := store.NormalizeAgentMode(store.AgentMode(mode))
	if currentMode == "" {
		currentMode = store.ModeChat
	}
	var system prompt.Output
	if source, ok := b.prompts.(loadedAppsPromptSource); ok {
		system, err = source.PromptWithLoadedApps(ctx, mode, sess.LoadedAppIDs)
	} else {
		system, err = b.prompts.Prompt(ctx, mode)
	}
	if err != nil {
		return provider.Request{}, err
	}
	if currentMode == store.ModeCode && strings.TrimSpace(sess.ProjectID) != "" {
		project, err := b.store.GetProject(ctx, sess.ProjectID)
		if err != nil {
			return provider.Request{}, err
		}
		projectDirs := store.NormalizeProjectDirs(project.RootDirs)
		system = appendProjectDirectories(system, projectDirs)
		system = appendProjectInstructions(system, loadProjectRootInstructions(projectDirs))
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
			if isToolAttachmentMessage(m) {
				flushAssistant()
				parts := b.providerParts(sessionID, m.Parts, currentMode, cfg)
				if len(parts) > 0 {
					req.Messages = append(req.Messages, provider.Message{Role: provider.RoleUser, Text: textFromProviderParts(parts), Parts: parts})
				}
				continue
			}
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

func appendProjectDirectories(system prompt.Output, projectDirs []string) prompt.Output {
	projectDirs = store.NormalizeProjectDirs(projectDirs)
	if len(projectDirs) == 0 {
		return system
	}
	var content strings.Builder
	content.WriteString("## Project Directories\n\n")
	content.WriteString("The current Project authorizes every directory below as a distinct root. Consider all roots when inspecting or changing the Project. When multiple roots are listed, use the absolute path to select the intended root; do not assume the first root is the whole Project.\n")
	for index, projectDir := range projectDirs {
		encoded, _ := json.Marshal(projectDir)
		content.WriteString("\n")
		content.WriteString(strconv.Itoa(index + 1))
		content.WriteString(". ")
		content.Write(encoded)
	}
	segment := prompt.Segment{ID: "project_directories", Layer: "project", Content: strings.TrimSpace(content.String())}
	system.Segments = append(system.Segments, segment)
	if existing := strings.TrimSpace(system.SystemInstruction); existing != "" {
		system.SystemInstruction = existing + "\n\n" + segment.Content
	} else {
		system.SystemInstruction = segment.Content
	}
	return system
}

func appendProjectInstructions(system prompt.Output, instructions []projectInstructionContext) prompt.Output {
	if len(instructions) == 0 {
		return system
	}
	var content strings.Builder
	content.WriteString("## Project Instructions\n\n")
	content.WriteString("These root-level files were loaded from the authorized Project before this Code turn. Follow them for work in the corresponding root. More specific instruction files in child directories override them within their directory scope.\n")
	for _, instruction := range instructions {
		content.WriteString("\n### `")
		content.WriteString(instruction.projectRoot)
		content.WriteString("/" + instruction.path + "`\n\n")
		content.WriteString(strings.TrimSpace(instruction.content))
		if instruction.truncated {
			content.WriteString("\n\n[Project instruction truncated by Pudding]")
		}
		content.WriteByte('\n')
	}
	segment := prompt.Segment{ID: "project_instructions", Layer: "project", Content: strings.TrimSpace(content.String())}
	system.Segments = append(system.Segments, segment)
	if existing := strings.TrimSpace(system.SystemInstruction); existing != "" {
		system.SystemInstruction = existing + "\n\n" + segment.Content
	} else {
		system.SystemInstruction = segment.Content
	}
	return system
}

func isToolAttachmentMessage(msg *store.Message) bool {
	if msg == nil || len(msg.Parts) != 1 {
		return false
	}
	part := msg.Parts[0]
	return part.Type == store.ContentPartAttachment && part.Origin == attachment.OriginTool
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
	if msg == nil || msg.TurnID == "" || msg.TurnIndex != 0 {
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
	voiceAudioInline := false
	for _, part := range parts {
		if part.Type != store.ContentPartAttachment || part.Origin != attachment.OriginVoiceAudio {
			continue
		}
		if _, ok := b.audioProviderPart(sessionID, part, cfg); ok {
			voiceAudioInline = true
			break
		}
	}
	localFolders := make([]store.LocalFolder, 0)
	projectReferences := make([]store.ProjectReference, 0)
	flushLocalFolders := func() {
		if text := localFoldersProviderText(localFolders); text != "" {
			out = append(out, provider.Part{Type: provider.PartText, Text: text})
		}
		localFolders = localFolders[:0]
	}
	flushProjectReferences := func() {
		if text := projectReferencesProviderText(projectReferences); text != "" {
			out = append(out, provider.Part{Type: provider.PartText, Text: text})
		}
		projectReferences = projectReferences[:0]
	}
	flushReferences := func() {
		flushLocalFolders()
		flushProjectReferences()
	}
	for _, part := range parts {
		switch part.Type {
		case store.ContentPartText:
			flushReferences()
			if part.Text != "" && !voiceAudioInline {
				out = append(out, provider.Part{Type: provider.PartText, Text: part.Text})
			}
		case store.ContentPartThought:
			continue
		case store.ContentPartToolUse:
			flushReferences()
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
			flushReferences()
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
			if part.Origin == attachment.OriginASRAudio {
				continue
			}
			flushReferences()
			if part.Origin == attachment.OriginVoiceAudio {
				if voiceAudioInline {
					if audioPart, ok := b.audioProviderPart(sessionID, part, cfg); ok {
						out = append(out, audioPart)
					}
				}
				continue
			}
			toolPath := b.attachmentToolPath(sessionID, part)
			if imagePart, ok := b.imageProviderPart(sessionID, part, cfg); ok {
				if text := attachmentProviderText(part, toolPath, "image"); text != "" {
					out = append(out, provider.Part{Type: provider.PartText, Text: text})
				}
				out = append(out, imagePart)
			} else if audioPart, ok := b.audioProviderPart(sessionID, part, cfg); ok {
				if text := attachmentProviderText(part, toolPath, "audio"); text != "" {
					out = append(out, provider.Part{Type: provider.PartText, Text: text})
				}
				out = append(out, audioPart)
			} else if text := attachmentProviderText(part, toolPath, ""); text != "" {
				out = append(out, provider.Part{Type: provider.PartText, Text: text})
			}
		case store.ContentPartLocalFolder:
			localFolders = append(localFolders, store.LocalFolder{
				ID:     part.CallID,
				Name:   part.Name,
				Path:   part.Path,
				Origin: part.Origin,
			})
		case store.ContentPartProjectRef:
			projectReferences = append(projectReferences, store.ProjectReference{
				ID:          part.CallID,
				Name:        part.Name,
				Path:        part.Path,
				SourcePath:  part.SourcePath,
				RootID:      part.RootID,
				Kind:        part.ResourceKind,
				StartLine:   part.StartLine,
				StartColumn: part.StartColumn,
				EndLine:     part.EndLine,
				EndColumn:   part.EndColumn,
			})
		case store.ContentPartUIContext:
			flushReferences()
			if text := uiContextProviderText(part); text != "" {
				out = append(out, provider.Part{Type: provider.PartText, Text: text})
			}
		}
	}
	flushReferences()
	return out
}

func uiContextProviderText(part store.ContentPart) string {
	type payload struct {
		Surface  string `json:"surface"`
		Resource string `json:"resource,omitempty"`
		ID       string `json:"id,omitempty"`
		Name     string `json:"name,omitempty"`
		Path     string `json:"path,omitempty"`
		URL      string `json:"url,omitempty"`
		Kind     string `json:"kind,omitempty"`
		RootID   string `json:"rootID,omitempty"`
	}
	if strings.TrimSpace(part.Surface) == "" {
		return ""
	}
	raw, err := json.Marshal(payload{
		Surface:  part.Surface,
		Resource: part.Resource,
		ID:       part.CallID,
		Name:     part.Name,
		Path:     part.Path,
		URL:      part.URL,
		Kind:     part.ResourceKind,
		RootID:   part.RootID,
	})
	if err != nil {
		return ""
	}
	return "<ui-context>" + string(raw) + "</ui-context>"
}

func (b *Builder) imageProviderPart(sessionID string, part store.ContentPart, cfg provider.ModelConfig) (provider.Part, bool) {
	if !imageAttachmentsAllowed(cfg) || strings.TrimSpace(b.attachmentHome) == "" {
		return provider.Part{}, false
	}
	mime := strings.ToLower(strings.TrimSpace(part.MIME))
	if !strings.HasPrefix(mime, "image/") || mime == "image/svg+xml" {
		return provider.Part{}, false
	}
	modelImage, err := attachment.NewService(b.attachmentHome).ModelImageForProvider(attachmentSessionID(sessionID, part), part.AttachmentKey, mime)
	if err != nil || len(modelImage.Data) == 0 {
		return provider.Part{}, false
	}
	return provider.Part{
		Type:   provider.PartImage,
		MIME:   modelImage.MIME,
		Data:   modelImage.Data,
		Width:  modelImage.Width,
		Height: modelImage.Height,
	}, true
}

func (b *Builder) audioProviderPart(sessionID string, part store.ContentPart, cfg provider.ModelConfig) (provider.Part, bool) {
	if !audioAttachmentsAllowed(cfg) || strings.TrimSpace(b.attachmentHome) == "" {
		return provider.Part{}, false
	}
	mime := strings.ToLower(strings.TrimSpace(part.MIME))
	if !strings.HasPrefix(mime, "audio/") {
		return provider.Part{}, false
	}
	path, ok, err := attachment.NewService(b.attachmentHome).Path(attachmentSessionID(sessionID, part), part.AttachmentKey)
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
	path, ok, err := attachment.NewService(b.attachmentHome).Path(attachmentSessionID(sessionID, part), part.AttachmentKey)
	if err != nil || !ok {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	return path
}

func attachmentSessionID(sessionID string, part store.ContentPart) string {
	if part.Origin == attachment.OriginTemp && strings.Contains(part.AttachmentKey, "/"+attachment.DraftSessionID+"/") {
		return attachment.DraftSessionID
	}
	return sessionID
}

func imageAttachmentsAllowed(cfg provider.ModelConfig) bool {
	return cfg.Capabilities != nil && cfg.Capabilities.Image
}

func audioAttachmentsAllowed(cfg provider.ModelConfig) bool {
	return cfg.Capabilities != nil && cfg.Capabilities.Audio
}

func attachmentProviderText(part store.ContentPart, toolPath, mediaKind string) string {
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
	if part.AttachmentKey != "" {
		b.WriteString("attachmentKey: ")
		b.WriteString(part.AttachmentKey)
		b.WriteByte('\n')
	}
	if part.URL != "" {
		b.WriteString("displayURL (UI only): ")
		b.WriteString(part.URL)
		b.WriteByte('\n')
	}
	if part.SourcePath != "" {
		b.WriteString("Source path: ")
		b.WriteString(part.SourcePath)
		b.WriteByte('\n')
	}
	if toolPath != "" && part.Origin == attachment.OriginTemp {
		b.WriteString("File tool scope: temp\n")
		b.WriteString("File tool path: ")
		b.WriteString(filepath.ToSlash(filepath.Join("attachments", filepath.Base(filepath.FromSlash(part.AttachmentKey)))))
		b.WriteByte('\n')
	}
	switch mediaKind {
	case "image":
		b.WriteString("Image content: provided as an image part.\n")
	case "audio":
		b.WriteString("Audio content: provided as an audio part.\n")
	default:
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(part.MIME)), "image/") {
			b.WriteString("Image content: not provided because the current model does not support image inputs. Do not describe visual details from metadata alone.\n")
		}
	}
	if part.AudioTranscript != "" {
		b.WriteString("Audio transcript: ")
		b.WriteString(part.AudioTranscript)
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

func localFoldersProviderText(folders []store.LocalFolder) string {
	folders = store.NormalizeLocalFolders(folders)
	if len(folders) == 0 {
		return ""
	}
	payload := struct {
		Folders []store.LocalFolder `json:"folders"`
	}{Folders: folders}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return "<pudding-local-folders version=\"1\">\n" + string(data) + "\n</pudding-local-folders>\n"
}

func projectReferencesProviderText(references []store.ProjectReference) string {
	references = store.NormalizeProjectReferences(references)
	if len(references) == 0 {
		return ""
	}
	payload := struct {
		References []store.ProjectReference `json:"references"`
	}{References: references}
	data, err := json.Marshal(payload)
	if err != nil {
		return ""
	}
	return "<pudding-project-references version=\"1\">\n" + string(data) + "\n</pudding-project-references>\n"
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

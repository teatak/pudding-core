package engine

// titler.go — 会话自动标题:
//
//   - 触发:空标题会话的 user 消息提交成功后(Submit 内),与主 turn
//     生命周期解耦,不等 assistant 回答。
//   - 两段式:先同步写 provisional(用户文本截断)让列表立即有名字,
//     再异步起裸 LLM 调用总结首条 user 消息。
//   - 覆盖纪律:provisional 只写在空标题上;LLM 标题只写在 provisional
//     上(逐步校验前值)。用户手动改名后两步都不会动它。
//   - 通知:每次写回 emit session.titled(不落库),前端刷新会话列表。
//
// 节流天然成立:provisional 写回后标题非空,后续 Submit 不再触发。

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/event"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

// titlerSystemPrompt 沿用旧项目的短标题原则:只描述任务,不做输出纠偏。
const titlerSystemPrompt = `You write only a short session-list title.

Requirements:

- Make it specific to the concrete task, question, or topic.
- Rewrite the request as a concise topic noun phrase; do not copy the request sentence.
- Remove request filler or imperative wording such as "please", "help me", "again", "请", "帮我", "再", or "查一下".
- Use the conversation's dominant user language. If no dominant language is clear, use English.
- Chinese titles must be 2-8 Chinese characters. English titles must be 2-5 words.
- Avoid filler such as "about", "discussion", or "chat".
- Do not mention "user" or "assistant".
- Do not include quotes, trailing punctuation, or emoji.
- Output only the title.`

const titlerUserTemplate = `<conversation>
user: %s
</conversation>

Return only the title.`

// titlerTimeout:LLM 通常 1-3s 出标题,30s 是含 thinking 模型预热的冗余上限。
const titlerTimeout = 30 * time.Second

const (
	provisionalTitleRunes = 24
	finalTitleRunes       = 20
	titlerMaxOutputTokens = 1024

	titleQuoteChars          = "\"'`“”‘’「」《》"
	titleTrailingPunctuation = ".,:;!?。，：；！？…"
)

// autoTitle 在 Submit 接受首条消息后调用(sess.Title == "" 时)。
// provisional 同步写(本地 SQLite,纳秒级),LLM 调用异步。
func (e *Engine) autoTitle(sessionID, providerName, model string, modelConfig provider.ModelConfig, userText string) {
	provisional := provisionalTitleFromText(userText)
	if provisional == "" {
		return
	}
	ctx := context.Background()
	cur, err := e.store.GetSession(ctx, sessionID)
	if err != nil || cur.Title != "" {
		return // 已有名字(手动或并发),不动
	}
	if _, err := e.store.UpdateSession(ctx, sessionID, store.SessionUpdate{Title: &provisional}); err != nil {
		slog.Warn("titler: provisional write failed", "session", sessionID, "err", err)
		return
	}
	e.hub.Publish(event.Event{SessionID: sessionID, Kind: event.SessionTitled, Title: provisional})

	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		startedAt := time.Now()
		title, err := e.generateTitle(providerName, model, modelConfig, userText)
		if err != nil {
			slog.Warn(
				"titler: generate failed, keeping provisional",
				"session", sessionID,
				"profile", providerName,
				"model", model,
				"duration", time.Since(startedAt),
				"err", err,
			)
			return
		}
		slog.Info(
			"titler: generation completed",
			"session", sessionID,
			"profile", providerName,
			"model", model,
			"duration", time.Since(startedAt),
			"provisional", provisional,
			"generated", title,
			"sameAsProvisional", title == provisional,
		)
		if title == "" {
			slog.Warn(
				"titler: model returned no title, keeping provisional",
				"session", sessionID,
				"profile", providerName,
				"model", model,
			)
			return
		}
		if title == provisional {
			return
		}
		cur, err := e.store.GetSession(context.Background(), sessionID)
		if err != nil || cur.Title != provisional {
			return // 期间被手动改名,LLM 结果作废
		}
		if _, err := e.store.UpdateSession(context.Background(), sessionID, store.SessionUpdate{Title: &title}); err != nil {
			slog.Warn("titler: final write failed", "session", sessionID, "err", err)
			return
		}
		e.hub.Publish(event.Event{SessionID: sessionID, Kind: event.SessionTitled, Title: title})
	}()
}

// generateTitle 起一次裸 LLM 调用(同 session 解析出的 provider/model),
// 收集全文后清洗截断。不产 turn、不进 messages,失败只留 provisional。
func (e *Engine) generateTitle(providerName, model string, modelConfig provider.ModelConfig, userText string) (string, error) {
	// 从 auxCtx 派生:Stop() 后(优雅退出)立即取消这次 LLM 调用,
	// 不让 30s 超时拖住 engine.Wait()。
	ctx, cancel := context.WithTimeout(e.auxCtx, titlerTimeout)
	defer cancel()
	client, err := e.resolver.Resolve(ctx, providerName)
	if err != nil {
		return "", err
	}
	ch, err := client.Stream(ctx, provider.Request{
		Model:  model,
		System: titlerSystemPrompt,
		Messages: []provider.Message{
			{Role: provider.RoleUser, Text: fmt.Sprintf(titlerUserTemplate, userText)},
		},
		Config: titlerConfig(modelConfig),
	})
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		if chunk.Part == "" || chunk.Part == provider.PartText {
			buf.WriteString(chunk.Delta)
		}
	}
	return truncateRunes(sanitizeTitle(buf.String()), finalTitleRunes), nil
}

// sanitizeTitle 沿用旧项目:取首行、剥首尾引号/书名号、去末尾标点。
func sanitizeTitle(s string) string {
	s = firstLine(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = strings.Trim(s, titleQuoteChars)
	s = strings.TrimRight(s, titleTrailingPunctuation)
	return strings.TrimSpace(s)
}

func provisionalTitleFromText(text string) string {
	s := firstSentence(firstLine(strings.TrimSpace(text)))
	if s == "" {
		return ""
	}
	s = sanitizeTitle(strings.Join(strings.Fields(s), " "))
	return truncateRunesWithEllipsis(s, provisionalTitleRunes)
}

func firstLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

func firstSentence(s string) string {
	for i, r := range s {
		switch r {
		case '。', '！', '？', '.', '!', '?':
			if i > 0 {
				return strings.TrimSpace(s[:i])
			}
			return strings.TrimSpace(s)
		}
	}
	return strings.TrimSpace(s)
}

func titlerConfig(base provider.ModelConfig) provider.ModelConfig {
	cfg := provider.ModelConfig{
		ContextWindow: base.ContextWindow,
	}
	if base.Capabilities != nil {
		cfg.Capabilities = &provider.ModelCapabilities{
			Image: base.Capabilities.Image,
			Audio: base.Capabilities.Audio,
			Tools: false,
		}
	}
	cfg.Limits = &provider.ModelLimits{MaxOutputTokens: titlerMaxOutputTokens}
	cfg.ProviderOptions = &provider.ModelProviderOptions{
		OpenAI:    map[string]any{"reasoning_effort": "low"},
		Google:    map[string]any{},
		Anthropic: map[string]any{},
	}
	return cfg
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimSpace(string(runes[:n]))
}

func truncateRunesWithEllipsis(s string, n int) string {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) <= n {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:n])) + "…"
}

package engine

// titler.go — 会话自动标题,移植旧项目 session_titler.go 的方案:
//
//   - 触发:空标题会话的 user 消息提交成功后(Submit 内),与主 turn
//     生命周期解耦,不等 assistant 回答。
//   - 两段式:先同步写 provisional(用户文本截断)让列表立即有名字,
//     再异步起裸 LLM 调用生成正式标题。
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

// titlerSystemPrompt 沿用旧项目:简短、跟随对话主导语言、无引号标点 emoji。
const titlerSystemPrompt = `Generate a concise title for the conversation.

Requirements:

- Make it specific to the concrete task, question, or topic.
- Use the conversation's dominant user language. If no dominant language is clear, use English.
- Keep it short enough for a session list.
- Avoid filler such as "about", "discussion", or "chat".
- Do not mention "user" or "assistant".
- Do not include quotes, trailing punctuation, or emoji.
- Output only the title.`

const titlerUserTemplate = `Generate a title for this conversation:

%s`

// titlerTimeout:LLM 通常 1-3s 出标题,30s 是含 thinking 模型预热的冗余上限。
const titlerTimeout = 30 * time.Second

const (
	provisionalTitleRunes = 24
	finalTitleRunes       = 64
)

// autoTitle 在 Submit 接受首条消息后调用(sess.Title == "" 时)。
// provisional 同步写(本地 SQLite,纳秒级),LLM 调用异步。
func (e *Engine) autoTitle(sessionID, providerName, model, userText string) {
	provisional := truncateRunes(sanitizeTitle(userText), provisionalTitleRunes)
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
		title, err := e.generateTitle(providerName, model, userText)
		if err != nil {
			slog.Warn("titler: generate failed, keeping provisional", "session", sessionID, "err", err)
			return
		}
		if title == "" || title == provisional {
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
func (e *Engine) generateTitle(providerName, model, userText string) (string, error) {
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
	})
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			return "", chunk.Err
		}
		buf.WriteString(chunk.Delta)
	}
	return truncateRunes(sanitizeTitle(buf.String()), finalTitleRunes), nil
}

// sanitizeTitle 沿用旧项目:取首行、剥首尾引号/书名号、去末尾标点。
func sanitizeTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	quotes := []string{
		`"`, `'`, "`",
		"“", "”", // “ ”
		"‘", "’", // ‘ ’
		"「", "」", // 「 」
		"《", "》", // 《 》
	}
	for _, q := range quotes {
		s = strings.TrimPrefix(s, q)
		s = strings.TrimSuffix(s, q)
	}
	s = strings.TrimSpace(s)
	puncts := []string{
		".", ",", ":", ";", "!", "?",
		"。", "，", "：", "；", "！", "？", "…", // 。,:;!?…
	}
	for _, p := range puncts {
		s = strings.TrimSuffix(s, p)
	}
	return strings.TrimSpace(s)
}

func truncateRunes(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return strings.TrimSpace(string(runes[:n]))
}

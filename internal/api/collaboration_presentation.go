package api

import (
	"context"
	"regexp"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
)

func (s *Server) setChildTask(ctx context.Context, view *childSessionView, page *store.TurnPage) error {
	for len(page.Turns) > 0 {
		for i := len(page.Turns) - 1; i >= 0; i-- {
			messages := page.Turns[i].Messages
			for j := len(messages) - 1; j >= 0; j-- {
				if childTaskInput(messages[j]) {
					view.setTask(messages[j].Text, page.HasMore || i > 0 || j > 0)
					return nil
				}
			}
		}
		if !page.HasMore {
			break
		}
		// A form reply or system continuation is not a new task. Read back to
		// the actual task input rather than restoring the first session title.
		var err error
		page, err = s.store.ListTurnsPage(ctx, view.Session.ID, page.Turns[0].ID, 1)
		if err != nil {
			return err
		}
	}
	return nil
}

// Task labels are read-only projections of canonical input, never a second
// mutable task record. The first task retains its explicitly dispatched title;
// a reused conversation describes its current task instead of that old title.
func (view *childSessionView) setTask(text string, reused bool) {
	view.Summary = childTextExcerpt(text, 96)
	if reused {
		title := view.Summary
		if i := strings.IndexAny(title, ",，;；"); i >= 0 {
			title = title[:i]
		}
		view.TaskTitle = truncateChildExcerpt(title, 28)
	}
}

func childTaskInput(message *store.Message) bool {
	if message.Role != store.RoleUser || strings.TrimSpace(message.Text) == "" {
		return false
	}
	for _, part := range message.Parts {
		if part.Type == store.ContentPartFormResult {
			return false
		}
	}
	return true
}

var (
	childSentenceEnd = regexp.MustCompile(`[。！？]|[.!?](?:\s|$)`)
	childListPrefix  = regexp.MustCompile(`^(?:[-*+>]\s+|\d+[.)]\s+)`)
	childTaskPrefix  = regexp.MustCompile(`(?i)^(?:新任务|新任務|new task)\s*[:：]\s*`)
	childLink        = regexp.MustCompile(`!?\[([^\]]*)\]\([^)]*\)`)
	childToolName    = regexp.MustCompile(`\b(?:builtin_|mcp__)[a-zA-Z0-9_]+`)
)

// Short prose excerpts omit fenced code and internal tool names. They are not
// model-generated summaries.
func childTextExcerpt(text string, limit int) string {
	inCode := false
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "```") || strings.HasPrefix(line, "~~~") {
			inCode = !inCode
			continue
		}
		if inCode || strings.HasPrefix(line, "#") {
			continue
		}
		line = childListPrefix.ReplaceAllString(line, "")
		line = childTaskPrefix.ReplaceAllString(line, "")
		line = childLink.ReplaceAllString(line, "$1")
		line = strings.NewReplacer("**", "", "__", "", "`", "").Replace(line)
		line = childSentenceEnd.Split(line, 2)[0]
		if line == "" || childToolName.MatchString(line) {
			continue
		}
		return truncateChildExcerpt(strings.Join(strings.Fields(line), " "), limit)
	}
	return ""
}

func truncateChildExcerpt(text string, limit int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

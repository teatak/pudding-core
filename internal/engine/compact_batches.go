package engine

import (
	"context"
	"strings"

	"github.com/teatak/pudding-core/internal/contextbuilder"
	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/store"
)

// Summarize bounded batches in order. Every fragment retains its canonical
// message reference, including a single user message larger than the window.
// Intermediate summaries remain local to this operation; only the final,
// validated summary becomes a canonical boundary.
func (e *Engine) summarizeCompactHistory(ctx context.Context, sessionID string, resolved *resolvedModel, client provider.Client, messages []*store.Message, hint string, target int) (string, error) {
	empty := compactSummaryRequest(resolved, "", hint, "", target)
	limit := compactMaxInputTokens
	if n := contextInputLimit(empty.Config, resolved.protocol); n > 0 {
		limit = min(limit, n)
	}
	type record struct {
		message *store.Message
		text    []rune
	}
	var records []record
	for _, message := range messages {
		text := compactMessageText(message)
		if strings.TrimSpace(text) == "" {
			continue
		}
		records = append(records, record{message: message, text: []rune(text)})
	}
	if len(records) == 0 {
		return "", ErrCompactEmpty
	}
	summary := ""
	for len(records) > 0 {
		var batch strings.Builder
		for len(records) > 0 {
			current := &records[0]
			req := compactSummaryRequest(resolved, batch.String(), hint, summary, target)
			// A generation target is advisory. Size each fragment against the
			// actual rolling summary, including when it exceeded that target.
			available := limit - contextbuilder.EstimateRequest(req).Total() - contextbuilder.EstimateTextTokens(compactMessageRecord(current.message, "")) - 64
			end := compactPrefixLength(current.text, available)
			if end == 0 {
				break
			}
			batch.WriteString(compactMessageRecord(current.message, string(current.text[:end])))
			current.text = current.text[end:]
			if len(current.text) > 0 {
				break
			}
			records = records[1:]
		}
		if batch.Len() == 0 {
			return "", ErrContextBudget
		}
		req := compactSummaryRequest(resolved, batch.String(), hint, summary, target)
		var err error
		summary, err = e.generateCompactSummary(ctx, sessionID, resolved, client, req)
		if err != nil {
			return "", err
		}
	}
	return summary, nil
}

func compactPrefixLength(text []rune, budget int) int {
	lo, hi := 0, len(text)
	for lo < hi {
		mid := lo + (hi-lo+1)/2
		if contextbuilder.EstimateTextTokens(string(text[:mid])) <= budget {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

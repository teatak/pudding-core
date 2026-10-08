package contextbuilder

import "github.com/teatak/pudding-core/internal/store"

// Notifications can be recorded during another turn's tool execution. Keep
// each turn together for model replay so a new user input cannot split a call
// from its results, attachments or native continuation. Within-turn steering
// retains its original order. Canonical storage and transcript order stay intact.
func historyInTurnOrder(messages []*store.Message) []*store.Message {
	out := make([]*store.Message, 0, len(messages))
	var groups [][]*store.Message
	positions := make(map[string]int)
	flush := func() {
		for _, group := range groups {
			out = append(out, group...)
		}
		groups = nil
		clear(positions)
	}
	for _, message := range messages {
		// A compaction boundary may reuse the active turn ID. Never pull its
		// later messages ahead of retained history or across the summary.
		if message.Role == store.RoleSummary || message.TurnID == "" {
			flush()
			out = append(out, message)
			continue
		}
		position, ok := positions[message.TurnID]
		if !ok {
			position = len(groups)
			positions[message.TurnID] = position
			groups = append(groups, nil)
		}
		groups[position] = append(groups[position], message)
	}
	flush()
	return out
}

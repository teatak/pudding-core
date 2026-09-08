package store

// OrderQueuedInputs validates the complete active set. A stale drag must not
// silently overwrite an input concurrently queued, cancelled or promoted.
func OrderQueuedInputs(active []*QueuedInput, ids []string) ([]*QueuedInput, error) {
	if len(active) != len(ids) {
		return nil, ErrQueueChanged
	}
	byID := make(map[string]*QueuedInput, len(active))
	for _, input := range active {
		byID[input.ClientMessageID] = input
	}
	ordered := make([]*QueuedInput, 0, len(ids))
	for _, id := range ids {
		input := byID[id]
		if input == nil {
			return nil, ErrQueueChanged
		}
		ordered = append(ordered, input)
		delete(byID, id)
	}
	return ordered, nil
}

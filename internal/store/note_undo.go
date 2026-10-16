package store

// Map an exact inverse through later, disjoint edits. Never apply fuzzy patches:
// an overlap rejects the complete operation rather than overwriting newer text.
func UndoNote(before, after, current string) (string, error) {
	inverse, later := textChanges(after, before), textChanges(after, current)
	if len(inverse) == 0 {
		return "", &InvalidContent{"version has no content changes"}
	}
	for i := range inverse {
		shift := 0
		for _, change := range later {
			if overlaps(inverse[i], change) {
				return "", ErrUndoConflict
			}
			if change.end <= inverse[i].start {
				shift += len(change.text) - (change.end - change.start)
			}
		}
		inverse[i].start += shift
		inverse[i].end += shift
	}
	out := []rune(current)
	for i := len(inverse) - 1; i >= 0; i-- {
		c := inverse[i]
		out = append(out[:c.start], append(c.text, out[c.end:]...)...)
	}
	result := string(out)
	return result, ValidateNote(result)
}

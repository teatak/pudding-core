package pagination

func Page(items []int, page, size int) []int {
	if size <= 0 {
		return nil
	}
	start := page * size
	if start >= len(items) {
		return nil
	}
	end := start + size
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}

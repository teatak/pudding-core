package provider

import "unicode"

func EstimateTextTokens(text string) int {
	if text == "" {
		return 0
	}
	asciiChars := 0
	wideChars := 0
	otherChars := 0
	for _, r := range text {
		switch {
		case r <= unicode.MaxASCII:
			asciiChars++
		case isCJK(r):
			wideChars++
		default:
			otherChars++
		}
	}
	return ceilDiv(asciiChars, 4) + wideChars + ceilDiv(otherChars, 2)
}

func EstimateImageTokens(width, height int) int {
	if width <= 0 || height <= 0 {
		return 1024
	}
	// Provider billing formulas differ. One token per 1024 pixels is a bounded,
	// provider-neutral estimate; provider-reported usage remains authoritative.
	tokens := int((int64(width)*int64(height) + 1023) / 1024)
	if tokens < 256 {
		return 256
	}
	if tokens > 8192 {
		return 8192
	}
	return tokens
}

func ceilDiv(n, d int) int {
	if n <= 0 {
		return 0
	}
	return (n + d - 1) / d
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) ||
		(r >= 0x2B740 && r <= 0x2B81F) ||
		(r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) ||
		(r >= 0x2F800 && r <= 0x2FA1F)
}

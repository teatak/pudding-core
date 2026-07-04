package tts

import (
	"regexp"
	"strings"
	"unicode"
)

// SanitizeText removes markdown decorations that sound noisy in TTS. It is
// intentionally lossy: the goal is speakable text, not faithful markdown.
func SanitizeText(text string) string {
	text = mdCodeFenceRE.ReplaceAllString(text, " ")
	text = mdLinkRE.ReplaceAllString(text, "$1")
	text = mdTableSepRE.ReplaceAllString(text, "\n")
	text = mdLinePrefixRE.ReplaceAllString(text, "$1")
	text = mdDecorRE.ReplaceAllString(text, " ")
	text = multiSpaceRE.ReplaceAllString(text, " ")
	text = spaceBeforePunctRE.ReplaceAllString(text, "$1")
	return strings.TrimSpace(text)
}

func HasSpeakableText(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

var (
	mdCodeFenceRE      = regexp.MustCompile("(?s)```[^`]*```")
	mdLinkRE           = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdTableSepRE       = regexp.MustCompile(`(?m)^\s*\|?\s*:?-{2,}:?(\s*\|\s*:?-{2,}:?)*\s*\|?\s*$`)
	mdLinePrefixRE     = regexp.MustCompile(`(?m)^[ \t]*(?:#{1,6}\s+|[-*+]\s+|>\s+|\d+\.\s+)(\S?)`)
	mdDecorRE          = regexp.MustCompile("[*_`#|~]+")
	multiSpaceRE       = regexp.MustCompile(`[ \t\n\r]+`)
	spaceBeforePunctRE = regexp.MustCompile(`\s+([，。！？；：、,.:;!?])`)
)

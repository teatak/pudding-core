package voice

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

const defaultSegmentMaxRunes = 240

type SentenceSplitter struct {
	buffer   string
	maxRunes int
}

func NewSentenceSplitter(maxRunes int) *SentenceSplitter {
	if maxRunes <= 0 {
		maxRunes = defaultSegmentMaxRunes
	}
	return &SentenceSplitter{maxRunes: maxRunes}
}

func (s *SentenceSplitter) Push(delta string) []string {
	if delta == "" {
		return nil
	}
	s.buffer += delta
	return s.pop(false)
}

func (s *SentenceSplitter) Flush() []string {
	return s.pop(true)
}

func (s *SentenceSplitter) Reset() {
	s.buffer = ""
}

func (s *SentenceSplitter) pop(flush bool) []string {
	var out []string
	for {
		end := sentenceEnd(s.buffer)
		if end <= 0 {
			break
		}
		if seg := strings.TrimSpace(s.buffer[:end]); seg != "" {
			out = append(out, seg)
		}
		s.buffer = s.buffer[end:]
	}
	for {
		seg, rest, ok := splitOverflow(s.buffer, s.maxRunes)
		if !ok {
			break
		}
		if seg != "" {
			out = append(out, seg)
		}
		s.buffer = rest
	}
	if flush {
		if seg := strings.TrimSpace(s.buffer); seg != "" {
			out = append(out, seg)
		}
		s.buffer = ""
	}
	return out
}

func sentenceEnd(text string) int {
	for i, r := range text {
		if !isSentenceTerminator(r) {
			continue
		}
		end := i + utf8.RuneLen(r)
		for end < len(text) {
			r2, size := utf8.DecodeRuneInString(text[end:])
			if !isSentenceCloser(r2) {
				break
			}
			end += size
		}
		return end
	}
	return -1
}

func isSentenceTerminator(r rune) bool {
	switch r {
	case '.', '!', '?', ';', '\n', '。', '！', '？', '；':
		return true
	default:
		return false
	}
}

func isSentenceCloser(r rune) bool {
	switch r {
	case '"', '\'', ')', ']', '}', '”', '’', '）', '】', '》':
		return true
	default:
		return false
	}
}

func splitOverflow(text string, maxRunes int) (string, string, bool) {
	if maxRunes <= 0 || utf8.RuneCountInString(text) < maxRunes {
		return "", text, false
	}
	byteLimit := len(text)
	count := 0
	for i, r := range text {
		count++
		if count >= maxRunes {
			byteLimit = i + utf8.RuneLen(r)
			break
		}
	}
	cut := byteLimit
	for i, r := range text[:byteLimit] {
		if unicode.IsSpace(r) && i > 0 {
			cut = i
		}
	}
	seg := strings.TrimSpace(text[:cut])
	rest := text[cut:]
	if seg == "" {
		return "", text, false
	}
	return seg, rest, true
}

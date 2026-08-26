package contextbuilder

import (
	"encoding/json"
	"unicode"

	"github.com/teatak/pudding-core/internal/provider"
)

const messageTokenOverhead = 4

type RequestEstimate struct {
	MessageTokens int `json:"messageTokens"`
	SystemTokens  int `json:"systemTokens"`
	ToolsTokens   int `json:"toolsTokens"`
}

func (e RequestEstimate) Total() int {
	return e.MessageTokens + e.SystemTokens + e.ToolsTokens
}

func EstimateRequest(req provider.Request) RequestEstimate {
	return RequestEstimate{
		MessageTokens: EstimateMessagesTokens(req.Messages),
		SystemTokens:  EstimateTextTokens(req.System),
		ToolsTokens:   EstimateToolsTokens(req.Tools),
	}
}

func EstimateMessagesTokens(messages []provider.Message) int {
	total := 0
	for _, msg := range messages {
		for _, segment := range provider.SplitMessage(msg) {
			total += messageTokenOverhead + estimateMessageTokens(segment)
		}
	}
	return total
}

func estimateMessageTokens(msg provider.Message) int {
	contentTokens := 0
	if len(msg.Parts) == 0 {
		contentTokens = EstimateTextTokens(msg.Text)
	} else {
		for _, part := range msg.Parts {
			contentTokens += estimatePartTokens(part)
		}
	}
	return contentTokens
}

func EstimateToolsTokens(tools []provider.ToolDef) int {
	if len(tools) == 0 {
		return 0
	}
	data, err := json.Marshal(tools)
	if err != nil {
		return 0
	}
	return EstimateTextTokens(string(data))
}

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

func estimatePartTokens(part provider.Part) int {
	switch part.Type {
	case provider.PartText, provider.PartThought:
		return EstimateTextTokens(part.Text)
	case provider.PartImage:
		if len(part.Data) == 0 {
			return 0
		}
		return EstimateImageTokens(part.Width, part.Height)
	case provider.PartAudio:
		if len(part.Data) == 0 {
			return 0
		}
		return 1024
	case provider.PartToolUse:
		return EstimateTextTokens(part.CallID) + EstimateTextTokens(part.Name) + EstimateTextTokens(string(part.Args))
	case provider.PartToolResult:
		return EstimateTextTokens(part.CallID) + EstimateTextTokens(part.Name) + EstimateTextTokens(part.Content)
	default:
		return 0
	}
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

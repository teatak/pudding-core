package provider

import (
	"net/http"
	"sync/atomic"
)

var appAttributionSequence atomic.Uint64

// SetAppAttribution identifies Pudding on model requests, including requests
// forwarded to OpenRouter by gateways such as BuzzHive.
// https://openrouter.ai/docs/app-attribution
func SetAppAttribution(headers http.Header) {
	headers.Set("HTTP-Referer", "https://x-t.top")
	headers.Set("X-OpenRouter-Title", "Pudding")
	// OpenRouter documents two categories per request and merges them across
	// requests. Rotate pairs without issuing extra requests or persisting state.
	groups := [...]string{
		"programming-app,personal-agent",
		"general-chat,creative-writing",
	}
	index := (appAttributionSequence.Add(1) - 1) % uint64(len(groups))
	headers.Set("X-OpenRouter-Categories", groups[index])
}

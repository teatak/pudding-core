package contextbuilder

import (
	"fmt"

	"github.com/teatak/pudding-core/internal/provider"
	"github.com/teatak/pudding-core/internal/provider/anthropic"
	"github.com/teatak/pudding-core/internal/provider/google"
	"github.com/teatak/pudding-core/internal/provider/openai"
)

// EstimateRequest counts the input built by the selected wire adapter, including
// native continuations and the adapter's tool/schema sanitization.
func EstimateRequest(req provider.Request, protocol string) (provider.RequestEstimate, error) {
	var input provider.WireInput
	switch protocol {
	case "openai-compatible":
		input = openai.ChatRequestInput(req)
	case "openai-responses":
		input = openai.ResponsesRequestInput(req)
	case "anthropic":
		input = anthropic.RequestInput(req)
	case "google":
		input = google.RequestInput(req)
	default:
		return provider.RequestEstimate{}, fmt.Errorf("estimate input: unsupported protocol %q", protocol)
	}
	return input.Estimate()
}

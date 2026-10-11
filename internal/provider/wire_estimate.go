package provider

import (
	"encoding/base64"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

type RequestEstimate struct {
	MessageTokens int `json:"messageTokens"`
	SystemTokens  int `json:"systemTokens"`
	ToolsTokens   int `json:"toolsTokens"`
}

func (e RequestEstimate) Total() int { return e.MessageTokens + e.SystemTokens + e.ToolsTokens }

// WireInput contains only model input, projected by the same adapter that
// serializes the HTTP body. Generation options and credentials are not input.
type WireInput struct{ System, Messages, Tools any }

func (in WireInput) Estimate() (RequestEstimate, error) {
	var out RequestEstimate
	for _, field := range []struct {
		value  any
		tokens *int
	}{
		{in.System, &out.SystemTokens}, {in.Messages, &out.MessageTokens}, {in.Tools, &out.ToolsTokens},
	} {
		data, err := json.Marshal(field.value)
		if err != nil {
			return RequestEstimate{}, err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return RequestEstimate{}, err
		}
		*field.tokens = estimateWireValue(value)
	}
	return out, nil
}

// Count decoded text, not JSON escape sequences or base64 media. Tokenization
// remains approximate and is calibrated against actual provider usage.
func estimateWireValue(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case string:
		return EstimateTextTokens(v)
	case []any:
		total := 0
		for _, item := range v {
			total += estimateWireValue(item)
		}
		return total
	case map[string]any:
		if tokens, ok := estimateWireMedia(v); ok {
			return tokens + 4
		}
		total := 4
		for key, item := range v {
			total += EstimateTextTokens(key) + estimateWireValue(item)
		}
		return total
	default:
		return 1
	}
}

func estimateWireMedia(v map[string]any) (int, bool) {
	// OpenAI Chat / Responses.
	switch v["type"] {
	case "image_url", "input_image":
		url, _ := v["image_url"].(string)
		if item, ok := v["image_url"].(map[string]any); ok {
			url, _ = item["url"].(string)
		}
		if _, data, ok := strings.Cut(url, ","); ok && strings.HasPrefix(url, "data:image/") {
			return estimateEncodedImage(data), true
		}
		return EstimateImageTokens(0, 0), true
	case "input_audio":
		return 1024, true
	case "image": // Anthropic.
		if source, ok := v["source"].(map[string]any); ok {
			data, _ := source["data"].(string)
			return estimateEncodedImage(data), true
		}
	}
	// Gemini native parts, including native continuations.
	for _, key := range []string{"inlineData", "inline_data"} {
		if data, ok := v[key].(map[string]any); ok {
			mime, _ := data["mimeType"].(string)
			if mime == "" {
				mime, _ = data["mime_type"].(string)
			}
			if strings.HasPrefix(mime, "image/") {
				encoded, _ := data["data"].(string)
				return estimateEncodedImage(encoded), true
			}
			if strings.HasPrefix(mime, "audio/") {
				return 1024, true
			}
		}
	}
	return 0, false
}

func estimateEncodedImage(encoded string) int {
	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))
	// DecodeConfig reads the header, without decoding an entire bitmap.
	cfg, _, err := image.DecodeConfig(reader)
	if err != nil {
		return EstimateImageTokens(0, 0)
	}
	return EstimateImageTokens(cfg.Width, cfg.Height)
}

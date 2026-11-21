package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/tool"
)

type debugMCPResult struct {
	Content           []map[string]any `json:"content"`
	IsError           bool             `json:"isError,omitempty"`
	StructuredContent json.RawMessage  `json:"structuredContent,omitempty"`
	Meta              json.RawMessage  `json:"_meta,omitempty"`
}

func debugMCPText(text string) debugMCPResult {
	return debugMCPResult{Content: []map[string]any{{"type": "text", "text": text}}}
}

func debugMCPFailure(err error) debugMCPResult {
	result := debugMCPText(err.Error())
	result.IsError = true
	return result
}

func (s *Server) debugMCPToolResult(sessionID string, result tool.Result) (debugMCPResult, error) {
	out := debugMCPResult{Content: []map[string]any{}}
	if len(result.MCPResult) > 0 {
		if err := json.Unmarshal(result.MCPResult, &out); err != nil {
			return out, fmt.Errorf("decode MCP tool result: %w", err)
		}
	}
	// The runner can reject a raw success response while validating or storing
	// its images. Never let that response hide the actual execution error.
	out.IsError = out.IsError || !result.Ok
	hasImage, hasText := false, false
	for _, content := range out.Content {
		if content["type"] == "image" {
			hasImage = true
		}
		if content["type"] == "text" && content["text"] == result.Content {
			hasText = true
		}
	}
	if result.Content != "" && (!hasText && (!result.Ok || len(out.Content) == 0)) {
		out.Content = append(out.Content, map[string]any{"type": "text", "text": result.Content})
	}
	// Browser and plugin MCP runners also persist their raw images as both
	// attachment types. The original MCP content already carries those bytes.
	if !hasImage {
		seen := make(map[string]bool)
		items := append(append([]store.Attachment(nil), result.Attachments...), result.ContextAttachments...)
		for _, item := range items {
			if !strings.HasPrefix(item.MIME, "image/") || seen[item.AttachmentKey] {
				continue
			}
			seen[item.AttachmentKey] = true
			image, err := attachment.NewService(s.home).ModelImageForProvider(sessionID, item.AttachmentKey, item.MIME)
			if err != nil {
				return out, fmt.Errorf("read tool image %s: %w", item.Name, err)
			}
			out.Content = append(out.Content, map[string]any{"type": "image", "mimeType": image.MIME, "data": base64.StdEncoding.EncodeToString(image.Data)})
		}
	}
	if out.Content == nil {
		out.Content = []map[string]any{}
	}
	return out, nil
}

// The media tool has already checked the session, file type and upload size and
// stored a session attachment. Send audio bytes on the external-agent transport.
func (s *Server) externalMCPToolResult(sessionID string, result tool.Result) (debugMCPResult, error) {
	out, err := s.debugMCPToolResult(sessionID, result)
	if err != nil || !result.Ok || result.Name != tool.MediaRead {
		return out, err
	}
	for _, item := range result.Attachments {
		if !strings.HasPrefix(item.MIME, "audio/") {
			continue
		}
		file, found, err := attachment.NewService(s.home).Path(sessionID, item.AttachmentKey)
		if err != nil {
			return out, err
		}
		if !found {
			return out, fmt.Errorf("audio attachment %s is unavailable", item.Name)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return out, err
		}
		out.Content = append(out.Content, map[string]any{"type": "audio", "mimeType": item.MIME, "data": base64.StdEncoding.EncodeToString(data)})
	}
	return out, nil
}

package api

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
			service := attachment.NewService(s.home)
			load := service.ModelImageForProvider
			if result.Name == tool.BrowserScreenshot {
				load = service.ScreenshotForProvider
			}
			image, err := load(sessionID, item.AttachmentKey, item.MIME)
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

func (s *Server) externalMCPToolResult(sessionID string, result tool.Result) (debugMCPResult, error) {
	if result.Ok && result.Name == tool.BrowserScreenshot {
		content, err := s.externalScreenshotContent(sessionID, result)
		if err != nil {
			return debugMCPResult{}, err
		}
		result.Content = content
	}
	out, err := s.debugMCPToolResult(sessionID, result)
	if err != nil || !result.Ok || result.Name != tool.MediaRead {
		return out, err
	}
	// The media tool has already validated and stored its audio attachments.
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

func (s *Server) externalScreenshotContent(sessionID string, result tool.Result) (string, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(result.Content), &metadata); err != nil {
		return "", fmt.Errorf("decode browser screenshot metadata: %w", err)
	}
	// A successful browser capture has one attachment. Only export that file.
	path, err := tool.ExportMCPScreenshot(s.home, sessionID, result.Attachments[0])
	if err != nil {
		metadata["localImageExportError"] = err.Error()
	} else {
		metadata["localImagePath"] = path
		metadata["localImageFolder"] = filepath.Dir(filepath.Dir(path))
		metadata["exportHint"] = "If the image is not visible inline, use your own image reader on the file path supplied by your client or on localImagePath. Use read_file in Antigravity. In Gemini App, connect localImageFolder in Task mode and use view_file. For browser coordinates using localImagePath, use the original width and height with captureBounds."
	}
	data, err := json.Marshal(metadata)
	return string(data), err
}

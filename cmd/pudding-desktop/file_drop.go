package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/store"
)

const desktopFileDropEvent = "pudding:file-drop"

type desktopDropTarget struct {
	Kind      string `json:"kind"`
	SessionID string `json:"sessionID"`
}

func bindDesktopFileDrop(window *application.WebviewWindow, home string) {
	if window == nil {
		return
	}
	svc := attachment.NewService(home)
	window.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		if event == nil || event.Context() == nil {
			return
		}
		paths := dedupeDroppedPaths(event.Context().DroppedFiles())
		if len(paths) == 0 {
			return
		}
		target := desktopFileDropTarget(event.Context().DropTargetDetails())
		directories, files, unknown := classifyDroppedPaths(paths)
		attachments, failedFiles := storeDroppedFileAttachments(svc, desktopAttachmentSessionID(target), files)
		window.EmitEvent(desktopFileDropEvent, map[string]any{
			"attachments": attachments,
			"directories": directories,
			"failedFiles": failedFiles,
			"files":       files,
			"paths":       paths,
			"target":      target,
			"unknown":     unknown,
		})
	})
}

func desktopAttachmentSessionID(target desktopDropTarget) string {
	switch target.Kind {
	case "draft":
		return attachment.DraftSessionID
	case "conversation":
		return strings.TrimSpace(target.SessionID)
	default:
		return ""
	}
}

func storeDroppedFileAttachments(svc *attachment.Service, sessionID string, paths []string) ([]store.Attachment, []string) {
	if strings.TrimSpace(sessionID) == "" || len(paths) == 0 {
		return nil, nil
	}
	attachments := make([]store.Attachment, 0, len(paths))
	failed := make([]string, 0)
	for _, path := range paths {
		stored, err := svc.StorePath(sessionID, path)
		if err != nil {
			failed = append(failed, path)
			slog.Warn("pudding-desktop: store dropped file", "path", path, "err", err)
			continue
		}
		stored = attachment.WithSourcePath(stored, path)
		attachments = append(attachments, stored)
	}
	return attachments, failed
}

func classifyDroppedPaths(paths []string) ([]string, []string, []string) {
	directories := make([]string, 0, len(paths))
	files := make([]string, 0, len(paths))
	unknown := make([]string, 0)
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			unknown = append(unknown, path)
			slog.Warn("pudding-desktop: stat dropped path", "path", path, "err", err)
			continue
		}
		if info.IsDir() {
			directories = append(directories, path)
			continue
		}
		files = append(files, path)
	}
	return directories, files, unknown
}

func desktopFileDropTarget(details *application.DropTargetDetails) desktopDropTarget {
	if details == nil || details.Attributes == nil {
		return desktopDropTarget{}
	}
	return desktopDropTarget{
		Kind:      details.Attributes["data-pudding-drop-target"],
		SessionID: details.Attributes["data-session-id"],
	}
}

func dedupeDroppedPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	deduped := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := strings.TrimSpace(path)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		deduped = append(deduped, clean)
	}
	return deduped
}

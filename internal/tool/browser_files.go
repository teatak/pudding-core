package tool

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/browser"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/store"
)

type browserUploadSource struct {
	Source        string `json:"source"`
	AttachmentKey string `json:"attachmentKey,omitempty"`
	Path          string `json:"path,omitempty"`
}

type browserUploadArgs struct {
	TabID    string                `json:"tabID"`
	FrameID  string                `json:"frameID,omitempty"`
	WindowID string                `json:"windowID,omitempty"`
	Selector string                `json:"selector"`
	Files    []browserUploadSource `json:"files"`
}

func (r *BuiltinRunner) browserUpload(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	service, ok := r.browser.(browser.FileTransferService)
	if !ok {
		return toolJSONError(out, "browser_unavailable", "native browser file transfers are unavailable")
	}
	var args browserUploadArgs
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if args.Selector == "" || len(args.Files) < 1 || len(args.Files) > 10 {
		return toolJSONError(out, "invalid_arguments", "selector and 1..10 files required")
	}
	tabID, err := r.browserResolveTabID(ctx, call.SessionID, args.TabID)
	if err != nil {
		return browserToolError(out, err)
	}
	root, rootPath, err := home.OpenSessionArtifacts(r.homeDir, call.SessionID)
	if err != nil {
		return toolJSONError(out, "upload_snapshot_failed", err.Error())
	}
	defer root.Close()
	batch := "browser-upload-" + rand.Text()
	if err := root.Mkdir(batch, 0700); err != nil {
		return toolJSONError(out, "upload_snapshot_failed", err.Error())
	}
	prepared := false
	defer func() {
		if !prepared {
			_ = root.RemoveAll(batch)
		}
	}()
	paths := make([]string, 0, len(args.Files))
	var attachmentNames map[string]string
	var total int64
	for i, source := range args.Files {
		if err := ctx.Err(); err != nil {
			return browserToolError(out, err)
		}
		input, info, err := r.openBrowserUploadSource(ctx, call, source)
		if err != nil {
			return toolJSONError(out, "upload_source_not_allowed", err.Error())
		}
		if info.Size() > attachment.MaxUploadBytes || total+info.Size() > 100<<20 {
			input.Close()
			return toolJSONError(out, "upload_too_large", "maximum 20 MiB per file and 100 MiB total")
		}
		name := info.Name()
		if source.Source == "attachment" {
			if attachmentNames == nil {
				attachmentNames, err = r.browserUploadAttachmentNames(ctx, call.SessionID)
				if err != nil {
					input.Close()
					return toolJSONError(out, "upload_source_not_allowed", err.Error())
				}
			}
			if displayName := attachmentNames[source.AttachmentKey]; displayName != "" {
				name = displayName
			}
		}
		dir := filepath.Join(batch, fmt.Sprint(i))
		if err := root.Mkdir(dir, 0700); err != nil {
			input.Close()
			return toolJSONError(out, "upload_snapshot_failed", err.Error())
		}
		rel := filepath.Join(dir, filepath.Base(name))
		output, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			input.Close()
			return toolJSONError(out, "upload_snapshot_failed", err.Error())
		}
		n, copyErr := io.Copy(output, io.LimitReader(input, attachment.MaxUploadBytes+1))
		closeErr := errors.Join(input.Close(), output.Close())
		if err := errors.Join(copyErr, closeErr, ctx.Err()); err != nil {
			return toolJSONError(out, "upload_snapshot_failed", err.Error())
		}
		if n > attachment.MaxUploadBytes || total+n > 100<<20 {
			return toolJSONError(out, "upload_too_large", "file grew beyond upload limit")
		}
		total += n
		paths = append(paths, filepath.Join(rootPath, rel))
	}
	if err := ctx.Err(); err != nil {
		return browserToolError(out, err)
	}
	// File objects may be read by Chromium well after selection. Retain snapshots
	// in the session-artifact lifecycle even if dispatch has an uncertain outcome.
	prepared = true
	result, err := service.Upload(ctx, call.SessionID, tabID, browser.UploadInput{WindowID: args.WindowID, FrameID: args.FrameID, Selector: args.Selector, Files: paths})
	if err != nil {
		return browserToolError(out, err)
	}
	return r.browserActionResult(ctx, call, out, result)
}

// Attachment blobs have generated storage names. Use the session's display
// names when metadata exists; file ownership is checked independently above.
func (r *BuiltinRunner) browserUploadAttachmentNames(ctx context.Context, sessionID string) (map[string]string, error) {
	names := make(map[string]string)
	history, ok := r.history.(attachmentExportMessageSource)
	if !ok {
		return names, nil
	}
	messages, err := history.ListMessages(ctx, sessionID, 0)
	if err != nil {
		return nil, err
	}
	for _, message := range messages {
		if message == nil || message.SessionID != sessionID {
			continue
		}
		for _, item := range store.AttachmentsFromParts(message.Parts) {
			name := filepath.Base(strings.ReplaceAll(item.Name, `\`, "/"))
			if name != "." && name != ".." && name != "/" && name != "" {
				names[item.AttachmentKey] = name
			}
		}
	}
	return names, nil
}

func (r *BuiltinRunner) openBrowserUploadSource(ctx context.Context, call Call, source browserUploadSource) (*os.File, os.FileInfo, error) {
	if source.Source == "attachment" {
		if source.AttachmentKey == "" || source.Path != "" {
			return nil, nil, errors.New("attachment source requires only attachmentKey")
		}
		resolved, ok, err := r.attachmentExportSource(ctx, call.SessionID, source.AttachmentKey)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, errors.New("attachment does not belong to this session")
		}
		return openAttachmentExportSource(r.homeDir, resolved)
	}
	if source.Source != "project" || source.Path == "" || source.AttachmentKey != "" {
		return nil, nil, errors.New("source must be attachment or project with its matching key/path")
	}
	resolved, err := r.resolveFilePath(call, managedScopeProject, source.Path, false, false, false)
	if err != nil {
		return nil, nil, err
	}
	root, err := os.OpenRoot(resolved.root)
	if err != nil {
		return nil, nil, err
	}
	defer root.Close()
	before, err := root.Stat(resolved.rel)
	if err != nil {
		return nil, nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, nil, errors.New("upload source must be a regular file")
	}
	file, err := root.Open(resolved.rel)
	if err != nil {
		return nil, nil, err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, nil, errors.New("upload source must be a regular file")
	}
	return file, info, nil
}

func (r *BuiltinRunner) browserDownloads(ctx context.Context, call Call) Result {
	out := browserBaseResult(call)
	if res, ok := r.browserReady(call, out); !ok {
		return res
	}
	service, ok := r.browser.(browser.FileTransferService)
	if !ok {
		return toolJSONError(out, "browser_unavailable", "native browser file transfers are unavailable")
	}
	var args browser.DownloadInput
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	result, err := service.Downloads(ctx, call.SessionID, args)
	if err != nil {
		return browserToolError(out, err)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "downloads": result.Downloads})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(result.Downloads)
	return out
}

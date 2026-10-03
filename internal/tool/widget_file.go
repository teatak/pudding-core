package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/store"
	"github.com/teatak/pudding-core/internal/widget"
)

func widgetVisiblePath(name string, directory bool) bool {
	if name == "." || name == "src" || name == "assets" {
		return true
	}
	if strings.HasPrefix(name, "src/") || strings.HasPrefix(name, "assets/") {
		return directory || widget.ValidFilePath(name)
	}
	return !directory && name == widget.ManifestFile
}

func widgetSourcePath(name string) bool {
	return widget.ValidFilePath(name) && !strings.HasPrefix(name, "fixtures/")
}

func (r *BuiltinRunner) widgetDraftForCall(call Call, id string) (widget.Draft, string, error) {
	if call.Mode != store.ModeCode || strings.TrimSpace(call.SessionID) == "" {
		return widget.Draft{}, "", errors.New("widget files require a Code session")
	}
	if r.widgetStore == nil {
		return widget.Draft{}, "", errors.New("widget store is unavailable")
	}
	item, err := r.widgetStore.GetStudioItem(context.Background(), id)
	if err != nil {
		return widget.Draft{}, "", err
	}
	if item.Kind != store.StudioItemKindWidget {
		return widget.Draft{}, "", errors.New("widget kind required")
	}
	root, err := widget.DraftRoot(r.homeDir, id)
	if err != nil {
		return widget.Draft{}, "", err
	}
	draft, err := widget.ReadDraft(r.homeDir, id)
	return draft, root, err
}

func (r *BuiltinRunner) resolveWidgetFilePath(call Call, rawPath string, requireWritable, allowRoot, allowMissing bool) (resolvedFilePath, error) {
	if requireWritable {
		return resolvedFilePath{}, errors.New("widget writes require builtin_file_patch")
	}
	switch call.Name {
	case FileList, FileRead, FileStat, FileSearch, FileSlice:
	default:
		return resolvedFilePath{}, errors.New("this tool cannot access widget files")
	}
	var args struct {
		ItemID string `json:"widget_id"`
	}
	if err := json.Unmarshal(call.Args, &args); err != nil || strings.TrimSpace(args.ItemID) == "" {
		return resolvedFilePath{}, errors.New("widget_id is required")
	}
	draft, root, err := r.widgetDraftForCall(call, args.ItemID)
	if err != nil {
		return resolvedFilePath{}, err
	}
	name := strings.TrimSpace(rawPath)
	if name == "" || name == "." {
		if !allowRoot {
			return resolvedFilePath{}, errors.New("widget source path is required")
		}
		name = "."
	} else if filepath.IsAbs(name) || strings.Contains(name, "\\") || path.Clean(name) != name || !widgetVisiblePath(name, true) && !widgetSourcePath(name) {
		return resolvedFilePath{}, errors.New("invalid widget source path")
	}
	if name != "." && !widgetVisiblePath(name, true) && !widgetSourcePath(name) {
		return resolvedFilePath{}, errors.New("invalid widget source path")
	}
	if !allowMissing && name != "." && !widgetVisiblePath(name, true) {
		if _, ok := draft.Files[name]; !ok {
			return resolvedFilePath{}, os.ErrNotExist
		}
	}
	resolvedRoot, target, rel, err := resolveProjectPath([]string{root}, name, allowRoot, allowMissing)
	if err != nil {
		return resolvedFilePath{}, err
	}
	return resolvedFilePath{root: resolvedRoot, target: target, rel: rel, widgetID: args.ItemID, draftHash: draft.DraftHash}, nil
}

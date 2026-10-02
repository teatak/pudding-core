package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/teatak/pudding-core/internal/canvas"
	"github.com/teatak/pudding-core/internal/store"
)

func canvasVisiblePath(name string, directory bool) bool {
	if name == "." || name == "src" || name == "assets" {
		return true
	}
	if strings.HasPrefix(name, "src/") || strings.HasPrefix(name, "assets/") {
		return directory || canvas.ValidFilePath(name)
	}
	return !directory && name == "canvas.json"
}

func canvasSourcePath(name string) bool {
	return canvas.ValidFilePath(name) && !strings.HasPrefix(name, "fixtures/")
}

func (r *BuiltinRunner) canvasDraftForCall(call Call, id string) (canvas.Draft, string, error) {
	if call.Mode != store.ModeCode || strings.TrimSpace(call.SessionID) == "" {
		return canvas.Draft{}, "", errors.New("canvas files require a Code session")
	}
	if r.canvasStore == nil {
		return canvas.Draft{}, "", errors.New("canvas store is unavailable")
	}
	if _, err := r.canvasStore.GetCanvas(context.Background(), id); err != nil {
		return canvas.Draft{}, "", err
	}
	root, err := canvas.DraftRoot(r.homeDir, id)
	if err != nil {
		return canvas.Draft{}, "", err
	}
	draft, err := canvas.ReadDraft(r.homeDir, id)
	return draft, root, err
}

func (r *BuiltinRunner) resolveCanvasFilePath(call Call, rawPath string, requireWritable, allowRoot, allowMissing bool) (resolvedFilePath, error) {
	if requireWritable {
		return resolvedFilePath{}, errors.New("canvas writes require builtin_file_patch")
	}
	switch call.Name {
	case FileList, FileRead, FileStat, FileSearch, FileSlice:
	default:
		return resolvedFilePath{}, errors.New("this tool cannot access canvas files")
	}
	var args struct {
		CanvasID string `json:"canvas_id"`
	}
	if err := json.Unmarshal(call.Args, &args); err != nil || strings.TrimSpace(args.CanvasID) == "" {
		return resolvedFilePath{}, errors.New("canvas_id is required")
	}
	draft, root, err := r.canvasDraftForCall(call, args.CanvasID)
	if err != nil {
		return resolvedFilePath{}, err
	}
	name := strings.TrimSpace(rawPath)
	if name == "" || name == "." {
		if !allowRoot {
			return resolvedFilePath{}, errors.New("canvas source path is required")
		}
		name = "."
	} else if filepath.IsAbs(name) || strings.Contains(name, "\\") || path.Clean(name) != name || !canvasVisiblePath(name, true) && !canvasSourcePath(name) {
		return resolvedFilePath{}, errors.New("invalid canvas source path")
	}
	if name != "." && !canvasVisiblePath(name, true) && !canvasSourcePath(name) {
		return resolvedFilePath{}, errors.New("invalid canvas source path")
	}
	if !allowMissing && name != "." && !canvasVisiblePath(name, true) {
		if _, ok := draft.Files[name]; !ok {
			return resolvedFilePath{}, os.ErrNotExist
		}
	}
	resolvedRoot, target, rel, err := resolveProjectPath([]string{root}, name, allowRoot, allowMissing)
	if err != nil {
		return resolvedFilePath{}, err
	}
	return resolvedFilePath{root: resolvedRoot, target: target, rel: rel, canvasID: args.CanvasID, draftHash: draft.DraftHash}, nil
}

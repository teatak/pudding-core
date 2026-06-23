package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const defaultWorkspaceListMax = 200

func workspaceList(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	roots := normalizeWorkspaceDirs(call.WorkspaceDirs)
	if len(roots) == 0 {
		out.Ok = false
		out.Content = jsonString(map[string]any{"ok": false, "reason": "workspace_dirs_required"})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 2
		return out
	}
	var args struct {
		Path       string `json:"path"`
		MaxEntries int    `json:"maxEntries"`
	}
	if len(call.Args) > 0 {
		if err := json.Unmarshal(call.Args, &args); err != nil {
			out.Ok = false
			out.Content = jsonString(map[string]any{"ok": false, "reason": "invalid_arguments", "error": err.Error()})
			out.SummaryKind = SummaryReturnedFields
			out.SummaryCount = 3
			return out
		}
	}
	maxEntries := args.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultWorkspaceListMax
	}
	if maxEntries > 1000 {
		maxEntries = 1000
	}
	root, target, rel, err := resolveWorkspacePath(roots, args.Path)
	if err != nil {
		out.Ok = false
		out.Content = jsonString(map[string]any{"ok": false, "reason": "path_not_allowed", "error": err.Error()})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 3
		return out
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		out.Ok = false
		out.Content = jsonString(map[string]any{"ok": false, "reason": "read_dir_failed", "error": err.Error()})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 3
		return out
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	capacity := len(entries)
	if capacity > maxEntries {
		capacity = maxEntries
	}
	items := make([]map[string]any, 0, capacity)
	for i, entry := range entries {
		if i >= maxEntries {
			break
		}
		item := map[string]any{
			"name": entry.Name(),
			"type": "file",
		}
		if entry.IsDir() {
			item["type"] = "dir"
		} else if info, err := entry.Info(); err == nil {
			item["size"] = info.Size()
		}
		items = append(items, item)
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{
		"ok":         true,
		"root":       root,
		"path":       rel,
		"entries":    items,
		"truncated":  len(entries) > maxEntries,
		"totalCount": len(entries),
	})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(items)
	return out
}

func normalizeWorkspaceDirs(dirs []string) []string {
	seen := make(map[string]bool, len(dirs))
	out := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		cleaned := filepath.Clean(dir)
		if seen[cleaned] {
			continue
		}
		seen[cleaned] = true
		out = append(out, cleaned)
	}
	return out
}

func resolveWorkspacePath(roots []string, rawPath string) (string, string, string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "."
	}
	candidates := make([]string, 0, len(roots))
	if filepath.IsAbs(rawPath) {
		candidates = append(candidates, filepath.Clean(rawPath))
	} else {
		for _, root := range roots {
			candidates = append(candidates, filepath.Join(root, rawPath))
		}
	}
	for _, candidate := range candidates {
		resolvedCandidate, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		for _, root := range roots {
			resolvedRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				continue
			}
			if !pathInsideRoot(resolvedCandidate, resolvedRoot) {
				continue
			}
			rel, err := filepath.Rel(resolvedRoot, resolvedCandidate)
			if err != nil {
				continue
			}
			if rel == "" {
				rel = "."
			}
			return root, resolvedCandidate, rel, nil
		}
	}
	return "", "", "", os.ErrPermission
}

func pathInsideRoot(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"reason":"encode_error"}`
	}
	return string(b)
}

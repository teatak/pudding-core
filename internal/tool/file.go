package tool

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/skill"
)

const (
	managedScopeSkillDraft     = "skill_draft"
	managedScopeSkillPublished = "skill_published"
	managedScopeTemp           = "temp"
	managedScopeWorkspace      = "workspace"

	defaultFileReadMaxChars = 20000
	maxFileReadChars        = 100000
	defaultFileListMax      = 200
)

type resolvedFilePath struct {
	root      string
	target    string
	rel       string
	workspace bool
}

func (p resolvedFilePath) outputPath() string {
	if p.workspace {
		return p.target
	}
	return p.rel
}

func (p resolvedFilePath) payload(base map[string]any) map[string]any {
	base["path"] = p.outputPath()
	if p.workspace {
		base["root"] = p.root
		base["relativePath"] = p.rel
	}
	return base
}

func (r *BuiltinRunner) fileList(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope      string `json:"scope"`
		Path       string `json:"path"`
		MaxEntries int    `json:"max_entries"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	maxEntries := args.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultFileListMax
	}
	if maxEntries > 1000 {
		maxEntries = 1000
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, true, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	entries, err := os.ReadDir(resolved.target)
	if err != nil {
		return toolJSONError(out, "read_dir_failed", err.Error())
	}
	if args.Scope == managedScopeSkillDraft {
		visible := entries[:0]
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") {
				visible = append(visible, entry)
			}
		}
		entries = visible
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir() != entries[j].IsDir() {
			return entries[i].IsDir()
		}
		return entries[i].Name() < entries[j].Name()
	})
	items := make([]map[string]any, 0, min(len(entries), maxEntries))
	for i, entry := range entries {
		if i >= maxEntries {
			break
		}
		itemRel := entry.Name()
		if resolved.workspace {
			itemRel = filepath.Join(resolved.target, entry.Name())
		} else if resolved.rel != "." {
			itemRel = filepath.ToSlash(filepath.Join(resolved.rel, entry.Name()))
		}
		item := map[string]any{"name": entry.Name(), "path": itemRel, "type": "file"}
		if entry.IsDir() {
			item["type"] = "dir"
		} else if info, err := entry.Info(); err == nil {
			item["size"] = info.Size()
		}
		items = append(items, item)
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{
		"ok":         true,
		"scope":      args.Scope,
		"entries":    items,
		"truncated":  len(entries) > maxEntries,
		"totalCount": len(entries),
	}))
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(items)
	return out
}

func (r *BuiltinRunner) fileRead(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope    string `json:"scope"`
		Path     string `json:"path"`
		MaxChars int    `json:"max_chars"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, false, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	data, err := os.ReadFile(resolved.target)
	if err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	}
	if !isToolText(data) {
		return toolJSONError(out, "binary_file", "file is not valid UTF-8 text")
	}
	maxChars := args.MaxChars
	if maxChars <= 0 {
		maxChars = defaultFileReadMaxChars
	}
	if maxChars > maxFileReadChars {
		maxChars = maxFileReadChars
	}
	content := string(data)
	truncated := false
	if len([]rune(content)) > maxChars {
		content = string([]rune(content)[:maxChars])
		truncated = true
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{
		"ok":        true,
		"scope":     args.Scope,
		"content":   content,
		"truncated": truncated,
		"chars":     len([]rune(string(data))),
	}))
	out.SummaryKind = SummaryReadChars
	out.SummaryCount = len([]rune(content))
	return out
}

func (r *BuiltinRunner) fileWrite(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope   string `json:"scope"`
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, true, false, true)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if err := os.MkdirAll(filepath.Dir(resolved.target), 0o700); err != nil {
		return toolJSONError(out, "mkdir_failed", err.Error())
	}
	if err := os.WriteFile(resolved.target, []byte(args.Content), 0o600); err != nil {
		return toolJSONError(out, "write_failed", err.Error())
	}
	if args.Scope == managedScopeSkillDraft {
		_ = r.removeDraftDelete(resolved.rel)
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{"ok": true, "scope": args.Scope, "bytes": len([]byte(args.Content))}))
	out.SummaryKind = SummaryChangedLines
	out.SummaryCount = countLines(args.Content)
	return out
}

func (r *BuiltinRunner) filePatch(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope      string `json:"scope"`
		Path       string `json:"path"`
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if args.OldString == "" {
		return toolJSONError(out, "old_string_required", "old_string must not be empty")
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, true, false, true)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if args.Scope == managedScopeSkillDraft {
		if err := r.copyPublishedFileForDraftPatch(resolved.rel, resolved.target); err != nil {
			return toolJSONError(out, "read_failed", err.Error())
		}
	}
	data, err := os.ReadFile(resolved.target)
	if err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	}
	if !isToolText(data) {
		return toolJSONError(out, "binary_file", "file is not valid UTF-8 text")
	}
	content := string(data)
	matches := strings.Count(content, args.OldString)
	if matches == 0 {
		return toolJSONError(out, "old_string_not_found", "old_string was not found")
	}
	if matches > 1 && !args.ReplaceAll {
		return toolJSONError(out, "old_string_ambiguous", "old_string matched more than once")
	}
	replaceN := 1
	if args.ReplaceAll {
		replaceN = -1
	}
	next := strings.Replace(content, args.OldString, args.NewString, replaceN)
	if err := os.WriteFile(resolved.target, []byte(next), 0o600); err != nil {
		return toolJSONError(out, "write_failed", err.Error())
	}
	if args.Scope == managedScopeSkillDraft {
		_ = r.removeDraftDelete(resolved.rel)
	}
	changed := matches
	if !args.ReplaceAll {
		changed = 1
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{"ok": true, "scope": args.Scope, "replacements": changed}))
	out.SummaryKind = SummaryChangedLines
	out.SummaryCount = changed
	return out
}

func (r *BuiltinRunner) fileDelete(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope     string `json:"scope"`
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, true, false, true)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if resolved.rel == "." {
		return toolJSONError(out, "refuse_root_delete", "deleting a scope root is not allowed")
	}
	if args.Scope == managedScopeSkillDraft {
		return r.fileDeleteSkillDraft(out, resolved.rel, resolved.target, args.Recursive)
	}
	info, err := os.Stat(resolved.target)
	if err != nil {
		return toolJSONError(out, "stat_failed", err.Error())
	}
	if info.IsDir() && !args.Recursive {
		return toolJSONError(out, "recursive_required", "recursive=true is required to delete a directory")
	}
	if args.Recursive {
		err = os.RemoveAll(resolved.target)
	} else {
		err = os.Remove(resolved.target)
	}
	if err != nil {
		return toolJSONError(out, "delete_failed", err.Error())
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{"ok": true, "scope": args.Scope}))
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func (r *BuiltinRunner) fileMove(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope    string `json:"scope"`
		FromPath string `json:"from_path"`
		ToPath   string `json:"to_path"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	fromResolved, err := r.resolveFilePath(call, args.Scope, args.FromPath, true, false, false)
	if err != nil {
		return filePathErrorWithReason(out, args.Scope, "from_path_not_allowed", err)
	}
	toResolved, err := r.resolveFilePath(call, args.Scope, args.ToPath, true, false, true)
	if err != nil {
		return filePathErrorWithReason(out, args.Scope, "to_path_not_allowed", err)
	}
	if fromResolved.rel == "." || toResolved.rel == "." {
		return toolJSONError(out, "refuse_root_move", "moving a scope root is not allowed")
	}
	if args.Scope == managedScopeSkillDraft {
		if err := r.copyPublishedFileForDraftPatch(fromResolved.rel, fromResolved.target); err != nil {
			return toolJSONError(out, "move_failed", err.Error())
		}
	}
	if err := os.MkdirAll(filepath.Dir(toResolved.target), 0o700); err != nil {
		return toolJSONError(out, "mkdir_failed", err.Error())
	}
	if err := os.Rename(fromResolved.target, toResolved.target); err != nil {
		return toolJSONError(out, "move_failed", err.Error())
	}
	if args.Scope == managedScopeSkillDraft {
		if publishedDeletes, err := r.publishedDeletePaths(fromResolved.rel, false); err == nil {
			_ = r.addDraftDeletes(publishedDeletes)
		}
		_ = r.removeDraftDelete(toResolved.rel)
	}
	out.Ok = true
	payload := map[string]any{"ok": true, "scope": args.Scope, "from": fromResolved.outputPath(), "to": toResolved.outputPath()}
	if fromResolved.workspace {
		payload["fromRoot"] = fromResolved.root
		payload["fromRelativePath"] = fromResolved.rel
		payload["toRoot"] = toResolved.root
		payload["toRelativePath"] = toResolved.rel
	}
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 4
	return out
}

func (r *BuiltinRunner) resolveFilePath(call Call, scope, rawPath string, requireWritable, allowRoot, allowMissing bool) (resolvedFilePath, error) {
	if strings.TrimSpace(scope) == managedScopeWorkspace {
		root, target, rel, err := resolveWorkspacePath(call.WorkspaceDirs, rawPath, allowRoot, allowMissing)
		if err != nil {
			return resolvedFilePath{}, err
		}
		return resolvedFilePath{root: root, target: target, rel: rel, workspace: true}, nil
	}
	root, target, rel, err := r.resolveManagedPath(scope, rawPath, requireWritable, allowRoot)
	if err != nil {
		return resolvedFilePath{}, err
	}
	return resolvedFilePath{root: root, target: target, rel: rel}, nil
}

func filePathError(out Result, scope string, err error) Result {
	return filePathErrorWithReason(out, scope, "path_not_allowed", err)
}

func filePathErrorWithReason(out Result, scope, fallbackReason string, err error) Result {
	if strings.TrimSpace(scope) != managedScopeWorkspace {
		return toolJSONError(out, fallbackReason, err.Error())
	}
	reason := "path_not_authorized"
	if errors.Is(err, errWorkspaceDirsRequired) {
		reason = "workspace_dirs_required"
	} else if errors.Is(err, errWorkspaceFilePathRequired) {
		reason = "path_not_allowed"
	}
	return toolJSONError(out, reason, err.Error())
}

func (r *BuiltinRunner) managedRoot(scope string) (string, bool, error) {
	if strings.TrimSpace(r.homeDir) == "" {
		return "", false, errors.New("home directory is not configured")
	}
	switch scope {
	case managedScopeSkillDraft:
		return home.SkillsDraftPath(r.homeDir), true, nil
	case managedScopeSkillPublished:
		return home.SkillsPath(r.homeDir), false, nil
	case managedScopeTemp:
		return home.TempPath(r.homeDir), true, nil
	default:
		return "", false, errors.New("unknown scope")
	}
}

func (r *BuiltinRunner) resolveManagedPath(scope, rawPath string, requireWritable, allowRoot bool) (string, string, string, error) {
	root, writable, err := r.managedRoot(scope)
	if err != nil {
		return "", "", "", err
	}
	if requireWritable && !writable {
		return "", "", "", errors.New("scope is read-only")
	}
	if requireWritable {
		if err := os.MkdirAll(root, 0o700); err != nil {
			return "", "", "", err
		}
	}
	resolvedRoot := root
	if evaluatedRoot, err := filepath.EvalSymlinks(root); err == nil {
		resolvedRoot = evaluatedRoot
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		rawPath = "."
	}
	if filepath.IsAbs(rawPath) {
		return "", "", "", errors.New("absolute paths are not allowed")
	}
	cleaned := filepath.Clean(rawPath)
	if cleaned == "." && !allowRoot {
		return "", "", "", errors.New("file path is required")
	}
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "", "", errors.New("parent traversal is not allowed")
	}
	if scope == managedScopeSkillDraft && hasHiddenPathComponent(cleaned) {
		return "", "", "", errors.New("path is reserved")
	}
	target := filepath.Join(root, cleaned)
	if !pathInsideRoot(target, root) {
		return "", "", "", errors.New("path escapes scope")
	}
	if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil {
			return "", "", "", err
		}
		if !pathInsideRoot(resolved, resolvedRoot) {
			return "", "", "", errors.New("symlink escapes scope")
		}
		target = resolved
	}
	if cleaned != "." {
		parent := filepath.Dir(target)
		if _, err := os.Stat(parent); err == nil {
			resolvedParent, err := filepath.EvalSymlinks(parent)
			if err != nil {
				return "", "", "", err
			}
			if !pathInsideRoot(resolvedParent, resolvedRoot) {
				return "", "", "", errors.New("parent escapes scope")
			}
		}
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", "", "", err
	}
	if rel == "" {
		rel = "."
	}
	return root, target, filepath.ToSlash(rel), nil
}

func firstPathComponent(cleaned string) string {
	if cleaned == "." {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(cleaned), "/")
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[0])
}

func hasHiddenPathComponent(cleaned string) bool {
	for _, part := range strings.Split(filepath.ToSlash(cleaned), "/") {
		if part == "" || part == "." {
			continue
		}
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func (r *BuiltinRunner) fileDeleteSkillDraft(out Result, rel, target string, recursive bool) Result {
	if _, item, err := splitDraftRel(rel); err != nil || item == "" {
		return toolJSONError(out, "refuse_root_delete", "deleting a draft root is not allowed")
	}
	deletes := []string{}
	info, draftErr := os.Stat(target)
	if draftErr == nil {
		if info.IsDir() && !recursive {
			return toolJSONError(out, "recursive_required", "recursive=true is required to delete a directory")
		}
		if recursive {
			draftErr = os.RemoveAll(target)
		} else {
			draftErr = os.Remove(target)
		}
		if draftErr != nil {
			return toolJSONError(out, "delete_failed", draftErr.Error())
		}
	} else if !errors.Is(draftErr, os.ErrNotExist) {
		return toolJSONError(out, "stat_failed", draftErr.Error())
	}
	publishedDeletes, pubErr := r.publishedDeletePaths(rel, recursive)
	if pubErr != nil {
		if draftErr != nil {
			return toolJSONError(out, "stat_failed", pubErr.Error())
		}
	} else {
		deletes = append(deletes, publishedDeletes...)
	}
	if len(deletes) > 0 {
		if err := r.addDraftDeletes(deletes); err != nil {
			return toolJSONError(out, "delete_failed", err.Error())
		}
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{"ok": true, "scope": managedScopeSkillDraft, "path": rel})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func (r *BuiltinRunner) publishedDeletePaths(rel string, recursive bool) ([]string, error) {
	id, item, err := splitDraftRel(rel)
	if err != nil || item == "" {
		return nil, errors.New("draft file path is required")
	}
	publishedRoot := filepath.Join(home.SkillsPath(r.homeDir), id)
	target := filepath.Join(publishedRoot, filepath.FromSlash(item))
	info, err := os.Stat(target)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []string{rel}, nil
	}
	if !recursive {
		return nil, errors.New("recursive=true is required to delete a directory")
	}
	var out []string
	err = filepath.WalkDir(target, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return err
		}
		itemRel, err := filepath.Rel(publishedRoot, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(filepath.Join(id, itemRel)))
		return nil
	})
	return out, err
}

func (r *BuiltinRunner) copyPublishedFileForDraftPatch(rel, target string) error {
	if _, err := os.Stat(target); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	id, item, err := splitDraftRel(rel)
	if err != nil || item == "" {
		return errors.New("draft file path is required")
	}
	published := filepath.Join(home.SkillsPath(r.homeDir), id, filepath.FromSlash(item))
	info, err := os.Stat(published)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("path is a directory")
	}
	data, err := os.ReadFile(published)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return os.WriteFile(target, data, info.Mode().Perm())
}

func (r *BuiltinRunner) addDraftDeletes(rels []string) error {
	grouped := map[string][]string{}
	for _, rel := range rels {
		id, item, err := splitDraftRel(rel)
		if err != nil || item == "" {
			continue
		}
		grouped[id] = append(grouped[id], item)
	}
	for id, items := range grouped {
		if err := updateDeleteManifest(filepath.Join(home.SkillsDraftPath(r.homeDir), id, skill.DraftDeleteFileName), items, nil); err != nil {
			return err
		}
	}
	return nil
}

func (r *BuiltinRunner) removeDraftDelete(rel string) error {
	id, item, err := splitDraftRel(rel)
	if err != nil || item == "" {
		return err
	}
	return updateDeleteManifest(filepath.Join(home.SkillsDraftPath(r.homeDir), id, skill.DraftDeleteFileName), nil, []string{item})
}

func splitDraftRel(rel string) (string, string, error) {
	parts := strings.SplitN(filepath.ToSlash(strings.TrimSpace(rel)), "/", 2)
	if len(parts) == 0 || parts[0] == "" || parts[0] == "." || parts[0] == ".." {
		return "", "", errors.New("draft id is required")
	}
	if len(parts) == 1 {
		return parts[0], "", nil
	}
	item := strings.TrimSpace(parts[1])
	if item == "" || item == "." || item == ".." || strings.HasPrefix(item, "../") {
		return "", "", errors.New("draft file path is required")
	}
	return parts[0], item, nil
}

func updateDeleteManifest(path string, add, remove []string) error {
	items := map[string]bool{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			item := strings.TrimSpace(line)
			if item != "" && !strings.HasPrefix(item, "#") {
				items[item] = true
			}
		}
	}
	for _, item := range remove {
		delete(items, filepath.ToSlash(filepath.Clean(item)))
	}
	for _, item := range add {
		clean := filepath.ToSlash(filepath.Clean(item))
		if clean != "." && clean != ".." && !strings.HasPrefix(clean, "../") {
			items[clean] = true
		}
	}
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	var lines []string
	for item := range items {
		lines = append(lines, item)
	}
	sort.Strings(lines)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}

func decodeStructToolArgs(raw json.RawMessage, into any) error {
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, into)
}

func toolJSONError(out Result, reason, detail string) Result {
	out.Ok = false
	out.Content = jsonString(map[string]any{"ok": false, "reason": reason, "detail": detail})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 3
	return out
}

func isToolText(data []byte) bool {
	return len(data) == 0 || (utf8.Valid(data) && !strings.Contains(string(data), "\x00"))
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

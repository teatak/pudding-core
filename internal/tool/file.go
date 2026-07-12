package tool

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/attachment"
	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/skill"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	managedScopeSkillDraft     = "skill_draft"
	managedScopeSkillPublished = "skill_published"
	managedScopeTemp           = "temp"
	managedScopeProject        = "project"

	defaultFileReadMaxChars       = 20000
	maxFileReadChars              = 100000
	maxFileReadWholeBytes         = 128 * 1024
	defaultFileListMax            = 200
	defaultFileSliceLines         = 100
	maxFileSliceLines             = 500
	maxFileSliceSkip              = 5000
	maxFileSlicePayload           = 64 * 1024
	maxFileSearchBytes            = 1 << 20
	maxFileSearchLineChars        = 200
	maxFileSearchExcerptLineChars = 400
	defaultFileSearchMax          = 100
	maxFileSearchMax              = 500
	maxFileSearchContextLines     = 5
	maxFileSearchGlobs            = 32
	maxFileSearchGlobChars        = 256
	maxFileSearchFiles            = 20000
)

type resolvedFilePath struct {
	root    string
	target  string
	rel     string
	project bool
}

func (p resolvedFilePath) outputPath() string {
	if p.project {
		return p.target
	}
	return p.rel
}

func (p resolvedFilePath) payload(base map[string]any) map[string]any {
	base["path"] = p.outputPath()
	if p.project {
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
		if resolved.project {
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

func (r *BuiltinRunner) fileStat(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope string `json:"scope"`
		Path  string `json:"path"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, true, true)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	info, err := os.Lstat(resolved.target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Ok = true
			out.Content = jsonString(resolved.payload(map[string]any{"ok": true, "scope": args.Scope, "exists": false}))
			out.SummaryKind = SummaryReturnedFields
			out.SummaryCount = 4
			return out
		}
		return toolJSONError(out, "stat_failed", err.Error())
	}
	kind := "file"
	switch {
	case info.IsDir():
		kind = "dir"
	case info.Mode()&os.ModeSymlink != 0:
		kind = "symlink"
	case !info.Mode().IsRegular():
		kind = "other"
	}
	payload := resolved.payload(map[string]any{
		"ok":     true,
		"scope":  args.Scope,
		"exists": true,
		"type":   kind,
		"size":   info.Size(),
		"mtime":  info.ModTime().UTC().Format(time.RFC3339),
	})
	if kind == "file" {
		if mt := sniffFileMIME(resolved.target); mt != "" {
			payload["mime"] = mt
		}
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func (r *BuiltinRunner) fileSearch(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope         string   `json:"scope"`
		Path          string   `json:"path"`
		Query         string   `json:"query"`
		Mode          string   `json:"mode"`
		CaseSensitive *bool    `json:"case_sensitive"`
		IncludeGlobs  []string `json:"include_globs"`
		ExcludeGlobs  []string `json:"exclude_globs"`
		ContextLines  int      `json:"context_lines"`
		MaxResults    int      `json:"max_results"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	args.Query = strings.TrimSpace(args.Query)
	if args.Query == "" {
		return toolJSONError(out, "query_required", "query must be a non-empty string")
	}
	mode := strings.ToLower(strings.TrimSpace(args.Mode))
	if mode == "" {
		mode = "literal"
	}
	if mode != "literal" && mode != "regex" {
		return toolJSONError(out, "invalid_search_mode", "mode must be literal or regex")
	}
	caseSensitive := true
	if args.CaseSensitive != nil {
		caseSensitive = *args.CaseSensitive
	}
	if args.ContextLines < 0 || args.ContextLines > maxFileSearchContextLines {
		return toolJSONError(out, "invalid_context_lines", "context_lines must be between 0 and 5")
	}
	matcher, err := newFileSearchMatcher(mode, args.Query, caseSensitive)
	if err != nil {
		return toolJSONError(out, "invalid_regex", err.Error())
	}
	if err := validateSearchGlobs(args.IncludeGlobs, args.ExcludeGlobs); err != nil {
		return toolJSONError(out, "invalid_glob", err.Error())
	}
	maxResults := args.MaxResults
	if maxResults <= 0 {
		maxResults = defaultFileSearchMax
	}
	if maxResults > maxFileSearchMax {
		maxResults = maxFileSearchMax
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, true, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	searchBaseRoot := resolved.root
	if evaluated, evaluateErr := filepath.EvalSymlinks(searchBaseRoot); evaluateErr == nil {
		searchBaseRoot = evaluated
	}
	options := fileSearchOptions{
		baseRoot:     searchBaseRoot,
		matcher:      matcher,
		includeGlobs: args.IncludeGlobs,
		excludeGlobs: args.ExcludeGlobs,
		contextLines: args.ContextLines,
	}
	matches, filesScanned, capped, err := searchTextFiles(resolved.target, options, maxResults)
	if err != nil {
		return toolJSONError(out, "search_failed", err.Error())
	}
	items := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		path := match.path
		if !resolved.project {
			if rel, err := filepath.Rel(resolved.root, match.path); err == nil {
				path = filepath.ToSlash(rel)
			}
		}
		items = append(items, map[string]any{
			"path":      path,
			"line":      match.line,
			"lineStart": match.lineStart,
			"lineEnd":   match.lineEnd,
			"text":      match.text,
			"excerpt":   match.excerpt,
			"truncated": match.truncated,
		})
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{
		"ok":            true,
		"scope":         args.Scope,
		"query":         args.Query,
		"matches":       items,
		"matchCount":    len(items),
		"filesScanned":  filesScanned,
		"resultsCapped": capped,
		"caseSensitive": caseSensitive,
		"searchType":    mode,
		"contextLines":  args.ContextLines,
		"includeGlobs":  args.IncludeGlobs,
		"excludeGlobs":  args.ExcludeGlobs,
	}))
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(items)
	return out
}

func (r *BuiltinRunner) fileSlice(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope  string `json:"scope"`
		Path   string `json:"path"`
		Origin string `json:"origin"`
		Start  int    `json:"start"`
		End    int    `json:"end"`
		Lines  int    `json:"lines"`
		Skip   int    `json:"skip"`
		Order  string `json:"order"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, false, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if isBinary, mt, err := probeBinaryFile(resolved.target); err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	} else if isBinary {
		if isReadableImageMIME(mt) {
			return r.fileReadImageAttachment(out, call, args.Scope, resolved)
		}
		return toolJSONError(out, "binary_file", "file is not UTF-8 text; mime="+mt)
	}
	origin := strings.TrimSpace(args.Origin)
	if origin == "" {
		origin = "start"
	}
	order := strings.TrimSpace(args.Order)
	if order == "" {
		order = "natural"
	}
	lines := args.Lines
	truncated := false
	if lines <= 0 {
		lines = defaultFileSliceLines
	}
	if lines > maxFileSliceLines {
		lines = maxFileSliceLines
		truncated = true
	}
	var slice fileLineSlice
	switch origin {
	case "start":
		start := args.Start
		end := args.End
		if start <= 0 {
			start = 1
		}
		if end <= 0 {
			end = start + lines - 1
		}
		if end < start {
			return toolJSONError(out, "invalid_line_range", "end must be greater than or equal to start")
		}
		if end-start+1 > maxFileSliceLines {
			end = start + maxFileSliceLines - 1
			truncated = true
		}
		slice, err = readLineRange(resolved.target, start, end)
	case "end":
		if args.Skip < 0 {
			return toolJSONError(out, "invalid_skip", "skip must be greater than or equal to 0")
		}
		if args.Skip > maxFileSliceSkip {
			return toolJSONError(out, "skip_too_large", "skip exceeds the maximum")
		}
		slice, err = readLineRangeFromEnd(resolved.target, lines, args.Skip)
	default:
		return toolJSONError(out, "invalid_origin", "origin must be start or end")
	}
	if err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	}
	switch order {
	case "natural":
	case "reverse":
		reverseLineRecords(slice.lines)
	default:
		return toolJSONError(out, "invalid_order", "order must be natural or reverse")
	}
	content, numbered, payloadTruncated := renderLineSlice(slice.lines, maxFileSlicePayload)
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{
		"ok":              true,
		"scope":           args.Scope,
		"origin":          origin,
		"order":           order,
		"start":           slice.start,
		"end":             slice.end,
		"lines":           len(slice.lines),
		"content":         content,
		"numberedContent": numbered,
		"truncated":       truncated || slice.truncated || payloadTruncated,
	}))
	out.SummaryKind = SummaryReadChars
	out.SummaryCount = utf8.RuneCountInString(content)
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
	if isBinary, mt, err := probeBinaryFile(resolved.target); err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	} else if isBinary {
		if isReadableImageMIME(mt) {
			return r.fileReadImageAttachment(out, call, args.Scope, resolved)
		}
		return toolJSONError(out, "binary_file", "file is not UTF-8 text; mime="+mt)
	}
	if info, err := os.Stat(resolved.target); err == nil && info.Mode().IsRegular() && info.Size() > maxFileReadWholeBytes {
		out.Ok = false
		out.Content = jsonString(resolved.payload(map[string]any{
			"ok":     false,
			"scope":  args.Scope,
			"reason": "file_too_large",
			"size":   info.Size(),
			"limit":  maxFileReadWholeBytes,
			"hint":   "Use builtin_file_slice for a line range or builtin_file_search to locate text before reading a focused slice.",
		}))
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 7
		return out
	}
	data, err := os.ReadFile(resolved.target)
	if err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	}
	if !isToolText(data) {
		mt := sniffBytesMIME(resolved.target, data)
		if isReadableImageMIME(mt) {
			return r.fileReadImageAttachment(out, call, args.Scope, resolved)
		}
		return toolJSONError(out, "binary_file", "file is not UTF-8 text; mime="+mt)
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

func (r *BuiltinRunner) fileReadImageAttachment(out Result, call Call, scope string, resolved resolvedFilePath) Result {
	if strings.TrimSpace(call.SessionID) == "" {
		return toolJSONError(out, "session_required", "session id is required to route image attachments")
	}
	stored, err := attachment.NewService(r.homeDir).StorePath(call.SessionID, resolved.target)
	if err != nil {
		return toolJSONError(out, "attachment_store_failed", err.Error())
	}
	stored = attachment.WithSourcePath(stored, resolved.target)
	stored.Origin = attachment.OriginTool
	out.Ok = true
	out.Attachments = []store.Attachment{stored}
	out.ContextAttachments = []store.Attachment{stored}
	out.Content = jsonString(resolved.payload(map[string]any{
		"ok":            true,
		"scope":         scope,
		"kind":          "attachment_routed",
		"mime":          stored.MIME,
		"size":          stored.Size,
		"attachmentKey": stored.AttachmentKey,
		"url":           stored.URL,
		"hint":          "image was saved as an attachment; image bytes are visible only to models with image input support. If the current model lacks image support, use the metadata only and do not describe visual contents.",
	}))
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 8
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
	if fromResolved.project {
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

func (r *BuiltinRunner) fileCopy(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope     string `json:"scope"`
		FromPath  string `json:"from_path"`
		ToPath    string `json:"to_path"`
		Recursive bool   `json:"recursive"`
		Overwrite bool   `json:"overwrite"`
	}
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	fromResolved, err := r.resolveFilePath(call, args.Scope, args.FromPath, false, false, false)
	if err != nil {
		return filePathErrorWithReason(out, args.Scope, "from_path_not_allowed", err)
	}
	toResolved, err := r.resolveFilePath(call, args.Scope, args.ToPath, true, false, true)
	if err != nil {
		return filePathErrorWithReason(out, args.Scope, "to_path_not_allowed", err)
	}
	if fromResolved.root != toResolved.root {
		return toolJSONError(out, "cross_root_copy", "from_path and to_path must be inside the same authorized root")
	}
	if filepath.Clean(fromResolved.target) == filepath.Clean(toResolved.target) {
		return toolJSONError(out, "same_path", "source and destination are the same path")
	}
	info, err := os.Lstat(fromResolved.target)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return toolJSONError(out, "from_not_found", "source path does not exist")
		}
		return toolJSONError(out, "stat_failed", err.Error())
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return toolJSONError(out, "symlink_unsupported", "copying symlinks is not supported")
	}
	if info.IsDir() {
		if !args.Recursive {
			return toolJSONError(out, "recursive_required", "recursive=true is required to copy a directory")
		}
		fromDir := filepath.Clean(fromResolved.target) + string(os.PathSeparator)
		toDir := filepath.Clean(toResolved.target) + string(os.PathSeparator)
		if strings.HasPrefix(toDir, fromDir) {
			return toolJSONError(out, "copy_into_self", "cannot copy a directory into itself or its descendants")
		}
		if err := prepareFileCopyDestination(toResolved.target, args.Overwrite); err != nil {
			return fileCopyDestinationError(out, toResolved.outputPath(), err)
		}
		if err := copyFileDir(fromResolved.target, toResolved.target); err != nil {
			return toolJSONError(out, "copy_failed", err.Error())
		}
		if args.Scope == managedScopeSkillDraft {
			_ = r.removeDraftDelete(toResolved.rel)
		}
		payload := map[string]any{"ok": true, "scope": args.Scope, "from": fromResolved.outputPath(), "to": toResolved.outputPath(), "copied": "directory"}
		if fromResolved.project {
			payload["fromRoot"] = fromResolved.root
			payload["fromRelativePath"] = fromResolved.rel
			payload["toRoot"] = toResolved.root
			payload["toRelativePath"] = toResolved.rel
		}
		out.Ok = true
		out.Content = jsonString(payload)
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = len(payload)
		return out
	}
	if !info.Mode().IsRegular() {
		return toolJSONError(out, "unsupported_file_type", "copy supports regular files and directories only")
	}
	if err := prepareFileCopyDestination(toResolved.target, args.Overwrite); err != nil {
		return fileCopyDestinationError(out, toResolved.outputPath(), err)
	}
	if err := copyFileBytes(fromResolved.target, toResolved.target, info); err != nil {
		return toolJSONError(out, "copy_failed", err.Error())
	}
	if args.Scope == managedScopeSkillDraft {
		_ = r.removeDraftDelete(toResolved.rel)
	}
	payload := map[string]any{"ok": true, "scope": args.Scope, "from": fromResolved.outputPath(), "to": toResolved.outputPath(), "copied": "file", "bytes": info.Size()}
	if fromResolved.project {
		payload["fromRoot"] = fromResolved.root
		payload["fromRelativePath"] = fromResolved.rel
		payload["toRoot"] = toResolved.root
		payload["toRelativePath"] = toResolved.rel
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

var errFileCopyDestinationExists = errors.New("copy destination exists")

func prepareFileCopyDestination(dst string, overwrite bool) error {
	if _, err := os.Lstat(dst); err == nil {
		if !overwrite {
			return errFileCopyDestinationExists
		}
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.MkdirAll(filepath.Dir(dst), 0o700)
}

func fileCopyDestinationError(out Result, to string, err error) Result {
	if errors.Is(err, errFileCopyDestinationExists) {
		out.Ok = false
		out.Content = jsonString(map[string]any{"ok": false, "reason": "to_exists", "to": to, "hint": "destination exists; pass overwrite=true to replace it"})
		out.SummaryKind = SummaryReturnedFields
		out.SummaryCount = 4
		return out
	}
	return toolJSONError(out, "copy_failed", err.Error())
}

func copyFileBytes(src, dst string, info os.FileInfo) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".copy-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := io.Copy(tmp, in); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return err
	}
	cleanup = false
	return nil
}

func copyFileDir(src, dst string) error {
	rootInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, rootInfo.Mode().Perm()); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("copying symlinks is not supported")
		}
		if entry.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return errors.New("copy supports regular files and directories only")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return copyFileBytes(path, target, info)
	})
}

func (r *BuiltinRunner) resolveFilePath(call Call, scope, rawPath string, requireWritable, allowRoot, allowMissing bool) (resolvedFilePath, error) {
	if isProjectFileScope(scope) {
		root, target, rel, err := resolveProjectPath(call.ProjectDirs, rawPath, allowRoot, allowMissing)
		if err != nil {
			return resolvedFilePath{}, err
		}
		return resolvedFilePath{root: root, target: target, rel: rel, project: true}, nil
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
	if !isProjectFileScope(scope) {
		return toolJSONError(out, fallbackReason, err.Error())
	}
	reason := "path_not_authorized"
	hint := "The path is outside authorized project directories. Use request_capability with targetMode=code and projectDirs containing the directory, then ask the user to approve it for this turn if temporary access is enough."
	if errors.Is(err, errProjectDirsRequired) {
		reason = "project_dirs_required"
		hint = "No project directories are authorized. Use request_capability with targetMode=code and projectDirs, then ask the user to approve it for this turn if temporary access is enough."
	} else if errors.Is(err, errProjectFilePathRequired) {
		reason = "path_not_allowed"
		hint = "A file path is required for this tool."
	}
	out.Ok = false
	out.Content = jsonString(map[string]any{"ok": false, "reason": reason, "detail": err.Error(), "hint": hint})
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = 4
	return out
}

func isProjectFileScope(scope string) bool {
	return strings.TrimSpace(scope) == managedScopeProject
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

type fileSearchMatch struct {
	path      string
	line      int
	lineStart int
	lineEnd   int
	text      string
	excerpt   string
	truncated bool
}

type fileSearchMatcher struct {
	caseSensitive bool
	literal       string
	regex         *regexp.Regexp
}

type fileSearchOptions struct {
	baseRoot     string
	matcher      fileSearchMatcher
	includeGlobs []string
	excludeGlobs []string
	contextLines int
}

var fileSearchSkipDirs = map[string]struct{}{
	".cache":        {},
	".git":          {},
	".hg":           {},
	".next":         {},
	".pytest_cache": {},
	".svn":          {},
	".turbo":        {},
	".vscode":       {},
	"__pycache__":   {},
	"build":         {},
	"dist":          {},
	"node_modules":  {},
}

var binaryFileExts = map[string]string{
	".7z":    "application/x-7z-compressed",
	".a":     "application/octet-stream",
	".avi":   "video/x-msvideo",
	".bin":   "application/octet-stream",
	".bmp":   "image/bmp",
	".bz2":   "application/x-bzip2",
	".class": "application/java-vm",
	".dll":   "application/octet-stream",
	".doc":   "application/msword",
	".docx":  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	".dylib": "application/octet-stream",
	".eot":   "application/vnd.ms-fontobject",
	".exe":   "application/octet-stream",
	".flac":  "audio/flac",
	".gif":   "image/gif",
	".gz":    "application/gzip",
	".heic":  "image/heic",
	".heif":  "image/heif",
	".ico":   "image/x-icon",
	".jar":   "application/java-archive",
	".jpeg":  "image/jpeg",
	".jpg":   "image/jpeg",
	".m4a":   "audio/mp4",
	".mkv":   "video/x-matroska",
	".mov":   "video/quicktime",
	".mp3":   "audio/mpeg",
	".mp4":   "video/mp4",
	".ogg":   "audio/ogg",
	".onnx":  "application/octet-stream",
	".otf":   "font/otf",
	".pdf":   "application/pdf",
	".png":   "image/png",
	".pt":    "application/octet-stream",
	".pth":   "application/octet-stream",
	".rar":   "application/vnd.rar",
	".so":    "application/octet-stream",
	".tar":   "application/x-tar",
	".ttf":   "font/ttf",
	".wav":   "audio/wav",
	".webm":  "video/webm",
	".webp":  "image/webp",
	".woff":  "font/woff",
	".woff2": "font/woff2",
	".xls":   "application/vnd.ms-excel",
	".xlsx":  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	".zip":   "application/zip",
}

func newFileSearchMatcher(mode, query string, caseSensitive bool) (fileSearchMatcher, error) {
	matcher := fileSearchMatcher{caseSensitive: caseSensitive, literal: query}
	if mode == "regex" {
		pattern := query
		if !caseSensitive {
			pattern = "(?i:" + pattern + ")"
		}
		compiled, err := regexp.Compile(pattern)
		if err != nil {
			return fileSearchMatcher{}, err
		}
		matcher.regex = compiled
	} else if !caseSensitive {
		matcher.literal = strings.ToLower(query)
	}
	return matcher, nil
}

func (m fileSearchMatcher) matches(line string) bool {
	if m.regex != nil {
		return m.regex.MatchString(line)
	}
	if !m.caseSensitive {
		line = strings.ToLower(line)
	}
	return strings.Contains(line, m.literal)
}

func validateSearchGlobs(groups ...[]string) error {
	for _, patterns := range groups {
		if len(patterns) > maxFileSearchGlobs {
			return errors.New("include_globs and exclude_globs each support at most 32 patterns")
		}
		for _, pattern := range patterns {
			pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
			if pattern == "" {
				return errors.New("glob patterns must not be empty")
			}
			if utf8.RuneCountInString(pattern) > maxFileSearchGlobChars {
				return errors.New("glob patterns must not exceed 256 characters")
			}
			if strings.HasPrefix(pattern, "/") {
				return errors.New("glob patterns must be project-relative")
			}
			for _, segment := range strings.Split(pattern, "/") {
				if segment == "**" {
					continue
				}
				if _, err := path.Match(segment, ""); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func searchTextFiles(root string, options fileSearchOptions, maxResults int) ([]fileSearchMatch, int, bool, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return nil, 0, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, false, errors.New("symlink search is not supported")
	}
	if !info.IsDir() {
		if !searchPathAllowed(root, options) {
			return nil, 0, false, nil
		}
		matches, scanned, err := searchTextFile(root, options, maxResults)
		return matches, scanned, len(matches) >= maxResults, err
	}
	var matches []fileSearchMatch
	filesScanned := 0
	capped := false
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root {
				if _, skip := fileSearchSkipDirs[entry.Name()]; skip {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !searchPathAllowed(path, options) {
			return nil
		}
		if filesScanned >= maxFileSearchFiles {
			capped = true
			return filepath.SkipAll
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() > maxFileSearchBytes {
			return nil
		}
		if isKnownBinaryExt(path) {
			return nil
		}
		fileMatches, scanned, err := searchTextFile(path, options, maxResults-len(matches))
		filesScanned += scanned
		if err != nil {
			return nil
		}
		matches = append(matches, fileMatches...)
		if len(matches) >= maxResults {
			capped = true
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !errors.Is(err, filepath.SkipAll) {
		return nil, filesScanned, capped, err
	}
	return matches, filesScanned, capped, nil
}

func searchPathAllowed(filePath string, options fileSearchOptions) bool {
	rel, err := filepath.Rel(options.baseRoot, filePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	if len(options.includeGlobs) > 0 && !matchesAnySearchGlob(options.includeGlobs, rel) {
		return false
	}
	return !matchesAnySearchGlob(options.excludeGlobs, rel)
}

func matchesAnySearchGlob(patterns []string, rel string) bool {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(strings.ReplaceAll(pattern, "\\", "/"))
		if !strings.Contains(pattern, "/") {
			matched, _ := path.Match(pattern, path.Base(rel))
			if matched {
				return true
			}
			continue
		}
		if matchSearchGlob(strings.Split(pattern, "/"), strings.Split(rel, "/")) {
			return true
		}
	}
	return false
}

func matchSearchGlob(pattern, value []string) bool {
	type state struct{ pattern, value int }
	memo := make(map[state]bool)
	visited := make(map[state]bool)
	var match func(int, int) bool
	match = func(patternIndex, valueIndex int) bool {
		key := state{pattern: patternIndex, value: valueIndex}
		if visited[key] {
			return memo[key]
		}
		visited[key] = true
		matched := false
		switch {
		case patternIndex == len(pattern):
			matched = valueIndex == len(value)
		case pattern[patternIndex] == "**":
			matched = match(patternIndex+1, valueIndex) || (valueIndex < len(value) && match(patternIndex, valueIndex+1))
		case valueIndex < len(value):
			segmentMatched, err := path.Match(pattern[patternIndex], value[valueIndex])
			matched = err == nil && segmentMatched && match(patternIndex+1, valueIndex+1)
		}
		memo[key] = matched
		return matched
	}
	return match(0, 0)
}

func searchTextFile(filePath string, options fileSearchOptions, remaining int) ([]fileSearchMatch, int, error) {
	if remaining <= 0 {
		return nil, 0, nil
	}
	if isBinary, _, err := probeBinaryFile(filePath); err != nil || isBinary {
		return nil, 0, err
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, 256)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, 1, err
	}
	matches := make([]fileSearchMatch, 0)
	for index, line := range lines {
		if !options.matcher.matches(line) {
			continue
		}
		start := max(0, index-options.contextLines)
		end := min(len(lines), index+options.contextLines+1)
		text, textTruncated := truncateSearchLine(line, maxFileSearchLineChars)
		excerptLines := make([]string, 0, end-start)
		truncated := textTruncated
		for _, excerptLine := range lines[start:end] {
			short, wasTruncated := truncateSearchLine(excerptLine, maxFileSearchExcerptLineChars)
			excerptLines = append(excerptLines, short)
			truncated = truncated || wasTruncated
		}
		matches = append(matches, fileSearchMatch{
			path:      filePath,
			line:      index + 1,
			lineStart: start + 1,
			lineEnd:   end,
			text:      text,
			excerpt:   strings.Join(excerptLines, "\n"),
			truncated: truncated,
		})
		if len(matches) >= remaining {
			break
		}
	}
	return matches, 1, nil
}

func truncateSearchLine(value string, limit int) (string, bool) {
	if utf8.RuneCountInString(value) <= limit {
		return value, false
	}
	return string([]rune(value)[:limit]), true
}

type fileLineRecord struct {
	number int
	text   string
}

type fileLineSlice struct {
	start     int
	end       int
	lines     []fileLineRecord
	truncated bool
}

func readLineRange(path string, start, end int) (fileLineSlice, error) {
	file, err := os.Open(path)
	if err != nil {
		return fileLineSlice{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	out := make([]fileLineRecord, 0, end-start+1)
	for scanner.Scan() {
		lineNo++
		if lineNo < start {
			continue
		}
		if lineNo > end {
			break
		}
		out = append(out, fileLineRecord{number: lineNo, text: scanner.Text()})
	}
	if err := scanner.Err(); err != nil {
		return fileLineSlice{}, err
	}
	actualStart, actualEnd := 0, 0
	if len(out) > 0 {
		actualStart = out[0].number
		actualEnd = out[len(out)-1].number
	}
	return fileLineSlice{start: actualStart, end: actualEnd, lines: out}, nil
}

func readLineRangeFromEnd(path string, lines, skip int) (fileLineSlice, error) {
	file, err := os.Open(path)
	if err != nil {
		return fileLineSlice{}, err
	}
	defer file.Close()
	windowSize := lines + skip
	if windowSize <= 0 {
		windowSize = lines
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	window := make([]fileLineRecord, 0, windowSize)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		window = append(window, fileLineRecord{number: lineNo, text: scanner.Text()})
		if len(window) > windowSize {
			copy(window, window[1:])
			window = window[:len(window)-1]
		}
	}
	if err := scanner.Err(); err != nil {
		return fileLineSlice{}, err
	}
	end := len(window) - skip
	if end < 0 {
		end = 0
	}
	start := end - lines
	if start < 0 {
		start = 0
	}
	out := append([]fileLineRecord(nil), window[start:end]...)
	actualStart, actualEnd := 0, 0
	if len(out) > 0 {
		actualStart = out[0].number
		actualEnd = out[len(out)-1].number
	}
	return fileLineSlice{start: actualStart, end: actualEnd, lines: out}, nil
}

func renderLineSlice(lines []fileLineRecord, maxChars int) (string, string, bool) {
	var content strings.Builder
	var numbered strings.Builder
	truncated := false
	for i, line := range lines {
		next := line.text
		prefix := ""
		if i > 0 {
			prefix = "\n"
		}
		numberedLine := prefix + strconv.Itoa(line.number) + ": " + line.text
		contentLine := prefix + next
		if content.Len()+len(contentLine) > maxChars || numbered.Len()+len(numberedLine) > maxChars {
			truncated = true
			break
		}
		content.WriteString(contentLine)
		numbered.WriteString(numberedLine)
	}
	return content.String(), numbered.String(), truncated
}

func reverseLineRecords(lines []fileLineRecord) {
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
}

func probeBinaryFile(path string) (bool, string, error) {
	if isKnownBinaryExt(path) {
		return true, sniffBytesMIME(path, nil), nil
	}
	file, err := os.Open(path)
	if err != nil {
		return false, "", err
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, err := file.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, "", err
	}
	mt := sniffBytesMIME(path, buf[:n])
	if n == 0 {
		return false, mt, nil
	}
	if !validUTF8ProbeSample(buf[:n]) || strings.Contains(string(buf[:n]), "\x00") {
		return true, mt, nil
	}
	main := strings.ToLower(strings.TrimSpace(strings.Split(mt, ";")[0]))
	switch {
	case strings.HasPrefix(main, "text/"),
		main == "application/json",
		main == "application/xml",
		main == "application/javascript",
		main == "application/x-yaml":
		return false, main, nil
	case strings.HasPrefix(main, "image/"),
		strings.HasPrefix(main, "audio/"),
		strings.HasPrefix(main, "video/"),
		strings.HasPrefix(main, "font/"),
		main == "application/pdf",
		main == "application/zip",
		main == "application/gzip":
		return true, main, nil
	}
	return false, main, nil
}

func sniffFileMIME(path string) string {
	file, err := os.Open(path)
	if err != nil {
		return sniffBytesMIME(path, nil)
	}
	defer file.Close()
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	return sniffBytesMIME(path, buf[:n])
}

func sniffBytesMIME(path string, data []byte) string {
	ext := strings.ToLower(filepath.Ext(path))
	if mt, ok := binaryFileExts[ext]; ok {
		return mt
	}
	if mt := mime.TypeByExtension(ext); mt != "" {
		if main, _, ok := strings.Cut(mt, ";"); ok {
			return strings.TrimSpace(main)
		}
		return strings.TrimSpace(mt)
	}
	if len(data) > 0 {
		mt := http.DetectContentType(data[:min(len(data), 512)])
		if main, _, ok := strings.Cut(mt, ";"); ok {
			return strings.TrimSpace(main)
		}
		return strings.TrimSpace(mt)
	}
	return ""
}

func isReadableImageMIME(mimeType string) bool {
	mimeType = strings.ToLower(strings.TrimSpace(mimeType))
	return strings.HasPrefix(mimeType, "image/") && mimeType != "image/svg+xml"
}

func isKnownBinaryExt(path string) bool {
	_, ok := binaryFileExts[strings.ToLower(filepath.Ext(path))]
	return ok
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

func validUTF8ProbeSample(data []byte) bool {
	if utf8.Valid(data) {
		return true
	}
	for trim := 1; trim < utf8.UTFMax && trim < len(data); trim++ {
		prefix := data[:len(data)-trim]
		tail := data[len(data)-trim:]
		if utf8.Valid(prefix) && incompleteUTF8Tail(tail) {
			return true
		}
	}
	return false
}

func incompleteUTF8Tail(tail []byte) bool {
	if len(tail) == 0 {
		return false
	}
	need := utf8SequenceLen(tail[0])
	if need == 0 || need <= len(tail) {
		return false
	}
	for _, b := range tail[1:] {
		if b&0xc0 != 0x80 {
			return false
		}
	}
	return true
}

func utf8SequenceLen(first byte) int {
	switch {
	case first < utf8.RuneSelf:
		return 1
	case first >= 0xc2 && first <= 0xdf:
		return 2
	case first >= 0xe0 && first <= 0xef:
		return 3
	case first >= 0xf0 && first <= 0xf4:
		return 4
	default:
		return 0
	}
}

func countLines(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

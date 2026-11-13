package tool

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	"github.com/teatak/pudding-core/internal/home"
	"github.com/teatak/pudding-core/internal/skill"
)

const (
	managedScopePlugin  = "plugin"
	managedScopeSkill   = "skill"
	managedScopeTemp    = "temp"
	managedScopeProject = "project"
	managedScopeWidget  = "widget"

	defaultFileReadMaxChars       = 20000
	maxFileReadChars              = 100000
	maxFileReadWholeBytes         = 128 * 1024
	defaultFileListMax            = 200
	defaultFileSliceLines         = 100
	maxFileSliceLines             = 500
	maxFileSliceSkip              = 5000
	maxFileSlicePayload           = 64 * 1024
	maxFileSearchBytes            = 1 << 20
	maxFileSearchDirectBytes      = 16 << 20
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
	root      string
	target    string
	rel       string
	project   bool
	widgetID  string
	draftHash string
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
	if p.widgetID != "" {
		base["widgetID"] = p.widgetID
		base["draftHash"] = p.draftHash
	}
	return base
}

func (r *BuiltinRunner) fileList(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		WidgetID       string   `json:"widget_id,omitempty"`
		Scope          string   `json:"scope"`
		Path           string   `json:"path"`
		MaxEntries     int      `json:"max_entries"`
		Cursor         string   `json:"cursor"`
		Recursive      bool     `json:"recursive"`
		IncludeGlobs   []string `json:"include_globs"`
		ExcludeGlobs   []string `json:"exclude_globs"`
		IncludeIgnored bool     `json:"include_ignored"`
	}
	if err := decodeToolArgumentObject(call.Args, &args); err != nil {
		return toolArgumentFailure(out, err)
	}
	args.Scope = strings.TrimSpace(args.Scope)
	maxEntries := args.MaxEntries
	if maxEntries <= 0 {
		maxEntries = defaultFileListMax
	}
	if maxEntries > 1000 {
		maxEntries = 1000
	}
	if err := validateSearchGlobs(args.IncludeGlobs, args.ExcludeGlobs); err != nil {
		return toolJSONError(out, "invalid_glob", err.Error())
	}
	if !args.Recursive && (len(args.IncludeGlobs) > 0 || len(args.ExcludeGlobs) > 0 || args.IncludeIgnored || args.Cursor != "") {
		return toolJSONError(out, "invalid_arguments", "glob filters, include_ignored and cursor require recursive=true")
	}
	if !args.Recursive && isProjectFileScope(args.Scope) && projectRootListRequested(args.Path) {
		if projectRoots := normalizeProjectDirs(call.ProjectDirs); len(projectRoots) > 1 {
			return projectRootListResult(out, projectRoots, maxEntries)
		}
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, true, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if args.Recursive {
		return r.fileListRecursive(ctx, out, resolved, maxEntries, args.IncludeGlobs, args.ExcludeGlobs, args.IncludeIgnored, args.Cursor)
	}
	entries, err := os.ReadDir(resolved.target)
	if err != nil {
		return toolJSONError(out, "read_dir_failed", err.Error())
	}
	if args.Scope == managedScopeSkill || args.Scope == managedScopePlugin || args.Scope == managedScopeTemp || args.Scope == managedScopeWidget {
		visible := entries[:0]
		for _, entry := range entries {
			if !strings.HasPrefix(entry.Name(), ".") && (args.Scope != managedScopeWidget || widgetVisiblePath(filepath.ToSlash(filepath.Join(resolved.rel, entry.Name())), entry.IsDir())) {
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

func projectRootListRequested(rawPath string) bool {
	rawPath = strings.TrimSpace(rawPath)
	return rawPath == "" || rawPath == "."
}

func projectRootListResult(out Result, roots []string, maxEntries int) Result {
	items := make([]map[string]any, 0, min(len(roots), maxEntries))
	for index, root := range roots {
		if index >= maxEntries {
			break
		}
		items = append(items, map[string]any{
			"name": filepath.Base(root),
			"path": root,
			"root": root,
			"type": "dir",
		})
	}
	out.Ok = true
	out.Content = jsonString(map[string]any{
		"ok":           true,
		"scope":        managedScopeProject,
		"path":         ".",
		"entries":      items,
		"projectRoots": roots,
		"rootCount":    len(roots),
		"truncated":    len(roots) > maxEntries,
		"totalCount":   len(roots),
	})
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(items)
	return out
}

func (r *BuiltinRunner) fileStat(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		WidgetID string `json:"widget_id,omitempty"`
		Scope    string `json:"scope"`
		Path     string `json:"path"`
	}
	if err := decodeToolArgumentObject(call.Args, &args); err != nil {
		return toolArgumentFailure(out, err)
	}
	args.Scope = strings.TrimSpace(args.Scope)
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

func (r *BuiltinRunner) fileSearch(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		WidgetID       string   `json:"widget_id,omitempty"`
		Scope          string   `json:"scope"`
		Path           string   `json:"path"`
		Query          string   `json:"query"`
		Mode           string   `json:"mode"`
		CaseSensitive  *bool    `json:"case_sensitive"`
		IncludeGlobs   []string `json:"include_globs"`
		ExcludeGlobs   []string `json:"exclude_globs"`
		ContextLines   int      `json:"context_lines"`
		MaxResults     int      `json:"max_results"`
		Cursor         string   `json:"cursor"`
		OutputMode     string   `json:"output_mode"`
		IncludeIgnored bool     `json:"include_ignored"`
	}
	if err := decodeToolArgumentObject(call.Args, &args); err != nil {
		return toolArgumentFailure(out, err)
	}
	args.Scope = strings.TrimSpace(args.Scope)
	caseSensitive := true
	if args.CaseSensitive != nil {
		caseSensitive = *args.CaseSensitive
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, true, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	excludeGlobs := args.ExcludeGlobs
	if args.Scope == managedScopeWidget {
		if len(excludeGlobs) >= maxFileSearchGlobs {
			return toolJSONError(out, "too_many_globs", "widget search reserves one exclude glob for private draft files")
		}
		excludeGlobs = append(append([]string(nil), excludeGlobs...), "fixtures/**")
	}
	result, err := SearchTextFiles(ctx, resolved.target, TextFileSearchOptions{
		BaseRoot:       resolved.root,
		CaseSensitive:  caseSensitive,
		ContextLines:   args.ContextLines,
		ExcludeGlobs:   excludeGlobs,
		IncludeGlobs:   args.IncludeGlobs,
		MaxResults:     args.MaxResults,
		Cursor:         args.Cursor,
		Mode:           args.Mode,
		Query:          args.Query,
		SkipHidden:     args.Scope == managedScopeSkill || args.Scope == managedScopePlugin || args.Scope == managedScopeTemp || args.Scope == managedScopeWidget,
		OutputMode:     args.OutputMode,
		IncludeIgnored: args.IncludeIgnored,
	})
	if err != nil {
		var searchErr *TextFileSearchError
		if errors.As(err, &searchErr) {
			return toolJSONError(out, searchErr.Code, searchErr.Detail)
		}
		return toolJSONError(out, "search_failed", err.Error())
	}
	items := make([]map[string]any, 0, len(result.Matches))
	for _, match := range result.Matches {
		path := match.Path
		if !resolved.project {
			if rel, err := filepath.Rel(resolved.root, match.Path); err == nil {
				path = filepath.ToSlash(rel)
			}
		}
		if args.Scope == managedScopeWidget && !widgetVisiblePath(path, false) {
			continue
		}
		items = append(items, map[string]any{
			"path":      path,
			"line":      match.Line,
			"lineStart": match.LineStart,
			"lineEnd":   match.LineEnd,
			"text":      match.Text,
			"excerpt":   match.Excerpt,
			"truncated": match.Truncated,
		})
	}
	out.Ok = true
	payload := resolved.payload(map[string]any{
		"ok":             true,
		"scope":          args.Scope,
		"query":          result.Query,
		"matches":        items,
		"matchCount":     len(items),
		"filesScanned":   result.FilesScanned,
		"resultsCapped":  result.ResultsCapped,
		"nextCursor":     result.NextCursor,
		"complete":       result.Complete,
		"skipped":        result.Skipped,
		"caseSensitive":  result.CaseSensitive,
		"searchType":     result.Mode,
		"contextLines":   result.ContextLines,
		"includeGlobs":   result.IncludeGlobs,
		"excludeGlobs":   result.ExcludeGlobs,
		"outputMode":     result.OutputMode,
		"includeIgnored": result.IncludeIgnored,
	})
	if result.OutputMode == "files" {
		files := make([]string, 0, len(items))
		for _, item := range items {
			files = append(files, item["path"].(string))
		}
		delete(payload, "matches")
		delete(payload, "matchCount")
		payload["files"], payload["fileCount"] = files, len(files)
	}
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedItems
	out.SummaryCount = len(items)
	return out
}

func (r *BuiltinRunner) fileSlice(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		WidgetID string `json:"widget_id,omitempty"`
		Scope    string `json:"scope"`
		Path     string `json:"path"`
		Origin   string `json:"origin"`
		Start    int    `json:"start"`
		End      int    `json:"end"`
		Lines    int    `json:"lines"`
		Skip     int    `json:"skip"`
		Order    string `json:"order"`
	}
	if err := decodeToolArgumentObject(call.Args, &args); err != nil {
		return toolArgumentFailure(out, err)
	}
	args.Scope = strings.TrimSpace(args.Scope)
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, false, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if isBinary, mt, err := probeBinaryFile(resolved.target); err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	} else if isBinary {
		if isMediaMIME(mt) {
			return mediaRequiredForTextTool(out, mt)
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
	var records []fileLineRecord
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
		records, err = readLineRange(resolved.target, start, end)
	case "end":
		if args.Skip < 0 {
			return toolJSONError(out, "invalid_skip", "skip must be greater than or equal to 0")
		}
		if args.Skip > maxFileSliceSkip {
			return toolJSONError(out, "skip_too_large", "skip exceeds the maximum")
		}
		records, err = readLineRangeFromEnd(resolved.target, lines, args.Skip)
	default:
		return toolJSONError(out, "invalid_origin", "origin must be start or end")
	}
	if err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	}
	switch order {
	case "natural":
	case "reverse":
		reverseLineRecords(records)
	default:
		return toolJSONError(out, "invalid_order", "order must be natural or reverse")
	}
	content, numbered, renderedLines := renderLineSlice(records, maxFileSlicePayload)
	truncated = truncated || renderedLines < len(records)
	start, end := 0, 0
	if renderedLines > 0 {
		first, last := records[0].number, records[renderedLines-1].number
		start, end = min(first, last), max(first, last)
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{
		"ok":              true,
		"scope":           args.Scope,
		"origin":          origin,
		"order":           order,
		"start":           start,
		"end":             end,
		"lines":           renderedLines,
		"content":         content,
		"numberedContent": numbered,
		"truncated":       truncated,
	}))
	out.SummaryKind = SummaryReadChars
	out.SummaryCount = utf8.RuneCountInString(content)
	return out
}

func (r *BuiltinRunner) fileRead(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		WidgetID string `json:"widget_id,omitempty"`
		Scope    string `json:"scope"`
		Path     string `json:"path"`
		MaxChars int    `json:"max_chars"`
	}
	if err := decodeToolArgumentObject(call.Args, &args); err != nil {
		return toolArgumentFailure(out, err)
	}
	args.Scope = strings.TrimSpace(args.Scope)
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, false, false, false)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if isBinary, mt, err := probeBinaryFile(resolved.target); err != nil {
		return toolJSONError(out, "read_failed", err.Error())
	} else if isBinary {
		if isMediaMIME(mt) {
			return mediaRequiredForTextTool(out, mt)
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
		if isMediaMIME(mt) {
			return mediaRequiredForTextTool(out, mt)
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
		"ok":          true,
		"scope":       args.Scope,
		"content":     content,
		"contentHash": patchContentHash(data),
		"truncated":   truncated,
		"chars":       len([]rune(string(data))),
	}))
	out.SummaryKind = SummaryReadChars
	out.SummaryCount = len([]rune(content))
	return out
}

func (r *BuiltinRunner) fileWrite(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args struct {
		Scope        string  `json:"scope"`
		Path         string  `json:"path"`
		Content      *string `json:"content"`
		ExpectedHash string  `json:"expected_hash,omitempty"`
	}
	if err := decodeToolArgumentObject(call.Args, &args,
		toolArgumentField{"scope", "string"}, toolArgumentField{"path", "string"}, toolArgumentField{"content", "string"}); err != nil {
		return toolArgumentFailure(out, err)
	}
	args.Scope = strings.TrimSpace(args.Scope)
	args.Path = strings.TrimSpace(args.Path)
	for _, field := range []struct{ name, value string }{{"scope", args.Scope}, {"path", args.Path}} {
		if field.value == "" {
			return toolArgumentFailure(out, &toolArgumentError{
				kind: "invalid_value", detail: field.name + " must be a non-empty string", field: field.name,
				expected: "non-empty string", hint: "Set the required " + field.name + " field and retry.", receivedBytes: len(call.Args),
			})
		}
	}
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, true, false, true)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	// Serialize compare-and-write across sessions sharing this runner. Engine
	// resource locks additionally serialize this with other project mutations.
	r.fileWriteMu.Lock()
	defer r.fileWriteMu.Unlock()
	if resolved.project || args.ExpectedHash != "" {
		if args.ExpectedHash != "" {
			if len(args.ExpectedHash) != 64 || strings.Trim(args.ExpectedHash, "0123456789abcdef") != "" {
				return toolJSONError(out, "invalid_arguments", "expected_hash must be the SHA-256 contentHash returned by file_read")
			}
		}
		currentHash, readErr := hashFileContent(resolved.target)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return toolJSONError(out, "read_failed", readErr.Error())
		}
		if readErr == nil && args.ExpectedHash == "" {
			return toolJSON(out, false, resolved.payload(map[string]any{"ok": false, "reason": "file_version_required", "hint": "Read the current file, then pass its contentHash as expected_hash to replace it; use file_patch for focused edits."}))
		}
		if args.ExpectedHash != "" && (readErr != nil || currentHash != args.ExpectedHash) {
			return toolJSON(out, false, resolved.payload(map[string]any{"ok": false, "reason": "file_conflict", "hint": "The file changed or was removed since it was read. Read it again and merge your changes before retrying."}))
		}
	}
	if err := os.MkdirAll(filepath.Dir(resolved.target), 0o700); err != nil {
		return toolJSONError(out, "mkdir_failed", err.Error())
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if resolved.project && args.ExpectedHash == "" {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	} else if args.ExpectedHash != "" {
		flags = os.O_WRONLY | os.O_TRUNC
	}
	file, err := os.OpenFile(resolved.target, flags, 0o600)
	if errors.Is(err, os.ErrExist) || args.ExpectedHash != "" && errors.Is(err, os.ErrNotExist) {
		return toolJSONError(out, "file_conflict", "file changed before writing; read it again before retrying")
	}
	if err != nil {
		return toolJSONError(out, "write_failed", err.Error())
	}
	_, writeErr := file.WriteString(*args.Content)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return toolJSONError(out, "write_failed", err.Error())
	}
	out.Ok = true
	out.Content = jsonString(resolved.payload(map[string]any{"ok": true, "scope": args.Scope, "bytes": len([]byte(*args.Content)), "contentHash": patchContentHash([]byte(*args.Content))}))
	out.SummaryKind = SummaryChangedLines
	out.SummaryCount = countLines(*args.Content)
	return out
}

func hashFileContent(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("expected a regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
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
	args.Scope = strings.TrimSpace(args.Scope)
	resolved, err := r.resolveFilePath(call, args.Scope, args.Path, true, false, true)
	if err != nil {
		return filePathError(out, args.Scope, err)
	}
	if resolved.rel == "." {
		return toolJSONError(out, "refuse_root_delete", "deleting a scope root is not allowed")
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
	args.Scope = strings.TrimSpace(args.Scope)
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
	if err := os.MkdirAll(filepath.Dir(toResolved.target), 0o700); err != nil {
		return toolJSONError(out, "mkdir_failed", err.Error())
	}
	if err := os.Rename(fromResolved.target, toResolved.target); err != nil {
		return toolJSONError(out, "move_failed", err.Error())
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

type fileCopyEndpoint struct {
	Scope string `json:"scope"`
	Path  string `json:"path"`
}

type fileCopyArgs struct {
	From      fileCopyEndpoint `json:"from"`
	To        fileCopyEndpoint `json:"to"`
	Recursive bool             `json:"recursive"`
	Overwrite bool             `json:"overwrite"`
}

func decodeFileCopyArgs(raw json.RawMessage) (fileCopyArgs, error) {
	var args fileCopyArgs
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return args, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return args, errors.New("expected one JSON object")
	}
	for _, endpoint := range []struct {
		name  string
		value *fileCopyEndpoint
	}{{"from", &args.From}, {"to", &args.To}} {
		endpoint.value.Scope = strings.TrimSpace(endpoint.value.Scope)
		endpoint.value.Path = strings.TrimSpace(endpoint.value.Path)
		if endpoint.value.Scope == "" {
			if !filepath.IsAbs(endpoint.value.Path) {
				return args, errors.New(endpoint.name + ".scope is required for a relative path")
			}
			endpoint.value.Scope = managedScopeProject
		}
		switch endpoint.value.Scope {
		case managedScopeTemp, managedScopeSkill, managedScopeProject:
		default:
			return args, &invalidScopeError{Field: endpoint.name + ".scope", Allowed: []string{managedScopeTemp, managedScopeSkill, managedScopeProject}}
		}
		if endpoint.value.Path == "" {
			return args, errors.New(endpoint.name + ".path is required")
		}
	}
	return args, nil
}

func (r *BuiltinRunner) fileCopy(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeFileCopyArgs(call.Args)
	if err != nil {
		var scopeErr *invalidScopeError
		if errors.As(err, &scopeErr) {
			return filePathError(out, "", err)
		}
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	grant := call.FileCopyGrant
	if grant != nil && !grant.consume(call) {
		return toolJSONError(out, "copy_approval_changed", "copy approval does not match this invocation or has already been used")
	}
	plan, err := r.prepareFileCopy(call, args, grant != nil)
	if err != nil {
		return fileCopyFailure(out, err)
	}
	if grant != nil && !grant.matches(plan) {
		return toolJSONError(out, "copy_approval_changed", "source or destination changed while waiting for approval; submit the copy again")
	}
	fromResolved, toResolved, info := plan.from.resolved, plan.to.resolved, plan.sourceInfo
	payload := map[string]any{
		"ok": true, "fromScope": plan.From.Scope, "toScope": plan.To.Scope,
		"from": fromResolved.outputPath(), "to": toResolved.outputPath(),
	}
	if fromResolved.project {
		payload["fromRoot"] = fromResolved.root
		payload["fromRelativePath"] = fromResolved.rel
	}
	if toResolved.project {
		payload["toRoot"] = toResolved.root
		payload["toRelativePath"] = toResolved.rel
	}
	warnings, err := r.applyFileCopy(call, args, plan)
	if err != nil {
		return fileCopyFailure(out, err)
	}
	if len(warnings) > 0 {
		payload["warnings"] = warnings
	}
	if info.IsDir() {
		payload["copied"] = "directory"
	} else {
		payload["copied"] = "file"
		payload["bytes"] = info.Size()
	}
	out.Ok = true
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

var errFileCopyDestinationExists = errors.New("copy destination exists")

// Validate and prepare the parent only. Never delete an existing destination
// before its staged replacement is complete (also used by attachment export).
func prepareFileCopyDestination(dst string, overwrite bool) error {
	if _, err := os.Lstat(dst); err == nil {
		if !overwrite {
			return errFileCopyDestinationExists
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
	scope = strings.TrimSpace(scope)
	if scope == managedScopeWidget {
		return r.resolveWidgetFilePath(call, rawPath, requireWritable, allowRoot, allowMissing)
	}
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
	var scopeErr *invalidScopeError
	if errors.As(err, &scopeErr) {
		return toolJSON(out, false, map[string]any{"ok": false, "reason": "invalid_scope", "detail": err.Error(), "field": scopeErr.Field, "allowedScopes": scopeErr.Allowed})
	}
	if !isProjectFileScope(scope) {
		if errors.Is(err, os.ErrNotExist) {
			return toolJSONError(out, "path_not_found", err.Error())
		}
		return toolJSONError(out, fallbackReason, err.Error())
	}
	payload := projectPathErrorPayload(err)
	out = toolJSON(out, false, payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func projectPathErrorPayload(err error) map[string]any {
	reason := projectPathErrorReason(err)
	hint := "Inspect the filesystem error for the selected path."
	switch reason {
	case "path_not_authorized":
		hint = "The path is outside authorized project directories. Request access only if this is the intended target."
	case "project_dirs_required":
		hint = "No project directories are authorized. Use request_capability with targetMode=code and projectDirs, then ask the user to approve it for this turn if temporary access is enough."
	case "absolute_path_required":
		hint = "Select one authorized project root and pass an absolute path or cwd. file_list with scope=project and path=. lists the roots."
	case "path_not_found":
		hint = "The selected authorized path does not exist. Check the path or use file_stat to test existence; additional project access is not required."
	case "path_not_allowed":
		hint = "A file path is required for this tool."
	}
	payload := map[string]any{"ok": false, "reason": reason, "detail": err.Error(), "hint": hint}
	if roots := projectPathErrorRoots(err); len(roots) > 0 {
		payload["projectRoots"] = roots
	}
	return payload
}

func isProjectFileScope(scope string) bool {
	return strings.TrimSpace(scope) == managedScopeProject
}

func (r *BuiltinRunner) managedRoot(scope string) (string, bool, error) {
	scope = strings.TrimSpace(scope)
	if scope != managedScopePlugin && scope != managedScopeSkill && scope != managedScopeTemp {
		return "", false, &invalidScopeError{Field: "scope", Allowed: []string{managedScopePlugin, managedScopeSkill, managedScopeTemp, managedScopeProject}}
	}
	if strings.TrimSpace(r.homeDir) == "" {
		return "", false, errors.New("home directory is not configured")
	}
	switch scope {
	case managedScopePlugin:
		return home.PluginsPath(r.homeDir), false, nil
	case managedScopeSkill:
		return home.SkillsPath(r.homeDir), true, nil
	case managedScopeTemp:
		return home.TempPath(r.homeDir), true, nil
	default:
		return "", false, &invalidScopeError{Field: "scope", Allowed: []string{managedScopePlugin, managedScopeSkill, managedScopeTemp, managedScopeProject}}
	}
}

func (r *BuiltinRunner) resolveManagedPath(scope, rawPath string, requireWritable, allowRoot bool) (string, string, string, error) {
	scope = strings.TrimSpace(scope)
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
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return "", "", "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return "", "", "", errors.New("managed scope root must be a directory, not a symlink")
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
	if err := validateManagedRelativePath(scope, cleaned, requireWritable); err != nil {
		return "", "", "", err
	}
	_, target, rel, err := resolveProjectPath([]string{root}, cleaned, allowRoot, true)
	if err != nil {
		return "", "", "", err
	}
	// The lexical name may be a visible symlink into a reserved directory.
	// Apply the same policy to the canonical target, including missing suffixes.
	if err := validateManagedRelativePath(scope, rel, requireWritable); err != nil {
		return "", "", "", err
	}
	return root, target, rel, nil
}

func validateManagedRelativePath(scope, path string, requireWritable bool) error {
	if hasHiddenPathComponent(path) {
		return errors.New("path is reserved")
	}
	if requireWritable && scope == managedScopeSkill && skill.IsBuiltinID(firstPathComponent(path)) {
		return errors.New("builtin Skill paths are read-only")
	}
	return nil
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
	baseRoot       string
	matcher        fileSearchMatcher
	includeGlobs   []string
	excludeGlobs   []string
	contextLines   int
	skipHidden     bool
	skipped        map[string]int
	filesOnly      bool
	includeIgnored bool
	page           *filePage
}

// TextFileSearchOptions configures the shared UTF-8 content search used by
// both model tools and the project browser.
type TextFileSearchOptions struct {
	BaseRoot       string
	CaseSensitive  bool
	ContextLines   int
	ExcludeGlobs   []string
	IncludeGlobs   []string
	MaxResults     int
	Mode           string
	Query          string
	SkipHidden     bool
	OutputMode     string
	IncludeIgnored bool
	Cursor         string
}

type TextFileSearchMatch struct {
	Path      string
	Line      int
	LineStart int
	LineEnd   int
	Text      string
	Excerpt   string
	Truncated bool
}

type TextFileSearchResult struct {
	CaseSensitive  bool
	ContextLines   int
	ExcludeGlobs   []string
	FilesScanned   int
	IncludeGlobs   []string
	Matches        []TextFileSearchMatch
	Mode           string
	Query          string
	ResultsCapped  bool
	Complete       bool
	Skipped        map[string]int
	OutputMode     string
	IncludeIgnored bool
	NextCursor     string
}

type TextFileSearchError struct {
	Code   string
	Detail string
}

func (e *TextFileSearchError) Error() string {
	return e.Detail
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

// SearchTextFiles performs the canonical bounded text search for authorized
// file roots. Callers remain responsible for authorizing root before calling.
func SearchTextFiles(ctx context.Context, root string, options TextFileSearchOptions) (TextFileSearchResult, error) {
	requestedRoot := root
	if options.OutputMode == "" {
		options.OutputMode = "content"
	}
	if options.OutputMode != "content" && options.OutputMode != "files" {
		return TextFileSearchResult{}, &TextFileSearchError{Code: "invalid_output_mode", Detail: "output_mode must be content or files"}
	}
	options.Query = strings.TrimSpace(options.Query)
	if options.Query == "" {
		return TextFileSearchResult{}, &TextFileSearchError{Code: "query_required", Detail: "query must be a non-empty string"}
	}
	mode := strings.ToLower(strings.TrimSpace(options.Mode))
	if mode == "" {
		mode = "literal"
	}
	if mode != "literal" && mode != "regex" {
		return TextFileSearchResult{}, &TextFileSearchError{Code: "invalid_search_mode", Detail: "mode must be literal or regex"}
	}
	if options.ContextLines < 0 || options.ContextLines > maxFileSearchContextLines {
		return TextFileSearchResult{}, &TextFileSearchError{
			Code:   "invalid_context_lines",
			Detail: "context_lines must be between 0 and " + strconv.Itoa(maxFileSearchContextLines) + ". Retry with context_lines=0 to locate matching line numbers, then use builtin_file_slice for a larger range.",
		}
	}
	matcher, err := newFileSearchMatcher(mode, options.Query, options.CaseSensitive)
	if err != nil {
		return TextFileSearchResult{}, &TextFileSearchError{Code: "invalid_regex", Detail: err.Error()}
	}
	if err := validateSearchGlobs(options.IncludeGlobs, options.ExcludeGlobs); err != nil {
		return TextFileSearchResult{}, &TextFileSearchError{Code: "invalid_glob", Detail: err.Error()}
	}
	maxResults := options.MaxResults
	if maxResults <= 0 {
		maxResults = defaultFileSearchMax
	}
	if maxResults > maxFileSearchMax {
		maxResults = maxFileSearchMax
	}
	baseRoot := options.BaseRoot
	if strings.TrimSpace(baseRoot) == "" {
		baseRoot = root
		if info, err := os.Lstat(root); err == nil && info.Mode().IsRegular() {
			baseRoot = filepath.Dir(root)
		}
	}
	if evaluated, evaluateErr := filepath.EvalSymlinks(baseRoot); evaluateErr == nil {
		info, statErr := os.Lstat(baseRoot)
		// Replace only a directory prefix; resolving a symlink search target
		// here would bypass searchTextFiles' no-symlink check.
		if rel, err := filepath.Rel(baseRoot, root); err == nil && statErr == nil && info.IsDir() && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			root = filepath.Join(evaluated, rel)
		}
		baseRoot = evaluated
	}
	queryOptions := options
	queryOptions.BaseRoot, queryOptions.Mode = baseRoot, mode
	queryOptions.Cursor, queryOptions.MaxResults = "", 0
	page, err := newFilePage(root, options.Cursor, []any{"search", root, queryOptions})
	if err != nil {
		return TextFileSearchResult{}, err
	}
	skipped := make(map[string]int)
	matches, filesScanned, capped, err := searchTextFiles(ctx, root, fileSearchOptions{
		baseRoot:       baseRoot,
		matcher:        matcher,
		includeGlobs:   options.IncludeGlobs,
		excludeGlobs:   options.ExcludeGlobs,
		contextLines:   options.ContextLines,
		skipHidden:     options.SkipHidden,
		skipped:        skipped,
		filesOnly:      options.OutputMode == "files",
		includeIgnored: options.IncludeIgnored,
		page:           page,
	}, maxResults)
	if err != nil {
		return TextFileSearchResult{}, err
	}
	result := TextFileSearchResult{
		CaseSensitive:  options.CaseSensitive,
		ContextLines:   options.ContextLines,
		ExcludeGlobs:   options.ExcludeGlobs,
		FilesScanned:   filesScanned,
		IncludeGlobs:   options.IncludeGlobs,
		Matches:        make([]TextFileSearchMatch, 0, len(matches)),
		Mode:           mode,
		Query:          options.Query,
		ResultsCapped:  capped,
		Complete:       !capped && len(skipped) == 0,
		Skipped:        skipped,
		OutputMode:     options.OutputMode,
		IncludeIgnored: options.IncludeIgnored,
	}
	if capped {
		result.NextCursor = page.next()
	}
	for _, match := range matches {
		// Keep the caller's path representation (e.g. /var vs /private/var),
		// while traversal and ignore matching use the canonical root.
		if root != requestedRoot {
			rel, err := filepath.Rel(root, match.path)
			if err != nil {
				return TextFileSearchResult{}, err
			}
			match.path = filepath.Join(requestedRoot, rel)
		}
		result.Matches = append(result.Matches, TextFileSearchMatch{
			Path: match.path, Line: match.line, LineStart: match.lineStart, LineEnd: match.lineEnd,
			Text: match.text, Excerpt: match.excerpt, Truncated: match.truncated,
		})
	}
	return result, nil
}

func searchTextFiles(ctx context.Context, root string, options fileSearchOptions, maxResults int) ([]fileSearchMatch, int, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, false, err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return nil, 0, false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, 0, false, errors.New("symlink search is not supported")
	}
	if !info.IsDir() {
		if options.page.skip(root, false) {
			return nil, 0, false, nil
		}
		if !searchPathAllowed(root, options) {
			return nil, 0, false, nil
		}
		if info.Size() > maxFileSearchDirectBytes {
			return nil, 0, false, &TextFileSearchError{Code: "file_too_large", Detail: "Direct file searches support files up to 16 MiB. Use file_slice for bounded reads or narrow the source output."}
		}
		matches, scanned, err := searchTextFile(ctx, root, options, maxResults+1)
		capped := len(matches) > maxResults
		if capped {
			matches = matches[:maxResults]
			options.page.last = options.page.position(root, matches[len(matches)-1].line)
		}
		return matches, scanned, capped, err
	}
	var matches []fileSearchMatch
	filesScanned := 0
	capped := false
	filter, err := newFileDiscoveryFilter(options.baseRoot, options.includeIgnored, options.skipHidden)
	if err != nil {
		return nil, 0, false, err
	}
	defer filter.close()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			options.skipped["read_error"]++
			return nil
		}
		if path == root {
			return nil
		}
		if options.page.skip(path, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !options.page.visit(path) {
			capped = true
			return filepath.SkipAll
		}
		allowed, err := filter.accept(path, entry)
		if err != nil {
			return err
		}
		if !allowed {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
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
		if err != nil {
			options.skipped["read_error"]++
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			options.skipped["symlink"]++
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if isKnownBinaryExt(path) {
			return nil
		}
		if info.Size() > maxFileSearchBytes {
			options.skipped["file_too_large"]++
			return nil
		}
		fileMatches, scanned, err := searchTextFile(ctx, path, options, maxResults+1-len(matches))
		filesScanned += scanned
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			options.skipped["read_error"]++
			return nil
		}
		matches = append(matches, fileMatches...)
		if len(matches) > maxResults {
			matches = matches[:maxResults]
			last := matches[len(matches)-1]
			options.page.last = options.page.position(last.path, last.line)
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

func searchTextFile(ctx context.Context, filePath string, options fileSearchOptions, remaining int) ([]fileSearchMatch, int, error) {
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
	if options.filesOnly {
		for scanner.Scan() {
			if err := ctx.Err(); err != nil {
				return nil, 1, err
			}
			if options.matcher.matches(scanner.Text()) {
				return []fileSearchMatch{{path: filePath}}, 1, nil
			}
		}
		return nil, 1, scanner.Err()
	}
	lines := make([]string, 0, 256)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, 1, err
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, 1, err
	}
	matches := make([]fileSearchMatch, 0)
	for index, line := range lines {
		if options.page != nil && index+1 <= options.page.startLine(filePath) {
			continue
		}
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

func readLineRange(path string, start, end int) ([]fileLineRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	return out, nil
}

func readLineRangeFromEnd(path string, lines, skip int) ([]fileLineRecord, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	end := len(window) - skip
	if end < 0 {
		end = 0
	}
	start := end - lines
	if start < 0 {
		start = 0
	}
	return window[start:end], nil
}

func renderLineSlice(lines []fileLineRecord, maxBytes int) (string, string, int) {
	var content strings.Builder
	var numbered strings.Builder
	renderedLines := 0
	for i, line := range lines {
		next := line.text
		prefix := ""
		if i > 0 {
			prefix = "\n"
		}
		numberedLine := prefix + strconv.Itoa(line.number) + ": " + line.text
		contentLine := prefix + next
		if content.Len()+len(contentLine) > maxBytes || numbered.Len()+len(numberedLine) > maxBytes {
			break
		}
		content.WriteString(contentLine)
		numbered.WriteString(numberedLine)
		renderedLines++
	}
	return content.String(), numbered.String(), renderedLines
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
	if isKnownTextExt(path) {
		return false, mt, nil
	}
	main := strings.ToLower(strings.TrimSpace(strings.Split(mt, ";")[0]))
	switch {
	case strings.HasPrefix(main, "text/"),
		main == "application/json",
		main == "application/xml",
		main == "application/javascript",
		main == "application/x-yaml",
		main == "image/svg+xml":
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

func isKnownBinaryExt(path string) bool {
	_, ok := binaryFileExts[strings.ToLower(filepath.Ext(path))]
	return ok
}

func isKnownTextExt(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cts", ".mts", ".ts", ".tsx":
		return true
	default:
		return false
	}
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

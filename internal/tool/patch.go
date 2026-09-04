package tool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	formatdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/sergi/go-diff/diffmatchpatch"
)

const (
	patchMaxFiles         = 16
	patchMaxHunksPerFile  = 64
	patchMaxFileBytes     = 512 << 10
	patchMaxTotalBytes    = 2 << 20
	patchMaxDiffBytes     = 256 << 10
	patchPreparedTTL      = 2 * time.Hour
	patchMaxPreparedItems = 128
)

type filePatchArgs struct {
	Scope string         `json:"scope"`
	Files []patchFileArg `json:"files"`
}

type patchFileArg struct {
	Path    string         `json:"path"`
	Action  string         `json:"action"`
	Content *string        `json:"content,omitempty"`
	Hunks   []patchHunkArg `json:"hunks,omitempty"`
}

type patchHunkArg struct {
	StartLine int       `json:"start_line"`
	OldLines  *[]string `json:"old_lines"`
	NewLines  *[]string `json:"new_lines"`
}

type preparedPatch struct {
	SessionID   string
	CallID      string
	ArgsHash    string
	ProjectRoot string
	Files       []preparedPatchFile
	Diff        string
	Additions   int
	Deletions   int
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

type preparedPatchFile struct {
	Path      string
	Target    string
	Operation string
	Existed   bool
	Delete    bool
	OldText   string
	NewText   string
	OldHash   string
	Mode      os.FileMode
	Additions int
	Deletions int
}

type patchFileView struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type patchError struct {
	reason string
	detail string
}

type patchLimitError struct {
	reason string
	detail string
	count  int
	limit  int
}

type patchArgumentError struct {
	kind     string
	detail   string
	hint     string
	field    string
	expected string
	offset   int64
}

func (e *patchArgumentError) Error() string { return e.detail }

func (e *patchError) Error() string {
	return e.detail
}

func (e *patchLimitError) Error() string {
	return e.detail
}

func newPatchError(reason, detail string) error {
	return &patchError{reason: reason, detail: detail}
}

func decodeFilePatchArgs(raw json.RawMessage) (filePatchArgs, *patchArgumentError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return filePatchArgs{}, &patchArgumentError{
			kind:   "missing_arguments",
			detail: "patch arguments are empty",
			hint:   "Pass one JSON object with scope and files fields.",
		}
	}
	if trimmed[0] != '{' {
		var value any
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return filePatchArgs{}, patchJSONArgumentError(err)
		}
		hint := "Pass one JSON object with scope and files fields."
		if trimmed[0] == '"' {
			hint = "Pass the object directly instead of a JSON-encoded string."
		}
		return filePatchArgs{}, &patchArgumentError{
			kind:     "expected_object",
			detail:   "patch arguments must be a JSON object",
			hint:     hint,
			expected: "object",
		}
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return filePatchArgs{}, patchJSONArgumentError(err)
	}
	for _, field := range []string{"scope", "files"} {
		if _, ok := fields[field]; !ok {
			return filePatchArgs{}, &patchArgumentError{
				kind:     "missing_field",
				detail:   "required field is missing: " + field,
				hint:     "Add the required " + field + " field and retry.",
				field:    field,
				expected: patchArgumentExpectedType(field),
			}
		}
	}

	var args filePatchArgs
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&args); err != nil {
		return filePatchArgs{}, patchJSONArgumentError(err)
	}
	return args, nil
}

func patchJSONArgumentError(err error) *patchArgumentError {
	if strings.HasPrefix(err.Error(), "json: unknown field ") {
		return &patchArgumentError{
			kind:   "unknown_field",
			detail: err.Error(),
			hint:   "Remove the unsupported field and use the current patch schema.",
		}
	}
	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		kind := "invalid_json"
		hint := "Correct the JSON syntax near the reported byte offset and retry."
		if strings.Contains(strings.ToLower(syntaxErr.Error()), "unexpected end") {
			kind = "truncated_json"
			hint = "The arguments appear truncated; resend the complete JSON object, or split a large change into smaller logical batches."
		}
		return &patchArgumentError{
			kind:   kind,
			detail: syntaxErr.Error(),
			hint:   hint,
			offset: syntaxErr.Offset,
		}
	}
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		return &patchArgumentError{
			kind:     "invalid_type",
			detail:   typeErr.Error(),
			hint:     "Use the JSON type required by the tool schema and retry.",
			field:    field,
			expected: patchArgumentExpectedType(field),
			offset:   typeErr.Offset,
		}
	}
	return &patchArgumentError{
		kind:   "invalid_json",
		detail: err.Error(),
		hint:   "Pass one valid JSON object matching the patch schema.",
	}
}

func patchArgumentExpectedType(field string) string {
	switch {
	case field == "scope", strings.HasSuffix(field, ".path"), strings.HasSuffix(field, ".action"), strings.HasSuffix(field, ".content"):
		return "string"
	case field == "files", strings.HasSuffix(field, ".hunks"), strings.HasSuffix(field, ".old_lines"), strings.HasSuffix(field, ".new_lines"):
		return "array"
	case strings.HasSuffix(field, ".start_line"):
		return "integer"
	default:
		return "schema-compatible value"
	}
}

func patchArgumentFailure(out Result, argumentErr *patchArgumentError) Result {
	payload := map[string]any{
		"ok":        false,
		"reason":    "invalid_arguments",
		"errorKind": argumentErr.kind,
		"detail":    argumentErr.detail,
	}
	if argumentErr.hint != "" {
		payload["hint"] = argumentErr.hint
	}
	if argumentErr.field != "" {
		payload["field"] = argumentErr.field
	}
	if argumentErr.expected != "" {
		payload["expected"] = argumentErr.expected
	}
	if argumentErr.offset > 0 {
		payload["offset"] = argumentErr.offset
	}
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func validateFilePatchArgs(args filePatchArgs) *patchArgumentError {
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return &patchArgumentError{
			kind:     "invalid_scope",
			detail:   "scope must be project",
			hint:     "Set scope to project and retry.",
			field:    "scope",
			expected: "project",
		}
	}
	if len(args.Files) == 0 {
		return &patchArgumentError{
			kind:     "empty_files",
			detail:   "files must contain at least one entry",
			hint:     "Add at least one file change and retry.",
			field:    "files",
			expected: "non-empty array",
		}
	}
	for index, file := range args.Files {
		fieldPrefix := "files[" + strconv.Itoa(index) + "]"
		if strings.TrimSpace(file.Path) == "" {
			return &patchArgumentError{kind: "path_required", detail: "patch file path is required", hint: "Set a project file path and retry.", field: fieldPrefix + ".path", expected: "non-empty string"}
		}
		action := strings.TrimSpace(file.Action)
		switch action {
		case "create", "replace":
			if file.Content == nil || len(file.Hunks) > 0 {
				return &patchArgumentError{kind: "invalid_file_operation", detail: "action=" + action + " requires content and does not accept hunks", hint: "Provide content only for this file action.", field: fieldPrefix, expected: "content without hunks"}
			}
		case "edit":
			if file.Content != nil || len(file.Hunks) == 0 {
				return &patchArgumentError{kind: "invalid_file_operation", detail: "action=edit requires hunks and does not accept content", hint: "Provide one or more hunks and omit content.", field: fieldPrefix, expected: "non-empty hunks without content"}
			}
			for hunkIndex, hunk := range file.Hunks {
				hunkField := fieldPrefix + ".hunks[" + strconv.Itoa(hunkIndex) + "]"
				if hunk.StartLine < 1 {
					return &patchArgumentError{kind: "hunk_line_invalid", detail: "hunk start_line must be at least 1", hint: "Use a one-based line number from the original file.", field: hunkField + ".start_line", expected: "integer >= 1"}
				}
				if hunk.NewLines == nil {
					return &patchArgumentError{kind: "hunk_new_lines_required", detail: "hunk new_lines is required", hint: "Provide replacement lines, or an empty array to delete old_lines.", field: hunkField + ".new_lines", expected: "array"}
				}
				if hunk.OldLines == nil {
					return &patchArgumentError{kind: "hunk_old_lines_required", detail: "hunk old_lines is required", hint: "Provide exact original lines, or an empty array to insert new_lines.", field: hunkField + ".old_lines", expected: "array"}
				}
				if len(*hunk.OldLines) == 0 && len(*hunk.NewLines) == 0 {
					return &patchArgumentError{kind: "empty_hunk", detail: "hunk old_lines and new_lines cannot both be empty", hint: "Provide old lines to remove or new lines to insert.", field: hunkField, expected: "a non-empty old_lines or new_lines array"}
				}
				for _, line := range append(append([]string(nil), (*hunk.OldLines)...), (*hunk.NewLines)...) {
					if strings.ContainsAny(line, "\r\n") {
						return &patchArgumentError{kind: "hunk_line_contains_newline", detail: "hunk line entries must not contain newline characters", hint: "Put each logical line in a separate array element.", field: hunkField, expected: "line arrays without newline characters"}
					}
					if !isToolText([]byte(line)) {
						return &patchArgumentError{kind: "binary_file", detail: "hunk lines must be UTF-8 text without NUL bytes", hint: "Use text line values only.", field: hunkField, expected: "UTF-8 text lines"}
					}
				}
			}
		case "delete":
			if file.Content != nil || len(file.Hunks) > 0 {
				return &patchArgumentError{kind: "invalid_file_operation", detail: "action=delete does not accept content or hunks", hint: "Remove content and hunks from this file entry.", field: fieldPrefix, expected: "path and action only"}
			}
		default:
			return &patchArgumentError{kind: "invalid_action", detail: "patch action must be create, replace, edit, or delete", hint: "Set a supported action and retry.", field: fieldPrefix + ".action", expected: "create, replace, edit, or delete"}
		}
	}
	return nil
}

func preparePatch(call Call, args filePatchArgs) (*preparedPatch, error) {
	if argumentErr := validateFilePatchArgs(args); argumentErr != nil {
		return nil, argumentErr
	}
	if strings.TrimSpace(call.SessionID) == "" {
		return nil, newPatchError("session_required", "session id is required for project patches")
	}
	if len(args.Files) > patchMaxFiles {
		return nil, &patchLimitError{
			reason: "too_many_files",
			detail: "patches support at most 16 files; split the change into smaller batches",
			count:  len(args.Files),
			limit:  patchMaxFiles,
		}
	}

	patch := &preparedPatch{
		SessionID: call.SessionID,
		CallID:    call.CallID,
		ArgsHash:  patchContentHash(bytes.TrimSpace(call.Args)),
		CreatedAt: time.Now(),
	}
	patch.ExpiresAt = patch.CreatedAt.Add(patchPreparedTTL)
	seen := make(map[string]bool, len(args.Files))
	totalBytes := 0
	var diffs strings.Builder
	for _, requested := range args.Files {
		file, root, err := preparePatchFile(call.ProjectDirs, requested)
		if err != nil {
			return nil, err
		}
		if patch.ProjectRoot == "" {
			patch.ProjectRoot = root
		} else if filepath.Clean(patch.ProjectRoot) != filepath.Clean(root) {
			return nil, newPatchError("cross_root_patch", "all patch files must be inside the same authorized project root")
		}
		if seen[file.Target] {
			return nil, newPatchError("duplicate_path", "patch contains the same file more than once: "+file.Path)
		}
		seen[file.Target] = true
		if file.Existed && !file.Delete && file.OldText == file.NewText {
			continue
		}
		totalBytes += len(file.OldText) + len(file.NewText)
		if totalBytes > patchMaxTotalBytes {
			return nil, newPatchError("patch_too_large", "patch source and destination text exceeds 2 MiB")
		}
		fileDiff, additions, deletions, err := buildUnifiedFileDiff(file)
		if err != nil {
			return nil, newPatchError("diff_failed", err.Error())
		}
		file.Additions = additions
		file.Deletions = deletions
		diffs.WriteString(fileDiff)
		if diffs.Len() > patchMaxDiffBytes {
			return nil, newPatchError("patch_diff_too_large", "review diff exceeds 256 KiB; split the change into smaller batches")
		}
		patch.Files = append(patch.Files, file)
		patch.Additions += additions
		patch.Deletions += deletions
	}
	if len(patch.Files) == 0 {
		return nil, newPatchError("no_changes", "patch does not change any files")
	}
	patch.Diff = diffs.String()
	return patch, nil
}

func preparePatchFile(projectDirs []string, requested patchFileArg) (preparedPatchFile, string, error) {
	path := strings.TrimSpace(requested.Path)
	if path == "" {
		return preparedPatchFile{}, "", newPatchError("path_required", "patch file path is required")
	}
	action := strings.TrimSpace(requested.Action)
	if len(requested.Hunks) > patchMaxHunksPerFile {
		return preparedPatchFile{}, "", newPatchError("too_many_hunks", "patch files support at most 64 hunks: "+path)
	}
	root, target, rel, err := resolveProjectPath(projectDirs, path, false, true)
	if err != nil {
		return preparedPatchFile{}, "", &patchError{reason: patchPathReason(err), detail: err.Error()}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return preparedPatchFile{}, "", newPatchError("project_root_unavailable", err.Error())
	}
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		target = filepath.Join(resolvedRoot, filepath.FromSlash(rel))
	}
	file := preparedPatchFile{Path: filepath.ToSlash(rel), Target: target, Delete: action == "delete", NewText: "", Mode: 0o600}
	info, statErr := os.Lstat(target)
	switch {
	case statErr == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return preparedPatchFile{}, "", newPatchError("symlink_unsupported", "patches do not support symlink files: "+file.Path)
		}
		if !info.Mode().IsRegular() {
			return preparedPatchFile{}, "", newPatchError("regular_file_required", "patch path must be a regular file: "+file.Path)
		}
		if info.Size() > patchMaxFileBytes {
			return preparedPatchFile{}, "", newPatchError("file_too_large", "patch files must not exceed 512 KiB: "+file.Path)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return preparedPatchFile{}, "", newPatchError("read_failed", err.Error())
		}
		if !isToolText(data) {
			return preparedPatchFile{}, "", newPatchError("binary_file", "patches support UTF-8 text files only: "+file.Path)
		}
		file.Existed = true
		file.OldText = string(data)
		file.OldHash = patchContentHash(data)
		file.Mode = info.Mode().Perm()
		if action == "create" {
			return preparedPatchFile{}, "", newPatchError("file_exists", "cannot create an existing file: "+file.Path)
		}
	case errors.Is(statErr, os.ErrNotExist):
		if action == "delete" {
			return preparedPatchFile{}, "", newPatchError("file_not_found", "cannot delete a missing file: "+file.Path)
		}
		if action != "create" {
			return preparedPatchFile{}, "", newPatchError("file_not_found", "action="+action+" requires an existing file: "+file.Path)
		}
		file.OldHash = patchContentHash(nil)
	default:
		return preparedPatchFile{}, "", newPatchError("stat_failed", statErr.Error())
	}
	if action == "delete" {
		file.Operation = "delete"
	} else if action == "edit" {
		next, err := applyPatchHunks(file.OldText, file.Path, requested.Hunks)
		if err != nil {
			return preparedPatchFile{}, "", err
		}
		file.NewText = next
		file.Operation = "update"
	} else {
		file.NewText = *requested.Content
		if len(file.NewText) > patchMaxFileBytes {
			return preparedPatchFile{}, "", newPatchError("file_too_large", "patched file text must not exceed 512 KiB: "+file.Path)
		}
		if !isToolText([]byte(file.NewText)) {
			return preparedPatchFile{}, "", newPatchError("binary_file", "patched file content must be UTF-8 text without NUL bytes: "+file.Path)
		}
		if action == "replace" {
			file.Operation = "update"
		} else {
			file.Operation = "create"
		}
	}
	return file, resolvedRoot, nil
}

type patchTextLine struct {
	text   string
	ending string
}

func applyPatchHunks(content, filePath string, hunks []patchHunkArg) (string, error) {
	if len(hunks) == 0 || len(hunks) > patchMaxHunksPerFile {
		return "", newPatchError("invalid_hunks", "patch hunks must contain between 1 and 64 entries: "+filePath)
	}
	type resolvedHunk struct {
		index    int
		start    int
		end      int
		newLines []string
	}
	lines := splitPatchTextLines(content)
	resolved := make([]resolvedHunk, 0, len(hunks))
	for index, hunk := range hunks {
		oldLines := *hunk.OldLines
		start := hunk.StartLine - 1
		if start > len(lines) || len(oldLines) > len(lines)-start {
			return "", newPatchError("hunk_line_out_of_range", "hunk "+strconv.Itoa(index+1)+" range is outside the original file: "+filePath)
		}
		end := start + len(oldLines)
		for offset, expected := range oldLines {
			if lines[start+offset].text != expected {
				return "", newPatchError("hunk_lines_mismatch", "hunk "+strconv.Itoa(index+1)+" old_lines do not match at start_line "+strconv.Itoa(hunk.StartLine)+": "+filePath)
			}
		}
		resolved = append(resolved, resolvedHunk{index: index, start: start, end: end, newLines: *hunk.NewLines})
	}
	sort.Slice(resolved, func(i, j int) bool {
		if resolved[i].start == resolved[j].start {
			return resolved[i].end < resolved[j].end
		}
		return resolved[i].start < resolved[j].start
	})
	for index := 1; index < len(resolved); index++ {
		previous := resolved[index-1]
		current := resolved[index]
		if current.start < previous.end || (current.start == previous.start && (current.start == current.end || previous.start == previous.end)) {
			return "", newPatchError("hunk_overlap", "hunks "+strconv.Itoa(resolved[index-1].index+1)+" and "+strconv.Itoa(resolved[index].index+1)+" overlap in the original file: "+filePath)
		}
	}
	defaultEnding := patchDefaultLineEnding(lines)
	for index := len(resolved) - 1; index >= 0; index-- {
		hunk := resolved[index]
		replacement := make([]patchTextLine, len(hunk.newLines))
		terminalEnding := defaultEnding
		if hunk.end > hunk.start {
			terminalEnding = lines[hunk.end-1].ending
		} else if hunk.start == len(lines) {
			terminalEnding = ""
			if len(lines) > 0 {
				last := len(lines) - 1
				if lines[last].ending == "" {
					lines[last].ending = defaultEnding
				} else {
					terminalEnding = lines[last].ending
				}
			}
		}
		for lineIndex, line := range hunk.newLines {
			ending := defaultEnding
			if lineIndex == len(hunk.newLines)-1 {
				ending = terminalEnding
			}
			replacement[lineIndex] = patchTextLine{text: line, ending: ending}
		}
		next := make([]patchTextLine, 0, len(lines)-(hunk.end-hunk.start)+len(replacement))
		next = append(next, lines[:hunk.start]...)
		next = append(next, replacement...)
		next = append(next, lines[hunk.end:]...)
		lines = next
	}
	var next strings.Builder
	for _, line := range lines {
		next.WriteString(line.text)
		next.WriteString(line.ending)
		if next.Len() > patchMaxFileBytes {
			return "", newPatchError("file_too_large", "edited file text must not exceed 512 KiB: "+filePath)
		}
	}
	return next.String(), nil
}

func splitPatchTextLines(content string) []patchTextLine {
	if content == "" {
		return nil
	}
	lines := make([]patchTextLine, 0, strings.Count(content, "\n")+1)
	for len(content) > 0 {
		newline := strings.IndexByte(content, '\n')
		if newline < 0 {
			lines = append(lines, patchTextLine{text: content})
			break
		}
		text := content[:newline]
		ending := "\n"
		if strings.HasSuffix(text, "\r") {
			text = strings.TrimSuffix(text, "\r")
			ending = "\r\n"
		}
		lines = append(lines, patchTextLine{text: text, ending: ending})
		content = content[newline+1:]
	}
	return lines
}

func patchDefaultLineEnding(lines []patchTextLine) string {
	for _, line := range lines {
		if line.ending != "" {
			return line.ending
		}
	}
	return "\n"
}

func (r *BuiltinRunner) filePatch(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, argumentErr := decodeFilePatchArgs(call.Args)
	if argumentErr != nil {
		return patchArgumentFailure(out, argumentErr)
	}
	if argumentErr := validateFilePatchArgs(args); argumentErr != nil {
		return patchArgumentFailure(out, argumentErr)
	}
	if len(args.Files) > patchMaxFiles {
		return patchLimitFailure(out, "too_many_files", "patches support at most 16 files; split the change into smaller batches", len(args.Files), patchMaxFiles)
	}
	patch, err := r.takePreparedPatch(call)
	if err != nil {
		return patchFailure(out, err)
	}
	return applyPreparedPatchResult(out, call.ProjectDirs, patch)
}

func applyPreparedPatchResult(out Result, projectDirs []string, patch *preparedPatch) Result {
	warnings, err := applyPreparedPatch(projectDirs, patch)
	if err != nil {
		return patchFailure(out, err)
	}
	payload := patchPayload(patch)
	payload["ok"] = true
	payload["status"] = "applied"
	payload["warnings"] = warnings
	delete(payload, "diff")
	return withResultSummary(toolJSON(out, true, payload), SummaryChangedLines, patch.Additions+patch.Deletions)
}

func patchLimitFailure(out Result, reason, detail string, count, limit int) Result {
	payload := map[string]any{
		"ok":     false,
		"reason": reason,
		"detail": detail,
		"count":  count,
		"limit":  limit,
		"hint":   "Split the change into smaller logical batches and retry.",
	}
	out.Content = jsonString(payload)
	out.SummaryKind = SummaryReturnedFields
	out.SummaryCount = len(payload)
	return out
}

func (r *BuiltinRunner) ApprovalDetails(ctx context.Context, call Call) (map[string]any, error) {
	switch call.Name {
	case CommandRun:
		return commandApprovalDetails(call)
	case GitStage, GitUnstage, GitCommit:
		return r.gitWriteApprovalDetails(ctx, call)
	case FilePatch:
		// Continue below and prepare the exact filesystem snapshot shown for approval.
	case ComputerUseApp:
		return computerUseAppApprovalDetails(call)
	case ComputerQuitApp:
		return computerQuitAppApprovalDetails(r.computer, call)
	case ComputerObserve:
		return computerObserveApprovalDetails(call)
	case ComputerAct:
		return computerActApprovalDetails(call)
	default:
		return nil, newPatchError("approval_details_unsupported", "approval details are not available for this tool")
	}
	args, argumentErr := decodeFilePatchArgs(call.Args)
	if argumentErr != nil {
		return nil, argumentErr
	}
	if argumentErr := validateFilePatchArgs(args); argumentErr != nil {
		return nil, argumentErr
	}
	patch, err := preparePatch(call, args)
	if err != nil {
		return nil, err
	}
	r.storePreparedPatch(call, patch)
	payload := patchPayload(patch)
	paths := make([]string, 0, len(patch.Files))
	for _, file := range patch.Files {
		paths = append(paths, file.Path)
	}
	payload["paths"] = paths
	return payload, nil
}

func preparedPatchKey(call Call) string {
	sessionID := strings.TrimSpace(call.SessionID)
	callID := strings.TrimSpace(call.CallID)
	if sessionID == "" || callID == "" {
		return ""
	}
	return sessionID + "\x00" + callID
}

func (r *BuiltinRunner) storePreparedPatch(call Call, patch *preparedPatch) {
	key := preparedPatchKey(call)
	if key == "" || patch == nil {
		return
	}
	r.patchMu.Lock()
	defer r.patchMu.Unlock()
	r.cleanupPreparedPatchesLocked(time.Now())
	if len(r.preparedPatches) >= patchMaxPreparedItems {
		var oldestKey string
		var oldest time.Time
		for candidateKey, candidate := range r.preparedPatches {
			if oldestKey == "" || candidate.CreatedAt.Before(oldest) {
				oldestKey = candidateKey
				oldest = candidate.CreatedAt
			}
		}
		delete(r.preparedPatches, oldestKey)
	}
	r.preparedPatches[key] = patch
}

func (r *BuiltinRunner) takePreparedPatch(call Call) (*preparedPatch, error) {
	key := preparedPatchKey(call)
	if key == "" {
		return nil, newPatchError("patch_not_prepared", "patch approval details are unavailable; prepare and approve the patch again")
	}
	r.patchMu.Lock()
	defer r.patchMu.Unlock()
	now := time.Now()
	patch := r.preparedPatches[key]
	delete(r.preparedPatches, key)
	if patch == nil {
		r.cleanupPreparedPatchesLocked(now)
		return nil, newPatchError("patch_not_prepared", "patch approval details are unavailable; prepare and approve the patch again")
	}
	if !patch.ExpiresAt.After(now) {
		r.cleanupPreparedPatchesLocked(now)
		return nil, newPatchError("patch_approval_expired", "patch approval details expired; prepare and approve the patch again")
	}
	if patch.ArgsHash != patchContentHash(bytes.TrimSpace(call.Args)) {
		return nil, newPatchError("patch_arguments_changed", "patch arguments changed after approval; prepare and approve the patch again")
	}
	r.cleanupPreparedPatchesLocked(now)
	return patch, nil
}

func (r *BuiltinRunner) cleanupPreparedPatchesLocked(now time.Time) {
	for key, patch := range r.preparedPatches {
		if !patch.ExpiresAt.After(now) {
			delete(r.preparedPatches, key)
		}
	}
}

func patchPayload(patch *preparedPatch) map[string]any {
	files := make([]patchFileView, 0, len(patch.Files))
	destructive := false
	for _, file := range patch.Files {
		destructive = destructive || file.Delete
		files = append(files, patchFileView{
			Path:      file.Path,
			Operation: file.Operation,
			Additions: file.Additions,
			Deletions: file.Deletions,
		})
	}
	return map[string]any{
		"projectRoot": patch.ProjectRoot,
		"files":       files,
		"fileCount":   len(files),
		"additions":   patch.Additions,
		"deletions":   patch.Deletions,
		"destructive": destructive,
		"diff":        patch.Diff,
	}
}

func patchFailure(out Result, err error) Result {
	var argumentErr *patchArgumentError
	if errors.As(err, &argumentErr) {
		return patchArgumentFailure(out, argumentErr)
	}
	var limitErr *patchLimitError
	if errors.As(err, &limitErr) {
		return patchLimitFailure(out, limitErr.reason, limitErr.detail, limitErr.count, limitErr.limit)
	}
	var patchErr *patchError
	if errors.As(err, &patchErr) {
		return toolJSONError(out, patchErr.reason, patchErr.detail)
	}
	return toolJSONError(out, "patch_failed", err.Error())
}

func patchPathReason(err error) string {
	switch {
	case errors.Is(err, errProjectDirsRequired):
		return "project_dirs_required"
	case errors.Is(err, errProjectFilePathRequired):
		return "path_required"
	default:
		return "path_not_authorized"
	}
}

func patchContentHash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func buildUnifiedFileDiff(file preparedPatchFile) (string, int, int, error) {
	dmp := diffmatchpatch.New()
	left, right, lines := dmp.DiffLinesToChars(file.OldText, file.NewText)
	diffs := dmp.DiffCharsToLines(dmp.DiffMain(left, right, false), lines)
	chunks := make([]formatdiff.Chunk, 0, len(diffs))
	additions := 0
	deletions := 0
	for _, item := range diffs {
		operation := formatdiff.Equal
		switch item.Type {
		case diffmatchpatch.DiffInsert:
			operation = formatdiff.Add
			additions += patchLineCount(item.Text)
		case diffmatchpatch.DiffDelete:
			operation = formatdiff.Delete
			deletions += patchLineCount(item.Text)
		}
		chunks = append(chunks, patchDiffChunk{content: item.Text, operation: operation})
	}
	mode := filemode.Regular
	if file.Mode&0o111 != 0 {
		mode = filemode.Executable
	}
	var from formatdiff.File
	var to formatdiff.File
	if file.Existed {
		from = patchDiffFile{path: file.Path, hash: plumbing.ComputeHash(plumbing.BlobObject, []byte(file.OldText)), mode: mode}
	}
	if !file.Delete {
		to = patchDiffFile{path: file.Path, hash: plumbing.ComputeHash(plumbing.BlobObject, []byte(file.NewText)), mode: mode}
	}
	patch := patchDiffPatch{files: []formatdiff.FilePatch{patchDiffFilePatch{from: from, to: to, chunks: chunks}}}
	var output strings.Builder
	if err := formatdiff.NewUnifiedEncoder(&output, formatdiff.DefaultContextLines).Encode(patch); err != nil {
		return "", 0, 0, err
	}
	return output.String(), additions, deletions, nil
}

func patchLineCount(text string) int {
	if text == "" {
		return 0
	}
	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}
	return count
}

type patchDiffPatch struct {
	files []formatdiff.FilePatch
}

func (p patchDiffPatch) FilePatches() []formatdiff.FilePatch { return p.files }
func (patchDiffPatch) Message() string                       { return "" }

type patchDiffFilePatch struct {
	from   formatdiff.File
	to     formatdiff.File
	chunks []formatdiff.Chunk
}

func (patchDiffFilePatch) IsBinary() bool { return false }
func (p patchDiffFilePatch) Files() (formatdiff.File, formatdiff.File) {
	return p.from, p.to
}
func (p patchDiffFilePatch) Chunks() []formatdiff.Chunk { return p.chunks }

type patchDiffFile struct {
	path string
	hash plumbing.Hash
	mode filemode.FileMode
}

func (f patchDiffFile) Path() string            { return f.path }
func (f patchDiffFile) Hash() plumbing.Hash     { return f.hash }
func (f patchDiffFile) Mode() filemode.FileMode { return f.mode }

type patchDiffChunk struct {
	content   string
	operation formatdiff.Operation
}

func (c patchDiffChunk) Content() string            { return c.content }
func (c patchDiffChunk) Type() formatdiff.Operation { return c.operation }

func ApprovalDetailsFailure(call Call, err error) Result {
	var writeErr *gitWriteError
	if errors.As(err, &writeErr) {
		return toolJSONError(Result{CallID: call.CallID, Name: call.Name}, writeErr.reason, writeErr.detail)
	}
	return patchFailure(Result{CallID: call.CallID, Name: call.Name}, err)
}

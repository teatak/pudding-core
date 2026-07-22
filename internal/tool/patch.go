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
	patchMaxEditsPerFile  = 64
	patchMaxFileBytes     = 512 << 10
	patchMaxTotalBytes    = 2 << 20
	patchMaxDiffBytes     = 256 << 10
	patchPreparedTTL      = 2 * time.Hour
	patchMaxPreparedItems = 128
)

type patchApplyArgs struct {
	Scope string         `json:"scope"`
	Files []patchFileArg `json:"files"`
}

type patchFileArg struct {
	Path    string         `json:"path"`
	NewText *string        `json:"new_text,omitempty"`
	Delete  bool           `json:"delete,omitempty"`
	Edits   []patchEditArg `json:"edits,omitempty"`
}

type patchEditArg struct {
	OldText    string `json:"old_text"`
	NewText    string `json:"new_text"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
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

func decodePatchApplyArgs(raw json.RawMessage) (patchApplyArgs, *patchArgumentError) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return patchApplyArgs{}, &patchArgumentError{
			kind:   "missing_arguments",
			detail: "patch arguments are empty",
			hint:   "Pass one JSON object with scope and files fields.",
		}
	}
	if trimmed[0] != '{' {
		var value any
		if err := json.Unmarshal(trimmed, &value); err != nil {
			return patchApplyArgs{}, patchJSONArgumentError(err)
		}
		hint := "Pass one JSON object with scope and files fields."
		if trimmed[0] == '"' {
			hint = "Pass the object directly instead of a JSON-encoded string."
		}
		return patchApplyArgs{}, &patchArgumentError{
			kind:     "expected_object",
			detail:   "patch arguments must be a JSON object",
			hint:     hint,
			expected: "object",
		}
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return patchApplyArgs{}, patchJSONArgumentError(err)
	}
	for _, field := range []string{"scope", "files"} {
		if _, ok := fields[field]; !ok {
			return patchApplyArgs{}, &patchArgumentError{
				kind:     "missing_field",
				detail:   "required field is missing: " + field,
				hint:     "Add the required " + field + " field and retry.",
				field:    field,
				expected: patchArgumentExpectedType(field),
			}
		}
	}

	var args patchApplyArgs
	if err := json.Unmarshal(trimmed, &args); err != nil {
		return patchApplyArgs{}, patchJSONArgumentError(err)
	}
	return args, nil
}

func patchJSONArgumentError(err error) *patchArgumentError {
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
	case field == "scope", strings.HasSuffix(field, ".path"), strings.HasSuffix(field, ".new_text"), strings.HasSuffix(field, ".old_text"):
		return "string"
	case field == "files", strings.HasSuffix(field, ".edits"):
		return "array"
	case strings.HasSuffix(field, ".delete"), strings.HasSuffix(field, ".replace_all"):
		return "boolean"
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

func validatePatchApplyArgs(args patchApplyArgs) *patchArgumentError {
	if len(args.Files) == 0 {
		return &patchArgumentError{
			kind:     "empty_files",
			detail:   "files must contain at least one entry",
			hint:     "Add at least one file change and retry.",
			field:    "files",
			expected: "non-empty array",
		}
	}
	return nil
}

func preparePatch(call Call, args patchApplyArgs) (*preparedPatch, error) {
	if strings.TrimSpace(call.SessionID) == "" {
		return nil, newPatchError("session_required", "session id is required for project patches")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return nil, newPatchError("invalid_scope", "patch scope must be project")
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
	operationCount := 0
	if requested.NewText != nil {
		operationCount++
	}
	if requested.Delete {
		operationCount++
	}
	if len(requested.Edits) > 0 {
		operationCount++
	}
	if operationCount != 1 {
		return preparedPatchFile{}, "", newPatchError("invalid_arguments", "each patch file must set exactly one of new_text, edits, or delete=true")
	}
	if len(requested.Edits) > patchMaxEditsPerFile {
		return preparedPatchFile{}, "", newPatchError("too_many_edits", "patch files support at most 64 edits: "+path)
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
	file := preparedPatchFile{Path: filepath.ToSlash(rel), Target: target, Delete: requested.Delete, NewText: "", Mode: 0o600}
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
	case errors.Is(statErr, os.ErrNotExist):
		if requested.Delete {
			return preparedPatchFile{}, "", newPatchError("file_not_found", "cannot delete a missing file: "+file.Path)
		}
		if len(requested.Edits) > 0 {
			return preparedPatchFile{}, "", newPatchError("file_not_found", "cannot apply edits to a missing file: "+file.Path)
		}
		file.OldHash = patchContentHash(nil)
	default:
		return preparedPatchFile{}, "", newPatchError("stat_failed", statErr.Error())
	}
	if requested.Delete {
		file.Operation = "delete"
	} else if len(requested.Edits) > 0 {
		next, err := applyPatchEdits(file.OldText, file.Path, requested.Edits)
		if err != nil {
			return preparedPatchFile{}, "", err
		}
		file.NewText = next
		file.Operation = "update"
	} else {
		file.NewText = *requested.NewText
		if len(file.NewText) > patchMaxFileBytes {
			return preparedPatchFile{}, "", newPatchError("file_too_large", "patched file text must not exceed 512 KiB: "+file.Path)
		}
		if !isToolText([]byte(file.NewText)) {
			return preparedPatchFile{}, "", newPatchError("binary_file", "patched file content must be UTF-8 text without NUL bytes: "+file.Path)
		}
		if file.Existed {
			file.Operation = "update"
		} else {
			file.Operation = "create"
		}
	}
	return file, resolvedRoot, nil
}

func applyPatchEdits(content, filePath string, edits []patchEditArg) (string, error) {
	if len(edits) == 0 || len(edits) > patchMaxEditsPerFile {
		return "", newPatchError("invalid_edits", "patch edits must contain between 1 and 64 entries: "+filePath)
	}
	next := content
	for index, edit := range edits {
		if edit.OldText == "" {
			return "", newPatchError("edit_text_required", "edit old_text must not be empty: "+filePath)
		}
		if !isToolText([]byte(edit.NewText)) {
			return "", newPatchError("binary_file", "edit new_text must be UTF-8 text without NUL bytes: "+filePath)
		}
		matches := strings.Count(next, edit.OldText)
		if matches == 0 {
			return "", newPatchError("edit_text_not_found", "edit "+strconv.Itoa(index+1)+" old_text was not found: "+filePath)
		}
		if matches > 1 && !edit.ReplaceAll {
			return "", newPatchError("edit_text_ambiguous", "edit "+strconv.Itoa(index+1)+" old_text matched more than once: "+filePath)
		}
		replaceCount := 1
		if edit.ReplaceAll {
			replaceCount = -1
		}
		next = strings.Replace(next, edit.OldText, edit.NewText, replaceCount)
		if len(next) > patchMaxFileBytes {
			return "", newPatchError("file_too_large", "edited file text must not exceed 512 KiB: "+filePath)
		}
	}
	return next, nil
}

func (r *BuiltinRunner) patchApply(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, argumentErr := decodePatchApplyArgs(call.Args)
	if argumentErr != nil {
		return patchArgumentFailure(out, argumentErr)
	}
	if argumentErr := validatePatchApplyArgs(args); argumentErr != nil {
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
	case PatchApply:
		// Continue below and prepare the exact filesystem snapshot shown for approval.
	default:
		return nil, newPatchError("approval_details_unsupported", "approval details are not available for this tool")
	}
	args, argumentErr := decodePatchApplyArgs(call.Args)
	if argumentErr != nil {
		return nil, argumentErr
	}
	if argumentErr := validatePatchApplyArgs(args); argumentErr != nil {
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

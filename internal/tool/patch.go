package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	formatdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/teatak/pudding-core/internal/store"
)

const (
	patchMaxFiles       = 16
	patchMaxFileBytes   = 512 << 10
	patchMaxTotalBytes  = 2 << 20
	patchMaxDiffBytes   = 256 << 10
	patchProposalTTL    = 2 * time.Hour
	patchMaxStoredItems = 128
)

type patchProposeArgs struct {
	Scope string                `json:"scope"`
	Files []patchProposeFileArg `json:"files"`
}

type patchProposeFileArg struct {
	Path    string  `json:"path"`
	NewText *string `json:"new_text,omitempty"`
	Delete  bool    `json:"delete,omitempty"`
}

type patchApplyArgs struct {
	ProposalID string `json:"proposal_id"`
}

type patchProposal struct {
	ID          string
	SessionID   string
	TurnID      string
	ProjectRoot string
	Files       []patchProposalFile
	Diff        string
	Additions   int
	Deletions   int
	CreatedAt   time.Time
	ExpiresAt   time.Time
	Applying    bool
}

type patchProposalFile struct {
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

type patchProposalFileView struct {
	Path      string `json:"path"`
	Operation string `json:"operation"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type patchError struct {
	reason string
	detail string
}

func (e *patchError) Error() string {
	return e.detail
}

func newPatchError(reason, detail string) error {
	return &patchError{reason: reason, detail: detail}
}

func (r *BuiltinRunner) patchPropose(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	if strings.TrimSpace(call.SessionID) == "" {
		return toolJSONError(out, "session_required", "session id is required for patch proposals")
	}
	var args patchProposeArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "patch proposal arguments must be a JSON object")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "patch proposal scope must be project")
	}
	if len(args.Files) == 0 || len(args.Files) > patchMaxFiles {
		return toolJSONError(out, "invalid_arguments", "files must contain between 1 and 16 entries")
	}

	proposal := &patchProposal{
		ID:        store.NewID("patch"),
		SessionID: call.SessionID,
		TurnID:    call.TurnID,
		CreatedAt: time.Now(),
	}
	proposal.ExpiresAt = proposal.CreatedAt.Add(patchProposalTTL)
	seen := make(map[string]bool, len(args.Files))
	totalBytes := 0
	var diffs strings.Builder
	for _, requested := range args.Files {
		file, root, err := preparePatchProposalFile(call.ProjectDirs, requested)
		if err != nil {
			return patchFailure(out, err)
		}
		if proposal.ProjectRoot == "" {
			proposal.ProjectRoot = root
		} else if filepath.Clean(proposal.ProjectRoot) != filepath.Clean(root) {
			return toolJSONError(out, "cross_root_patch", "all proposal files must be inside the same authorized project root")
		}
		if seen[file.Target] {
			return toolJSONError(out, "duplicate_path", "patch proposal contains the same file more than once: "+file.Path)
		}
		seen[file.Target] = true
		if file.Existed && !file.Delete && file.OldText == file.NewText {
			continue
		}
		totalBytes += len(file.OldText) + len(file.NewText)
		if totalBytes > patchMaxTotalBytes {
			return toolJSONError(out, "proposal_too_large", "patch proposal source and destination text exceeds 2 MiB")
		}
		fileDiff, additions, deletions, err := buildUnifiedFileDiff(file)
		if err != nil {
			return toolJSONError(out, "diff_failed", err.Error())
		}
		file.Additions = additions
		file.Deletions = deletions
		diffs.WriteString(fileDiff)
		if diffs.Len() > patchMaxDiffBytes {
			return toolJSONError(out, "proposal_diff_too_large", "review diff exceeds 256 KiB; split the change into smaller proposals")
		}
		proposal.Files = append(proposal.Files, file)
		proposal.Additions += additions
		proposal.Deletions += deletions
	}
	if len(proposal.Files) == 0 {
		return toolJSONError(out, "no_changes", "patch proposal does not change any files")
	}
	proposal.Diff = diffs.String()

	r.patchMu.Lock()
	r.cleanupPatchProposalsLocked(proposal.CreatedAt)
	if len(r.patchProposals) >= patchMaxStoredItems {
		r.patchMu.Unlock()
		return toolJSONError(out, "proposal_limit_reached", "too many active patch proposals; wait for older proposals to expire")
	}
	r.patchProposals[proposal.ID] = proposal
	r.patchMu.Unlock()

	payload := patchProposalPayload(proposal)
	payload["ok"] = true
	payload["status"] = "proposed"
	return withResultSummary(toolJSON(out, true, payload), SummaryChangedLines, proposal.Additions+proposal.Deletions)
}

func preparePatchProposalFile(projectDirs []string, requested patchProposeFileArg) (patchProposalFile, string, error) {
	path := strings.TrimSpace(requested.Path)
	if path == "" {
		return patchProposalFile{}, "", newPatchError("path_required", "patch file path is required")
	}
	if requested.Delete == (requested.NewText != nil) {
		return patchProposalFile{}, "", newPatchError("invalid_arguments", "each patch file must set exactly one of new_text or delete=true")
	}
	root, target, rel, err := resolveProjectPath(projectDirs, path, false, true)
	if err != nil {
		return patchProposalFile{}, "", &patchError{reason: patchPathReason(err), detail: err.Error()}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return patchProposalFile{}, "", newPatchError("project_root_unavailable", err.Error())
	}
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		target = filepath.Join(resolvedRoot, filepath.FromSlash(rel))
	}
	file := patchProposalFile{Path: filepath.ToSlash(rel), Target: target, Delete: requested.Delete, NewText: "", Mode: 0o600}
	info, statErr := os.Lstat(target)
	switch {
	case statErr == nil:
		if info.Mode()&os.ModeSymlink != 0 {
			return patchProposalFile{}, "", newPatchError("symlink_unsupported", "patch proposals do not support symlink files: "+file.Path)
		}
		if !info.Mode().IsRegular() {
			return patchProposalFile{}, "", newPatchError("regular_file_required", "patch proposal path must be a regular file: "+file.Path)
		}
		if info.Size() > patchMaxFileBytes {
			return patchProposalFile{}, "", newPatchError("file_too_large", "patch proposal files must not exceed 512 KiB: "+file.Path)
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return patchProposalFile{}, "", newPatchError("read_failed", err.Error())
		}
		if !isToolText(data) {
			return patchProposalFile{}, "", newPatchError("binary_file", "patch proposals support UTF-8 text files only: "+file.Path)
		}
		file.Existed = true
		file.OldText = string(data)
		file.OldHash = patchContentHash(data)
		file.Mode = info.Mode().Perm()
	case errors.Is(statErr, os.ErrNotExist):
		if requested.Delete {
			return patchProposalFile{}, "", newPatchError("file_not_found", "cannot delete a missing file: "+file.Path)
		}
		file.OldHash = patchContentHash(nil)
	default:
		return patchProposalFile{}, "", newPatchError("stat_failed", statErr.Error())
	}
	if requested.Delete {
		file.Operation = "delete"
	} else {
		file.NewText = *requested.NewText
		if len(file.NewText) > patchMaxFileBytes {
			return patchProposalFile{}, "", newPatchError("file_too_large", "proposed file text must not exceed 512 KiB: "+file.Path)
		}
		if !isToolText([]byte(file.NewText)) {
			return patchProposalFile{}, "", newPatchError("binary_file", "proposed file content must be UTF-8 text without NUL bytes: "+file.Path)
		}
		if file.Existed {
			file.Operation = "update"
		} else {
			file.Operation = "create"
		}
	}
	return file, resolvedRoot, nil
}

func (r *BuiltinRunner) patchApply(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodePatchApplyArgs(call.Args)
	if err != nil {
		return patchFailure(out, err)
	}
	proposal, err := r.beginPatchApply(call.SessionID, args.ProposalID)
	if err != nil {
		return patchFailure(out, err)
	}
	warnings, applyErr := applyPatchProposal(call.ProjectDirs, proposal)
	r.patchMu.Lock()
	if applyErr == nil {
		delete(r.patchProposals, proposal.ID)
	} else if stored := r.patchProposals[proposal.ID]; stored == proposal {
		stored.Applying = false
	}
	r.patchMu.Unlock()
	if applyErr != nil {
		return patchFailure(out, applyErr)
	}
	payload := patchProposalPayload(proposal)
	payload["ok"] = true
	payload["status"] = "applied"
	payload["warnings"] = warnings
	delete(payload, "diff")
	delete(payload, "expiresAt")
	return withResultSummary(toolJSON(out, true, payload), SummaryChangedLines, proposal.Additions+proposal.Deletions)
}

func (r *BuiltinRunner) beginPatchApply(sessionID, proposalID string) (*patchProposal, error) {
	r.patchMu.Lock()
	defer r.patchMu.Unlock()
	r.cleanupPatchProposalsLocked(time.Now())
	proposal := r.patchProposals[proposalID]
	if proposal == nil || strings.TrimSpace(sessionID) == "" || proposal.SessionID != sessionID {
		return nil, newPatchError("proposal_not_found", "patch proposal was not found for this session")
	}
	if proposal.Applying {
		return nil, newPatchError("proposal_busy", "patch proposal is already being applied")
	}
	proposal.Applying = true
	return proposal, nil
}

func decodePatchApplyArgs(raw json.RawMessage) (patchApplyArgs, error) {
	var args patchApplyArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, newPatchError("invalid_arguments", "patch apply arguments must be a JSON object")
	}
	args.ProposalID = strings.TrimSpace(args.ProposalID)
	if args.ProposalID == "" {
		return args, newPatchError("proposal_id_required", "proposal_id is required")
	}
	return args, nil
}

func (r *BuiltinRunner) ApprovalDetails(ctx context.Context, call Call) (map[string]any, error) {
	if call.Name != PatchApply {
		return r.gitWriteApprovalDetails(ctx, call)
	}
	args, err := decodePatchApplyArgs(call.Args)
	if err != nil {
		return nil, err
	}
	r.patchMu.Lock()
	r.cleanupPatchProposalsLocked(time.Now())
	proposal := r.patchProposals[args.ProposalID]
	if proposal == nil || proposal.SessionID != call.SessionID {
		r.patchMu.Unlock()
		return nil, newPatchError("proposal_not_found", "patch proposal was not found for this session")
	}
	r.patchMu.Unlock()
	if err := validatePatchProposalState(call.ProjectDirs, proposal); err != nil {
		return nil, err
	}
	payload := patchProposalPayload(proposal)
	paths := make([]string, 0, len(proposal.Files))
	for _, file := range proposal.Files {
		paths = append(paths, file.Path)
	}
	payload["paths"] = paths
	return payload, nil
}

func (r *BuiltinRunner) cleanupPatchProposalsLocked(now time.Time) {
	for id, proposal := range r.patchProposals {
		if !proposal.Applying && !proposal.ExpiresAt.After(now) {
			delete(r.patchProposals, id)
		}
	}
}

func patchProposalPayload(proposal *patchProposal) map[string]any {
	files := make([]patchProposalFileView, 0, len(proposal.Files))
	for _, file := range proposal.Files {
		files = append(files, patchProposalFileView{
			Path:      file.Path,
			Operation: file.Operation,
			Additions: file.Additions,
			Deletions: file.Deletions,
		})
	}
	return map[string]any{
		"proposalID":  proposal.ID,
		"projectRoot": proposal.ProjectRoot,
		"files":       files,
		"fileCount":   len(files),
		"additions":   proposal.Additions,
		"deletions":   proposal.Deletions,
		"diff":        proposal.Diff,
		"expiresAt":   proposal.ExpiresAt.UTC().Format(time.RFC3339),
	}
}

func patchFailure(out Result, err error) Result {
	var proposalErr *patchError
	if errors.As(err, &proposalErr) {
		return toolJSONError(out, proposalErr.reason, proposalErr.detail)
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

func buildUnifiedFileDiff(file patchProposalFile) (string, int, int, error) {
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

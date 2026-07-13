package tool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/projectgit"
)

const (
	gitWriteMaxPaths          = 128
	gitStageMaxFileBytes      = 64 << 20
	gitStageMaxTotalBytes     = 256 << 20
	gitCommitMaxMessageBytes  = 16 << 10
	gitCommitApprovalTTL      = 2 * time.Hour
	gitCommitMaxApprovalItems = 128
)

type gitPathsArgs struct {
	Scope string   `json:"scope"`
	CWD   string   `json:"cwd,omitempty"`
	Paths []string `json:"paths"`
}

type gitCommitArgs struct {
	Scope   string `json:"scope"`
	CWD     string `json:"cwd,omitempty"`
	Message string `json:"message"`
}

type gitWriteRepository struct {
	gitRepository
	GitDir    string
	CommonDir string
	IndexPath string
}

type gitCommitApprovalSnapshot struct {
	SessionID   string
	CallID      string
	RepoRoot    string
	Fingerprint string
	ExpiresAt   time.Time
}

type gitWriteError struct {
	reason string
	detail string
}

func (e *gitWriteError) Error() string { return e.detail }

func newGitWriteError(reason, detail string) error {
	return &gitWriteError{reason: reason, detail: detail}
}

func RequiresApprovalDetails(name string) bool {
	switch name {
	case CommandRun, CommandStart, PatchApply, GitStage, GitUnstage, GitCommit:
		return true
	default:
		return false
	}
}

func (r *BuiltinRunner) gitStage(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeGitPathsArgs(call.Args, "stage")
	if err != nil {
		return gitWriteFailure(out, err)
	}
	repo, err := resolveGitWriteRepository(ctx, call, args.Scope, args.CWD)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	paths, err := normalizeGitWritePaths(repo, args.Paths)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	if err := stageGitPaths(ctx, repo, paths); err != nil {
		return gitWriteFailure(out, err)
	}
	snapshot, err := readGitWriteStatus(ctx, repo.gitRepository)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	payload := gitWriteStatusPayload(repo.gitRepository, snapshot)
	payload["ok"] = true
	payload["status"] = "staged"
	payload["paths"] = paths
	payload["pathCount"] = len(paths)
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(paths))
}

func (r *BuiltinRunner) gitUnstage(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeGitPathsArgs(call.Args, "unstage")
	if err != nil {
		return gitWriteFailure(out, err)
	}
	repo, err := resolveGitWriteRepository(ctx, call, args.Scope, args.CWD)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	paths, err := normalizeGitWritePaths(repo, args.Paths)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	hasHead, err := gitRepositoryHasHead(ctx, repo.Root)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	gitArgs := []string{"--literal-pathspecs"}
	if hasHead {
		gitArgs = append(gitArgs, "restore", "--staged", "--")
	} else {
		gitArgs = append(gitArgs, "rm", "--cached", "-r", "--ignore-unmatch", "--")
	}
	gitArgs = append(gitArgs, paths...)
	result := runGit(ctx, repo.Root, gitMetadataLimitBytes, gitArgs...)
	if result.err != nil {
		return gitExecFailure(out, ctx, result, "git_unstage_failed")
	}
	snapshot, err := readGitWriteStatus(ctx, repo.gitRepository)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	payload := gitWriteStatusPayload(repo.gitRepository, snapshot)
	payload["ok"] = true
	payload["status"] = "unstaged"
	payload["paths"] = paths
	payload["pathCount"] = len(paths)
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(paths))
}

func (r *BuiltinRunner) gitCommit(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	args, err := decodeGitCommitArgs(call.Args)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	repo, err := resolveGitWriteRepository(ctx, call, args.Scope, args.CWD)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	approval, err := r.takeGitCommitApproval(call.SessionID, call.CallID, repo.Root)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	fingerprint, err := gitCommitFingerprint(ctx, repo.Root)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	if fingerprint != approval.Fingerprint {
		return gitWriteFailure(out, newGitWriteError("commit_snapshot_stale", "HEAD or the Git index changed while commit approval was pending"))
	}
	if state := gitRepositoryOperationState(repo); state != "" {
		return gitWriteFailure(out, newGitWriteError("git_operation_in_progress", "cannot create a normal commit while Git operation state is present: "+state))
	}
	snapshot, err := readGitWriteStatus(ctx, repo.gitRepository)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	if snapshot.Conflicted > 0 {
		return gitWriteFailure(out, newGitWriteError("git_conflicts", "resolve Git conflicts before committing"))
	}
	if snapshot.Staged == 0 {
		return gitWriteFailure(out, newGitWriteError("no_staged_changes", "there are no staged changes to commit"))
	}
	result := runGit(ctx, repo.Root, gitMetadataLimitBytes,
		"-c", "core.hooksPath="+os.DevNull,
		"-c", "commit.gpgSign=false",
		"-c", "gc.auto=0",
		"commit", "--no-verify", "--no-gpg-sign", "--cleanup=verbatim", "-m", args.Message,
	)
	if result.err != nil {
		return gitExecFailure(out, ctx, result, "git_commit_failed")
	}
	commit, err := readGitHeadCommit(ctx, repo.Root)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	after, err := readGitWriteStatus(ctx, repo.gitRepository)
	if err != nil {
		return gitWriteFailure(out, err)
	}
	payload := gitWriteStatusPayload(repo.gitRepository, after)
	payload["ok"] = true
	payload["status"] = "committed"
	payload["commit"] = commit
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedFields, len(payload))
}

func (r *BuiltinRunner) gitWriteApprovalDetails(ctx context.Context, call Call) (map[string]any, error) {
	switch call.Name {
	case GitStage, GitUnstage:
		operation := "stage"
		if call.Name == GitUnstage {
			operation = "unstage"
		}
		args, err := decodeGitPathsArgs(call.Args, operation)
		if err != nil {
			return nil, err
		}
		repo, err := resolveGitWriteRepository(ctx, call, args.Scope, args.CWD)
		if err != nil {
			return nil, err
		}
		paths, err := normalizeGitWritePaths(repo, args.Paths)
		if err != nil {
			return nil, err
		}
		snapshot, err := readGitWriteStatus(ctx, repo.gitRepository)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"repoRoot":        repo.Root,
			"paths":           paths,
			"pathCount":       len(paths),
			"branch":          snapshot.Branch,
			"stagedCount":     snapshot.Staged,
			"unstagedCount":   snapshot.Unstaged,
			"untrackedCount":  snapshot.Untracked,
			"conflictedCount": snapshot.Conflicted,
		}, nil
	case GitCommit:
		args, err := decodeGitCommitArgs(call.Args)
		if err != nil {
			return nil, err
		}
		repo, err := resolveGitWriteRepository(ctx, call, args.Scope, args.CWD)
		if err != nil {
			return nil, err
		}
		if state := gitRepositoryOperationState(repo); state != "" {
			return nil, newGitWriteError("git_operation_in_progress", "cannot create a normal commit while Git operation state is present: "+state)
		}
		status, err := readGitWriteStatus(ctx, repo.gitRepository)
		if err != nil {
			return nil, err
		}
		if status.Conflicted > 0 {
			return nil, newGitWriteError("git_conflicts", "resolve Git conflicts before committing")
		}
		if status.Staged == 0 {
			return nil, newGitWriteError("no_staged_changes", "there are no staged changes to commit")
		}
		diff, err := readGitWriteDiff(ctx, repo.gitRepository)
		if err != nil {
			return nil, err
		}
		fingerprint, err := gitCommitFingerprint(ctx, repo.Root)
		if err != nil {
			return nil, err
		}
		r.storeGitCommitApproval(gitCommitApprovalSnapshot{
			SessionID:   call.SessionID,
			CallID:      call.CallID,
			RepoRoot:    repo.Root,
			Fingerprint: fingerprint,
			ExpiresAt:   time.Now().Add(gitCommitApprovalTTL),
		})
		paths := make([]string, 0, len(diff.Files))
		for _, file := range diff.Files {
			paths = append(paths, file.Path)
		}
		return map[string]any{
			"repoRoot":        repo.Root,
			"branch":          status.Branch,
			"head":            status.Head,
			"commitMessage":   args.Message,
			"paths":           paths,
			"files":           diff.Files,
			"fileCount":       len(diff.Files),
			"additions":       diff.Additions,
			"deletions":       diff.Deletions,
			"diff":            diff.Diff,
			"truncated":       diff.Truncated,
			"stagedCount":     status.Staged,
			"unstagedCount":   status.Unstaged,
			"untrackedCount":  status.Untracked,
			"conflictedCount": status.Conflicted,
		}, nil
	default:
		return nil, nil
	}
}

func decodeGitPathsArgs(raw json.RawMessage, operation string) (gitPathsArgs, error) {
	var args gitPathsArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, newGitWriteError("invalid_arguments", "git "+operation+" arguments must be a JSON object")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return args, newGitWriteError("invalid_scope", "git "+operation+" scope must be project")
	}
	if len(args.Paths) == 0 || len(args.Paths) > gitWriteMaxPaths {
		return args, newGitWriteError("invalid_arguments", "paths must contain between 1 and 128 entries")
	}
	return args, nil
}

func decodeGitCommitArgs(raw json.RawMessage) (gitCommitArgs, error) {
	var args gitCommitArgs
	if len(raw) == 0 || json.Unmarshal(raw, &args) != nil {
		return args, newGitWriteError("invalid_arguments", "git commit arguments must be a JSON object")
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return args, newGitWriteError("invalid_scope", "git commit scope must be project")
	}
	args.Message = strings.TrimSpace(args.Message)
	if args.Message == "" {
		return args, newGitWriteError("commit_message_required", "commit message is required")
	}
	if len(args.Message) > gitCommitMaxMessageBytes || strings.ContainsRune(args.Message, '\x00') {
		return args, newGitWriteError("invalid_commit_message", "commit message must be UTF-8 text no longer than 16 KiB")
	}
	return args, nil
}

func resolveGitWriteRepository(ctx context.Context, call Call, scope, cwd string) (gitWriteRepository, error) {
	repo, failed := resolveGitRepository(ctx, call, scope, cwd)
	if failed != nil {
		if failed.path != nil {
			return gitWriteRepository{}, newGitWriteError(patchPathReason(failed.path), failed.path.Error())
		}
		return gitWriteRepository{}, newGitWriteError(failed.reason, failed.detail)
	}
	gitDir, err := resolveGitAdminPath(ctx, repo, "--absolute-git-dir", false)
	if err != nil {
		return gitWriteRepository{}, err
	}
	commonDir, err := resolveGitAdminPath(ctx, repo, "--git-common-dir", false)
	if err != nil {
		return gitWriteRepository{}, err
	}
	indexPath, err := resolveGitAdminPath(ctx, repo, "--git-path", true, "index")
	if err != nil {
		return gitWriteRepository{}, err
	}
	return gitWriteRepository{gitRepository: repo, GitDir: gitDir, CommonDir: commonDir, IndexPath: indexPath}, nil
}

func resolveGitAdminPath(ctx context.Context, repo gitRepository, flag string, allowMissing bool, extra ...string) (string, error) {
	args := []string{"rev-parse", flag}
	args = append(args, extra...)
	result := runGit(ctx, repo.Root, 4096, args...)
	if result.err != nil || result.stdout.Truncated() {
		return "", newGitWriteError("git_metadata_unavailable", gitExecDetail(result))
	}
	path := strings.TrimSpace(result.stdout.String())
	if !filepath.IsAbs(path) {
		path = filepath.Join(repo.Root, path)
	}
	path = filepath.Clean(path)
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		if !allowMissing || !errors.Is(err, os.ErrNotExist) {
			return "", newGitWriteError("git_metadata_unavailable", err.Error())
		}
		parent, parentErr := resolveExistingParent(path)
		if parentErr != nil || !pathInsideRoot(parent, repo.ProjectRoot) {
			return "", newGitWriteError("git_metadata_outside_project", "Git metadata path is outside the authorized project directory")
		}
		resolved = path
	}
	if !pathInsideRoot(resolved, repo.ProjectRoot) {
		return "", newGitWriteError("git_metadata_outside_project", "Git metadata path is outside the authorized project directory")
	}
	return resolved, nil
}

func normalizeGitWritePaths(repo gitWriteRepository, rawPaths []string) ([]string, error) {
	seen := make(map[string]bool, len(rawPaths))
	paths := make([]string, 0, len(rawPaths))
	for _, raw := range rawPaths {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.ContainsRune(raw, '\x00') {
			return nil, newGitWriteError("path_required", "Git write paths must be non-empty")
		}
		candidate := raw
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(repo.Root, filepath.FromSlash(candidate))
		}
		candidate = filepath.Clean(candidate)
		if !pathInsideRoot(candidate, repo.Root) {
			return nil, newGitWriteError("path_not_authorized", "Git write path is outside the repository: "+raw)
		}
		rel, err := filepath.Rel(repo.Root, candidate)
		if err != nil || rel == "." {
			return nil, newGitWriteError("explicit_file_required", "Git writes require explicit file paths")
		}
		first := strings.Split(filepath.ToSlash(rel), "/")[0]
		if strings.EqualFold(first, ".git") {
			return nil, newGitWriteError("git_metadata_forbidden", "Git metadata paths cannot be staged or unstaged")
		}
		resolvedParent, err := resolveExistingParent(candidate)
		if err != nil || !pathInsideRoot(resolvedParent, repo.Root) {
			return nil, newGitWriteError("path_not_authorized", "Git write path resolves outside the repository: "+raw)
		}
		if info, err := os.Lstat(candidate); err == nil {
			if info.IsDir() {
				return nil, newGitWriteError("explicit_file_required", "Git writes do not accept directories: "+raw)
			}
			if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
				return nil, newGitWriteError("regular_file_required", "Git write path must be a regular file or symlink: "+raw)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, newGitWriteError("stat_failed", err.Error())
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			seen[rel] = true
			paths = append(paths, rel)
		}
	}
	if len(paths) == 0 {
		return nil, newGitWriteError("path_required", "Git writes require at least one file path")
	}
	sort.Strings(paths)
	return paths, nil
}

func stageGitPaths(ctx context.Context, repo gitWriteRepository, paths []string) error {
	formatResult := runGit(ctx, repo.Root, 64, "rev-parse", "--show-object-format")
	if formatResult.err != nil || formatResult.stdout.Truncated() {
		return newGitWriteError("git_object_format_failed", gitExecDetail(formatResult))
	}
	zeroOID := ""
	switch strings.TrimSpace(formatResult.stdout.String()) {
	case "sha1":
		zeroOID = strings.Repeat("0", 40)
	case "sha256":
		zeroOID = strings.Repeat("0", 64)
	default:
		return newGitWriteError("git_object_format_unsupported", "unsupported Git object format")
	}
	var indexInfo strings.Builder
	totalBytes := int64(0)
	for _, path := range paths {
		target := filepath.Join(repo.Root, filepath.FromSlash(path))
		info, err := os.Lstat(target)
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(&indexInfo, "0 %s\t%s%c", zeroOID, path, byte(0))
			continue
		}
		if err != nil {
			return newGitWriteError("stat_failed", err.Error())
		}
		mode := "100644"
		var content []byte
		if info.Mode()&os.ModeSymlink != 0 {
			link, err := os.Readlink(target)
			if err != nil {
				return newGitWriteError("read_failed", err.Error())
			}
			mode = "120000"
			content = []byte(link)
		} else {
			if !info.Mode().IsRegular() {
				return newGitWriteError("regular_file_required", "Git stage path must be a regular file or symlink: "+path)
			}
			if info.Size() > gitStageMaxFileBytes {
				return newGitWriteError("git_stage_file_too_large", "Git stage files must not exceed 64 MiB: "+path)
			}
			content, err = os.ReadFile(target)
			if err != nil {
				return newGitWriteError("read_failed", err.Error())
			}
			if info.Mode().Perm()&0o111 != 0 {
				mode = "100755"
			}
		}
		totalBytes += int64(len(content))
		if totalBytes > gitStageMaxTotalBytes {
			return newGitWriteError("git_stage_too_large", "Git stage input exceeds the 256 MiB safety limit")
		}
		hashResult := runGitInput(ctx, repo.Root, 256, content, "hash-object", "-w", "--stdin")
		if hashResult.err != nil || hashResult.stdout.Truncated() {
			return newGitWriteError(gitFailureReason(ctx, hashResult.err, "git_hash_failed"), gitExecDetail(hashResult))
		}
		oid := strings.TrimSpace(hashResult.stdout.String())
		if len(oid) != len(zeroOID) {
			return newGitWriteError("git_hash_failed", "Git returned an invalid object id")
		}
		fmt.Fprintf(&indexInfo, "%s %s\t%s%c", mode, oid, path, byte(0))
	}
	result := runGitInput(ctx, repo.Root, gitMetadataLimitBytes, []byte(indexInfo.String()), "update-index", "-z", "--index-info")
	if result.err != nil {
		return newGitWriteError(gitFailureReason(ctx, result.err, "git_stage_failed"), gitExecDetail(result))
	}
	return nil
}

func readGitWriteStatus(ctx context.Context, repo gitRepository) (gitStatusSnapshot, error) {
	snapshot, err := projectgit.ReadStatus(ctx, projectgit.Repository{ProjectRoot: repo.ProjectRoot, Root: repo.Root})
	if err != nil {
		code := projectgit.ErrorCode(err)
		if code == "" {
			code = "git_status_failed"
		}
		return gitStatusSnapshot{}, newGitWriteError(code, err.Error())
	}
	return snapshot, nil
}

type gitWriteDiffSnapshot struct {
	Diff      string
	Truncated bool
	Files     []gitDiffFile
	Additions int
	Deletions int
}

func readGitWriteDiff(ctx context.Context, repo gitRepository) (gitWriteDiffSnapshot, error) {
	diff, err := projectgit.ReadPatchDiff(ctx, projectgit.Repository{ProjectRoot: repo.ProjectRoot, Root: repo.Root}, true, gitDiffOutputLimitBytes)
	if err != nil {
		code := projectgit.ErrorCode(err)
		if code == "" {
			code = "git_diff_failed"
		}
		return gitWriteDiffSnapshot{}, newGitWriteError(code, err.Error())
	}
	return gitWriteDiffSnapshot{
		Diff:      diff.Patch,
		Truncated: diff.Truncated,
		Files:     diff.Files,
		Additions: diff.Additions,
		Deletions: diff.Deletions,
	}, nil
}

func gitWriteStatusPayload(repo gitRepository, snapshot gitStatusSnapshot) map[string]any {
	return map[string]any{
		"cwd":             repo.CWD,
		"repoRoot":        repo.Root,
		"head":            snapshot.Head,
		"branch":          snapshot.Branch,
		"clean":           len(snapshot.Files) == 0,
		"files":           snapshot.Files,
		"fileCount":       len(snapshot.Files),
		"stagedCount":     snapshot.Staged,
		"unstagedCount":   snapshot.Unstaged,
		"untrackedCount":  snapshot.Untracked,
		"conflictedCount": snapshot.Conflicted,
	}
}

func gitRepositoryHasHead(ctx context.Context, repoRoot string) (bool, error) {
	result := runGit(ctx, repoRoot, 1024, "rev-parse", "--verify", "--quiet", "HEAD")
	if result.err == nil {
		return true, nil
	}
	if gitExitCode(result.err) == 1 && ctx.Err() == nil {
		return false, nil
	}
	return false, newGitWriteError(gitFailureReason(ctx, result.err, "git_head_failed"), gitExecDetail(result))
}

func gitCommitFingerprint(ctx context.Context, repoRoot string) (string, error) {
	hasHead, err := gitRepositoryHasHead(ctx, repoRoot)
	if err != nil {
		return "", err
	}
	args := []string{"ls-files", "--stage", "-z"}
	head := "unborn"
	if hasHead {
		headResult := runGit(ctx, repoRoot, 1024, "rev-parse", "HEAD")
		if headResult.err != nil || headResult.stdout.Truncated() {
			return "", newGitWriteError("git_head_failed", gitExecDetail(headResult))
		}
		head = strings.TrimSpace(headResult.stdout.String())
		args = []string{"diff", "--cached", "--raw", "-z", "--no-renames", "--no-ext-diff", "--no-textconv"}
	}
	result := runGit(ctx, repoRoot, gitMetadataLimitBytes, args...)
	if result.err != nil {
		return "", newGitWriteError(gitFailureReason(ctx, result.err, "git_index_failed"), gitExecDetail(result))
	}
	if result.stdout.Truncated() {
		return "", newGitWriteError("git_index_too_large", "staged Git metadata exceeds the approval safety limit")
	}
	sum := sha256.Sum256(append([]byte(head+"\x00"), []byte(result.stdout.String())...))
	return hex.EncodeToString(sum[:]), nil
}

func gitRepositoryOperationState(repo gitWriteRepository) string {
	checks := []struct {
		root string
		name string
	}{
		{repo.GitDir, "MERGE_HEAD"},
		{repo.GitDir, "CHERRY_PICK_HEAD"},
		{repo.GitDir, "REVERT_HEAD"},
		{repo.GitDir, "BISECT_LOG"},
		{repo.GitDir, "rebase-apply"},
		{repo.GitDir, "rebase-merge"},
		{repo.GitDir, "sequencer"},
	}
	for _, check := range checks {
		if _, err := os.Stat(filepath.Join(check.root, check.name)); err == nil {
			return check.name
		}
	}
	return ""
}

func readGitHeadCommit(ctx context.Context, repoRoot string) (gitCommit, error) {
	format := "%H%x00%h%x00%P%x00%an%x00%ae%x00%aI%x00%s"
	result := runGit(ctx, repoRoot, gitMetadataLimitBytes, "log", "-z", "--max-count=1", "--format="+format)
	if result.err != nil || result.stdout.Truncated() {
		return gitCommit{}, newGitWriteError(gitFailureReason(ctx, result.err, "git_log_failed"), gitExecDetail(result))
	}
	commits, err := parseGitLog(result.stdout.String())
	if err != nil || len(commits) != 1 {
		if err == nil {
			err = errors.New("new commit is unavailable")
		}
		return gitCommit{}, newGitWriteError("git_parse_failed", err.Error())
	}
	return commits[0], nil
}

func (r *BuiltinRunner) storeGitCommitApproval(snapshot gitCommitApprovalSnapshot) {
	r.gitApprovalMu.Lock()
	defer r.gitApprovalMu.Unlock()
	r.cleanupGitCommitApprovalsLocked(time.Now())
	if len(r.gitApprovals) >= gitCommitMaxApprovalItems {
		oldestKey := ""
		var oldest time.Time
		for key, item := range r.gitApprovals {
			if oldestKey == "" || item.ExpiresAt.Before(oldest) {
				oldestKey, oldest = key, item.ExpiresAt
			}
		}
		delete(r.gitApprovals, oldestKey)
	}
	r.gitApprovals[gitApprovalKey(snapshot.SessionID, snapshot.CallID)] = snapshot
}

func (r *BuiltinRunner) takeGitCommitApproval(sessionID, callID, repoRoot string) (gitCommitApprovalSnapshot, error) {
	r.gitApprovalMu.Lock()
	defer r.gitApprovalMu.Unlock()
	r.cleanupGitCommitApprovalsLocked(time.Now())
	key := gitApprovalKey(sessionID, callID)
	snapshot, ok := r.gitApprovals[key]
	delete(r.gitApprovals, key)
	if !ok || strings.TrimSpace(sessionID) == "" || snapshot.RepoRoot != repoRoot {
		return gitCommitApprovalSnapshot{}, newGitWriteError("commit_approval_snapshot_required", "commit requires a current staged-diff approval snapshot")
	}
	return snapshot, nil
}

func (r *BuiltinRunner) cleanupGitCommitApprovalsLocked(now time.Time) {
	for key, snapshot := range r.gitApprovals {
		if !snapshot.ExpiresAt.After(now) {
			delete(r.gitApprovals, key)
		}
	}
}

func gitApprovalKey(sessionID, callID string) string {
	return sessionID + "\x00" + callID
}

func gitWriteFailure(out Result, err error) Result {
	var writeErr *gitWriteError
	if errors.As(err, &writeErr) {
		return toolJSONError(out, writeErr.reason, writeErr.detail)
	}
	return toolJSONError(out, "git_write_failed", fmt.Sprint(err))
}

package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/teatak/pudding-core/internal/projectgit"
)

const (
	gitDiffOutputLimitBytes = 128 << 10
	gitMetadataLimitBytes   = 4 << 20
	gitStderrLimitBytes     = 16 << 10
	gitDefaultLogLimit      = 20
	gitMaxLogLimit          = 100
)

type gitBaseArgs struct {
	Scope string `json:"scope"`
	CWD   string `json:"cwd,omitempty"`
}

type gitDiffArgs struct {
	Scope  string `json:"scope"`
	CWD    string `json:"cwd,omitempty"`
	Staged bool   `json:"staged,omitempty"`
}

type gitLogArgs struct {
	Scope string `json:"scope"`
	CWD   string `json:"cwd,omitempty"`
	Limit int    `json:"limit,omitempty"`
}

type gitRepository struct {
	CWD         string
	Root        string
	ProjectRoot string
}

type gitExecResult struct {
	stdout *truncatingBuffer
	stderr *truncatingBuffer
	err    error
}

type gitStatusFile = projectgit.StatusFile
type gitStatusSnapshot = projectgit.Status
type gitDiffFile = projectgit.DiffFile

type gitCommit struct {
	Hash        string   `json:"hash"`
	ShortHash   string   `json:"shortHash"`
	Parents     []string `json:"parents"`
	AuthorName  string   `json:"authorName"`
	AuthorEmail string   `json:"authorEmail"`
	AuthoredAt  string   `json:"authoredAt"`
	Subject     string   `json:"subject"`
}

func (r *BuiltinRunner) gitStatus(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args gitBaseArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "git status arguments must be a JSON object")
	}
	repo, failed := resolveGitRepository(ctx, call, args.Scope, args.CWD)
	if failed != nil {
		return gitRepositoryFailure(out, args.Scope, failed)
	}
	snapshot, err := projectgit.ReadStatus(ctx, projectgit.Repository{ProjectRoot: repo.ProjectRoot, Root: repo.Root})
	if err != nil {
		return projectGitFailure(out, err, "git_status_failed")
	}
	payload := map[string]any{
		"ok":              true,
		"cwd":             repo.CWD,
		"repoRoot":        repo.Root,
		"head":            snapshot.Head,
		"branch":          snapshot.Branch,
		"upstream":        snapshot.Upstream,
		"detached":        snapshot.Detached,
		"ahead":           snapshot.Ahead,
		"behind":          snapshot.Behind,
		"clean":           len(snapshot.Files) == 0,
		"files":           snapshot.Files,
		"fileCount":       len(snapshot.Files),
		"stagedCount":     snapshot.Staged,
		"unstagedCount":   snapshot.Unstaged,
		"untrackedCount":  snapshot.Untracked,
		"conflictedCount": snapshot.Conflicted,
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryReadFiles, len(snapshot.Files))
}

func (r *BuiltinRunner) gitDiff(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args gitDiffArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "git diff arguments must be a JSON object")
	}
	repo, failed := resolveGitRepository(ctx, call, args.Scope, args.CWD)
	if failed != nil {
		return gitRepositoryFailure(out, args.Scope, failed)
	}
	diff, err := projectgit.ReadPatchDiff(ctx, projectgit.Repository{ProjectRoot: repo.ProjectRoot, Root: repo.Root}, args.Staged, gitDiffOutputLimitBytes)
	if err != nil {
		return projectGitFailure(out, err, "git_diff_failed")
	}
	payload := map[string]any{
		"ok":        true,
		"cwd":       repo.CWD,
		"repoRoot":  repo.Root,
		"staged":    args.Staged,
		"diff":      diff.Patch,
		"truncated": diff.Truncated,
		"files":     diff.Files,
		"fileCount": len(diff.Files),
		"additions": diff.Additions,
		"deletions": diff.Deletions,
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryChangedLines, diff.Additions+diff.Deletions)
}

func projectGitFailure(out Result, err error, fallback string) Result {
	code := projectgit.ErrorCode(err)
	if code == "" {
		code = fallback
	}
	return toolJSONError(out, code, err.Error())
}

func (r *BuiltinRunner) gitLog(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args gitLogArgs
	if len(call.Args) == 0 || json.Unmarshal(call.Args, &args) != nil {
		return toolJSONError(out, "invalid_arguments", "git log arguments must be a JSON object")
	}
	if args.Limit == 0 {
		args.Limit = gitDefaultLogLimit
	}
	if args.Limit < 1 || args.Limit > gitMaxLogLimit {
		return toolJSONError(out, "invalid_arguments", "limit must be between 1 and 100")
	}
	repo, failed := resolveGitRepository(ctx, call, args.Scope, args.CWD)
	if failed != nil {
		return gitRepositoryFailure(out, args.Scope, failed)
	}
	headResult := runGit(ctx, repo.Root, 1024, "rev-parse", "--verify", "--quiet", "HEAD")
	if headResult.err != nil {
		if gitExitCode(headResult.err) == 1 && ctx.Err() == nil {
			payload := map[string]any{"ok": true, "cwd": repo.CWD, "repoRoot": repo.Root, "commits": []gitCommit{}, "count": 0}
			return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, 0)
		}
		return gitExecFailure(out, ctx, headResult, "git_log_failed")
	}
	format := "%H%x00%h%x00%P%x00%an%x00%ae%x00%aI%x00%s"
	logResult := runGit(ctx, repo.Root, gitMetadataLimitBytes, "log", "-z", "--max-count="+strconv.Itoa(args.Limit), "--format="+format)
	if logResult.err != nil {
		return gitExecFailure(out, ctx, logResult, "git_log_failed")
	}
	if logResult.stdout.Truncated() {
		return toolJSONError(out, "git_output_too_large", "git log output exceeded the safety limit")
	}
	commits, err := parseGitLog(logResult.stdout.String())
	if err != nil {
		return toolJSONError(out, "git_parse_failed", err.Error())
	}
	payload := map[string]any{"ok": true, "cwd": repo.CWD, "repoRoot": repo.Root, "commits": commits, "count": len(commits)}
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedItems, len(commits))
}

type gitRepositoryError struct {
	reason string
	detail string
	path   error
}

func resolveGitRepository(ctx context.Context, call Call, scope, cwd string) (gitRepository, *gitRepositoryError) {
	scope = strings.TrimSpace(scope)
	if scope != managedScopeProject {
		return gitRepository{}, &gitRepositoryError{reason: "invalid_scope", detail: "git scope must be project"}
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	authorizedRoot, resolvedCWD, _, err := resolveProjectPath(call.ProjectDirs, cwd, true, false)
	if err != nil {
		return gitRepository{}, &gitRepositoryError{path: err}
	}
	info, err := os.Stat(resolvedCWD)
	if err != nil {
		return gitRepository{}, &gitRepositoryError{reason: "cwd_unavailable", detail: err.Error()}
	}
	if !info.IsDir() {
		return gitRepository{}, &gitRepositoryError{reason: "cwd_not_directory", detail: "git cwd must be a directory"}
	}
	rootResult := runGit(ctx, resolvedCWD, 4096, "rev-parse", "--show-toplevel")
	if rootResult.err != nil {
		return gitRepository{}, &gitRepositoryError{reason: gitFailureReason(ctx, rootResult.err, "not_git_repository"), detail: gitExecDetail(rootResult)}
	}
	if rootResult.stdout.Truncated() {
		return gitRepository{}, &gitRepositoryError{reason: "git_output_too_large", detail: "git repository path exceeded the safety limit"}
	}
	repoRoot := strings.TrimSpace(rootResult.stdout.String())
	resolvedRepoRoot, err := filepath.EvalSymlinks(repoRoot)
	if err != nil {
		return gitRepository{}, &gitRepositoryError{reason: "repository_unavailable", detail: err.Error()}
	}
	resolvedAuthorizedRoot, err := filepath.EvalSymlinks(authorizedRoot)
	if err != nil {
		return gitRepository{}, &gitRepositoryError{reason: "project_root_unavailable", detail: err.Error()}
	}
	if !pathInsideRoot(resolvedRepoRoot, resolvedAuthorizedRoot) {
		return gitRepository{}, &gitRepositoryError{reason: "repository_outside_project", detail: "git repository root is outside the authorized project directory"}
	}
	return gitRepository{CWD: resolvedCWD, Root: resolvedRepoRoot, ProjectRoot: resolvedAuthorizedRoot}, nil
}

func gitRepositoryFailure(out Result, scope string, failed *gitRepositoryError) Result {
	if failed.path != nil {
		return filePathError(out, scope, failed.path)
	}
	return toolJSONError(out, failed.reason, failed.detail)
}

func runGit(ctx context.Context, dir string, stdoutLimit int, args ...string) gitExecResult {
	return runGitInput(ctx, dir, stdoutLimit, nil, args...)
}

func runGitInput(ctx context.Context, dir string, stdoutLimit int, input []byte, args ...string) gitExecResult {
	gitArgs := []string{"--no-pager", "-c", "core.fsmonitor=false", "-c", "color.ui=false"}
	gitArgs = append(gitArgs, args...)
	stdout := newTruncatingBuffer(stdoutLimit)
	stderr := newTruncatingBuffer(gitStderrLimitBytes)
	env, err := commandEnvironment(map[string]string{
		"GIT_ATTR_NOSYSTEM":   "1",
		"GIT_OPTIONAL_LOCKS":  "0",
		"GIT_PAGER":           "cat",
		"GIT_TERMINAL_PROMPT": "0",
		"LANG":                "C",
		"LC_ALL":              "C",
		"NO_COLOR":            "1",
		"PAGER":               "cat",
	})
	if err != nil {
		return gitExecResult{stdout: stdout, stderr: stderr, err: err}
	}
	executable, err := resolveExecutableFromEnv("git", dir, env)
	if err != nil {
		return gitExecResult{stdout: stdout, stderr: stderr, err: err}
	}
	cmd := exec.CommandContext(ctx, executable, gitArgs...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if input != nil {
		cmd.Stdin = bytes.NewReader(input)
	}
	return gitExecResult{stdout: stdout, stderr: stderr, err: cmd.Run()}
}

func runGitWithoutExternalFilters(ctx context.Context, dir string, stdoutLimit int, args ...string) gitExecResult {
	configResult := runGit(ctx, dir, gitMetadataLimitBytes, "config", "--name-only", "--get-regexp", `^filter\..*\.(clean|process|required)$`)
	if configResult.err != nil && gitExitCode(configResult.err) != 1 {
		return configResult
	}
	if configResult.stdout.Truncated() {
		configResult.err = errors.New("Git filter configuration exceeded the safety limit")
		return configResult
	}
	drivers := make(map[string]bool)
	for _, key := range strings.Fields(configResult.stdout.String()) {
		for _, suffix := range []string{".clean", ".process", ".required"} {
			if strings.HasPrefix(key, "filter.") && strings.HasSuffix(key, suffix) {
				driver := strings.TrimSuffix(strings.TrimPrefix(key, "filter."), suffix)
				if driver != "" {
					drivers[driver] = true
				}
			}
		}
	}
	names := make([]string, 0, len(drivers))
	for driver := range drivers {
		names = append(names, driver)
	}
	sort.Strings(names)
	safeArgs := make([]string, 0, len(names)*6+len(args))
	for _, driver := range names {
		safeArgs = append(safeArgs,
			"-c", "filter."+driver+".clean=cat",
			"-c", "filter."+driver+".process=",
			"-c", "filter."+driver+".required=false",
		)
	}
	safeArgs = append(safeArgs, args...)
	return runGit(ctx, dir, stdoutLimit, safeArgs...)
}

func gitExecFailure(out Result, ctx context.Context, result gitExecResult, fallback string) Result {
	return toolJSONError(out, gitFailureReason(ctx, result.err, fallback), gitExecDetail(result))
}

func gitFailureReason(ctx context.Context, err error, fallback string) string {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timed_out"
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, exec.ErrNotFound) {
		return "git_unavailable"
	}
	return fallback
}

func gitExecDetail(result gitExecResult) string {
	if detail := strings.TrimSpace(result.stderr.String()); detail != "" {
		return detail
	}
	if result.err != nil {
		return result.err.Error()
	}
	return "git command failed"
}

func gitExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

func parseGitLog(raw string) ([]gitCommit, error) {
	if raw == "" {
		return []gitCommit{}, nil
	}
	fields := strings.Split(raw, "\x00")
	if fields[len(fields)-1] == "" {
		fields = fields[:len(fields)-1]
	}
	const fieldsPerCommit = 7
	if len(fields)%fieldsPerCommit != 0 {
		return nil, errors.New("malformed git log output")
	}
	commits := make([]gitCommit, 0, len(fields)/fieldsPerCommit)
	for i := 0; i < len(fields); i += fieldsPerCommit {
		commits = append(commits, gitCommit{
			Hash:        fields[i],
			ShortHash:   fields[i+1],
			Parents:     strings.Fields(fields[i+2]),
			AuthorName:  fields[i+3],
			AuthorEmail: fields[i+4],
			AuthoredAt:  fields[i+5],
			Subject:     fields[i+6],
		})
	}
	return commits, nil
}

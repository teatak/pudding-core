package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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

type gitStatusFile struct {
	Path           string `json:"path"`
	OriginalPath   string `json:"originalPath,omitempty"`
	Kind           string `json:"kind"`
	IndexStatus    string `json:"indexStatus"`
	WorktreeStatus string `json:"worktreeStatus"`
}

type gitStatusSnapshot struct {
	Head       string
	Branch     string
	Upstream   string
	Detached   bool
	Ahead      int
	Behind     int
	Files      []gitStatusFile
	Staged     int
	Unstaged   int
	Untracked  int
	Conflicted int
}

type gitDiffFile struct {
	Path         string `json:"path"`
	OriginalPath string `json:"originalPath,omitempty"`
	Additions    int    `json:"additions"`
	Deletions    int    `json:"deletions"`
	Binary       bool   `json:"binary,omitempty"`
}

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
	result := runGitWithoutExternalFilters(ctx, repo.Root, gitMetadataLimitBytes, "status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if result.err != nil {
		return gitExecFailure(out, ctx, result, "git_status_failed")
	}
	if result.stdout.Truncated() {
		return toolJSONError(out, "git_output_too_large", "git status output exceeded the safety limit")
	}
	snapshot, err := parseGitStatus(result.stdout.String())
	if err != nil {
		return toolJSONError(out, "git_parse_failed", err.Error())
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
	baseArgs := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames=50%"}
	if args.Staged {
		baseArgs = append(baseArgs, "--cached")
	}
	numstatArgs := append(append([]string(nil), baseArgs...), "--numstat", "-z")
	statsResult := runGitWithoutExternalFilters(ctx, repo.Root, gitMetadataLimitBytes, numstatArgs...)
	if statsResult.err != nil {
		return gitExecFailure(out, ctx, statsResult, "git_diff_failed")
	}
	if statsResult.stdout.Truncated() {
		return toolJSONError(out, "git_output_too_large", "git diff statistics exceeded the safety limit")
	}
	files, additions, deletions, err := parseGitNumstat(statsResult.stdout.String())
	if err != nil {
		return toolJSONError(out, "git_parse_failed", err.Error())
	}
	patchArgs := append(append([]string(nil), baseArgs...), "--src-prefix=a/", "--dst-prefix=b/")
	patchResult := runGitWithoutExternalFilters(ctx, repo.Root, gitDiffOutputLimitBytes, patchArgs...)
	if patchResult.err != nil {
		return gitExecFailure(out, ctx, patchResult, "git_diff_failed")
	}
	payload := map[string]any{
		"ok":        true,
		"cwd":       repo.CWD,
		"repoRoot":  repo.Root,
		"staged":    args.Staged,
		"diff":      patchResult.stdout.String(),
		"truncated": patchResult.stdout.Truncated(),
		"files":     files,
		"fileCount": len(files),
		"additions": additions,
		"deletions": deletions,
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryChangedLines, additions+deletions)
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
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Dir = dir
	cmd.Env, _ = commandEnvironment(map[string]string{
		"GIT_ATTR_NOSYSTEM":   "1",
		"GIT_OPTIONAL_LOCKS":  "0",
		"GIT_PAGER":           "cat",
		"GIT_TERMINAL_PROMPT": "0",
		"LANG":                "C",
		"LC_ALL":              "C",
		"NO_COLOR":            "1",
		"PAGER":               "cat",
	})
	stdout := newTruncatingBuffer(stdoutLimit)
	stderr := newTruncatingBuffer(gitStderrLimitBytes)
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

func parseGitStatus(raw string) (gitStatusSnapshot, error) {
	var snapshot gitStatusSnapshot
	records := strings.Split(raw, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		switch {
		case strings.HasPrefix(record, "# branch.oid "):
			oid := strings.TrimPrefix(record, "# branch.oid ")
			if oid != "(initial)" {
				snapshot.Head = oid
			}
		case strings.HasPrefix(record, "# branch.head "):
			head := strings.TrimPrefix(record, "# branch.head ")
			if head == "(detached)" {
				snapshot.Detached = true
			} else {
				snapshot.Branch = head
			}
		case strings.HasPrefix(record, "# branch.upstream "):
			snapshot.Upstream = strings.TrimPrefix(record, "# branch.upstream ")
		case strings.HasPrefix(record, "# branch.ab "):
			if _, err := fmt.Sscanf(strings.TrimPrefix(record, "# branch.ab "), "+%d -%d", &snapshot.Ahead, &snapshot.Behind); err != nil {
				return snapshot, fmt.Errorf("parse branch divergence: %w", err)
			}
		case strings.HasPrefix(record, "1 "):
			fields := strings.SplitN(record, " ", 9)
			if len(fields) != 9 || len(fields[1]) != 2 {
				return snapshot, errors.New("malformed ordinary git status entry")
			}
			snapshot.Files = append(snapshot.Files, newGitStatusFile(fields[8], "", fields[1], false))
		case strings.HasPrefix(record, "2 "):
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || len(fields[1]) != 2 || i+1 >= len(records) {
				return snapshot, errors.New("malformed renamed git status entry")
			}
			i++
			snapshot.Files = append(snapshot.Files, newGitStatusFile(fields[9], records[i], fields[1], false))
		case strings.HasPrefix(record, "u "):
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 || len(fields[1]) != 2 {
				return snapshot, errors.New("malformed conflicted git status entry")
			}
			snapshot.Files = append(snapshot.Files, newGitStatusFile(fields[10], "", fields[1], true))
		case strings.HasPrefix(record, "? "):
			snapshot.Files = append(snapshot.Files, newGitStatusFile(strings.TrimPrefix(record, "? "), "", "??", false))
		case strings.HasPrefix(record, "! "):
			continue
		default:
			return snapshot, errors.New("unknown git status entry")
		}
	}
	for _, file := range snapshot.Files {
		switch file.Kind {
		case "untracked":
			snapshot.Untracked++
		case "conflicted":
			snapshot.Conflicted++
		default:
			if file.IndexStatus != "." {
				snapshot.Staged++
			}
			if file.WorktreeStatus != "." {
				snapshot.Unstaged++
			}
		}
	}
	return snapshot, nil
}

func newGitStatusFile(path, originalPath, status string, conflicted bool) gitStatusFile {
	indexStatus := status[:1]
	worktreeStatus := status[1:]
	kind := gitStatusKind(indexStatus, worktreeStatus, conflicted)
	return gitStatusFile{
		Path:           strings.ToValidUTF8(path, "\uFFFD"),
		OriginalPath:   strings.ToValidUTF8(originalPath, "\uFFFD"),
		Kind:           kind,
		IndexStatus:    indexStatus,
		WorktreeStatus: worktreeStatus,
	}
}

func gitStatusKind(indexStatus, worktreeStatus string, conflicted bool) string {
	if conflicted || strings.Contains(indexStatus+worktreeStatus, "U") {
		return "conflicted"
	}
	if indexStatus == "?" {
		return "untracked"
	}
	for _, candidate := range []struct {
		code string
		kind string
	}{{"R", "renamed"}, {"C", "copied"}, {"D", "deleted"}, {"A", "added"}, {"T", "type_changed"}, {"M", "modified"}} {
		if indexStatus == candidate.code || worktreeStatus == candidate.code {
			return candidate.kind
		}
	}
	return "changed"
}

func parseGitNumstat(raw string) ([]gitDiffFile, int, int, error) {
	records := strings.Split(raw, "\x00")
	files := make([]gitDiffFile, 0, len(records))
	totalAdditions := 0
	totalDeletions := 0
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\t", 3)
		if len(fields) != 3 {
			return nil, 0, 0, errors.New("malformed git numstat entry")
		}
		path := fields[2]
		originalPath := ""
		if path == "" {
			if i+2 >= len(records) {
				return nil, 0, 0, errors.New("malformed renamed git numstat entry")
			}
			originalPath = records[i+1]
			path = records[i+2]
			i += 2
		}
		binary := fields[0] == "-" || fields[1] == "-"
		additions := 0
		deletions := 0
		var err error
		if !binary {
			additions, err = strconv.Atoi(fields[0])
			if err != nil {
				return nil, 0, 0, errors.New("invalid git numstat additions")
			}
			deletions, err = strconv.Atoi(fields[1])
			if err != nil {
				return nil, 0, 0, errors.New("invalid git numstat deletions")
			}
		}
		totalAdditions += additions
		totalDeletions += deletions
		files = append(files, gitDiffFile{
			Path:         strings.ToValidUTF8(path, "\uFFFD"),
			OriginalPath: strings.ToValidUTF8(originalPath, "\uFFFD"),
			Additions:    additions,
			Deletions:    deletions,
			Binary:       binary,
		})
	}
	return files, totalAdditions, totalDeletions, nil
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

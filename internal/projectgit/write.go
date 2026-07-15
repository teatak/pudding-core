package projectgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/projectpath"
)

const (
	writeMaxPaths         = 512
	commitMaxMessageBytes = 16 << 10
)

var repositoryLocks sync.Map

// Initialize creates Git metadata only at the authorized project root.
func Initialize(ctx context.Context, projectRoot string) (Repository, error) {
	projectRoot = filepath.Clean(strings.TrimSpace(projectRoot))
	resolvedRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return Repository{}, newError(CodeInitFailed, "Project root is unavailable", err)
	}
	info, err := os.Stat(resolvedRoot)
	if err != nil || !info.IsDir() {
		return Repository{}, newError(CodeInitFailed, "Project root is not a directory", err)
	}
	var repo Repository
	err = withRepositoryLock(resolvedRoot, func() error {
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		result := run(commandCtx, resolvedRoot, metadataLimitBytes, "init", "--quiet", "--template=")
		if result.err != nil {
			return commandError(commandCtx, CodeInitFailed, result)
		}
		var discoverErr error
		repo, discoverErr = Discover(ctx, resolvedRoot)
		return discoverErr
	})
	return repo, err
}

// Stage adds only explicit repository-relative files to the index.
func Stage(ctx context.Context, repo Repository, rawPaths []string) (Status, error) {
	paths, err := normalizeWritePaths(repo, rawPaths)
	if err != nil {
		return Status{}, err
	}
	err = withRepositoryLock(repo.Root, func() error {
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		args := []string{"--literal-pathspecs", "add", "--"}
		args = append(args, paths...)
		result := runWithoutExternalFilters(commandCtx, repo.Root, metadataLimitBytes, args...)
		if result.err != nil {
			return commandError(commandCtx, CodeStageFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

// Unstage removes only explicit files from the index and leaves the worktree unchanged.
func Unstage(ctx context.Context, repo Repository, rawPaths []string) (Status, error) {
	paths, err := normalizeWritePaths(repo, rawPaths)
	if err != nil {
		return Status{}, err
	}
	err = withRepositoryLock(repo.Root, func() error {
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		hasHead, err := repositoryHasHead(commandCtx, repo.Root)
		if err != nil {
			return err
		}
		args := []string{"--literal-pathspecs"}
		if hasHead {
			args = append(args, "restore", "--staged", "--")
		} else {
			args = append(args, "rm", "--cached", "-r", "--ignore-unmatch", "--")
		}
		args = append(args, paths...)
		result := run(commandCtx, repo.Root, metadataLimitBytes, args...)
		if result.err != nil {
			return commandError(commandCtx, CodeUnstageFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

// Discard restores tracked worktree files from the index and removes explicit untracked files.
func Discard(ctx context.Context, repo Repository, rawPaths []string) (Status, error) {
	paths, err := normalizeWritePaths(repo, rawPaths)
	if err != nil {
		return Status{}, err
	}
	err = withRepositoryLock(repo.Root, func() error {
		status, err := ReadStatus(ctx, repo)
		if err != nil {
			return err
		}
		byPath := make(map[string]StatusFile, len(status.Files))
		for _, file := range status.Files {
			byPath[file.Path] = file
		}
		tracked := make([]string, 0, len(paths))
		untracked := make([]string, 0, len(paths))
		for _, path := range paths {
			file, ok := byPath[path]
			if !ok || (file.WorktreeStatus == "." && file.Kind != "untracked" && file.Kind != "conflicted") {
				return newError(CodeDiscardFailed, "File has no worktree changes: "+path, nil)
			}
			if file.Kind == "conflicted" {
				return newError(CodeConflicts, "Resolve Git conflicts before discarding files", nil)
			}
			if file.Kind == "untracked" {
				untracked = append(untracked, path)
			} else {
				tracked = append(tracked, path)
			}
		}
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		if len(tracked) > 0 {
			args := []string{"--literal-pathspecs", "restore", "--worktree", "--"}
			args = append(args, tracked...)
			result := runWithoutExternalFilters(commandCtx, repo.Root, metadataLimitBytes, args...)
			if result.err != nil {
				return commandError(commandCtx, CodeDiscardFailed, result)
			}
		}
		for _, path := range untracked {
			target := filepath.Join(repo.Root, filepath.FromSlash(path))
			info, err := os.Lstat(target)
			if err != nil {
				return newError(CodeDiscardFailed, "Unable to inspect untracked file: "+path, err)
			}
			if info.IsDir() {
				return newError(CodeDiscardFailed, "Discard accepts explicit files only: "+path, nil)
			}
			if err := os.Remove(target); err != nil {
				return newError(CodeDiscardFailed, "Unable to remove untracked file: "+path, err)
			}
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

// Commit creates a normal commit from the current index with hooks and signing disabled.
func Commit(ctx context.Context, repo Repository, message string) (Status, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		return Status{}, newError(CodeCommitMessageRequired, "Commit message is required", nil)
	}
	if !utf8.ValidString(message) || len(message) > commitMaxMessageBytes || strings.ContainsRune(message, '\x00') {
		return Status{}, newError(CodeCommitMessageRequired, "Commit message is invalid", nil)
	}
	err := withRepositoryLock(repo.Root, func() error {
		status, err := ReadStatus(ctx, repo)
		if err != nil {
			return err
		}
		if status.Conflicted > 0 {
			return newError(CodeConflicts, "Resolve Git conflicts before committing", nil)
		}
		if status.Staged == 0 {
			return newError(CodeNoStagedChanges, "There are no staged changes to commit", nil)
		}
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		result := run(commandCtx, repo.Root, metadataLimitBytes,
			"-c", "core.hooksPath="+os.DevNull,
			"-c", "commit.gpgSign=false",
			"-c", "gc.auto=0",
			"commit", "--no-verify", "--no-gpg-sign", "--cleanup=verbatim", "-m", message,
		)
		if result.err != nil {
			return commandError(commandCtx, CodeCommitFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

func normalizeWritePaths(repo Repository, rawPaths []string) ([]string, error) {
	if len(rawPaths) == 0 || len(rawPaths) > writeMaxPaths {
		return nil, newError(CodeInvalidPath, "Git writes require between 1 and 512 explicit files", nil)
	}
	seen := make(map[string]bool, len(rawPaths))
	paths := make([]string, 0, len(rawPaths))
	for _, raw := range rawPaths {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.ContainsRune(raw, '\x00') || filepath.IsAbs(filepath.FromSlash(raw)) {
			return nil, newError(CodeInvalidPath, "Git write path is invalid", nil)
		}
		candidate := filepath.Clean(filepath.Join(repo.Root, filepath.FromSlash(raw)))
		if !projectpath.Inside(candidate, repo.Root) {
			return nil, newError(CodeInvalidPath, "Git write path is outside the repository: "+raw, nil)
		}
		rel, err := filepath.Rel(repo.Root, candidate)
		if err != nil || rel == "." {
			return nil, newError(CodeInvalidPath, "Git writes require explicit file paths", err)
		}
		rel = filepath.ToSlash(rel)
		if strings.EqualFold(strings.Split(rel, "/")[0], ".git") {
			return nil, newError(CodeInvalidPath, "Git metadata paths are not allowed", nil)
		}
		resolvedParent, err := projectpath.ResolveExistingParent(candidate)
		if err != nil || !projectpath.Inside(resolvedParent, repo.Root) {
			return nil, newError(CodeInvalidPath, "Git write path resolves outside the repository: "+raw, err)
		}
		if info, err := os.Lstat(candidate); err == nil {
			if info.IsDir() || (!info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0) {
				return nil, newError(CodeInvalidPath, "Git writes accept regular files and symlinks only: "+raw, nil)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, newError(CodeInvalidPath, "Unable to inspect Git write path: "+raw, err)
		}
		if !seen[rel] {
			seen[rel] = true
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func repositoryHasHead(ctx context.Context, root string) (bool, error) {
	result := run(ctx, root, 64, "rev-parse", "--verify", "HEAD")
	if result.err == nil {
		return true, nil
	}
	if exitCode(result.err) == 128 || exitCode(result.err) == 1 {
		return false, nil
	}
	return false, commandError(ctx, CodeUnstageFailed, result)
}

func withRepositoryLock(root string, operation func() error) error {
	value, _ := repositoryLocks.LoadOrStore(filepath.Clean(root), &sync.Mutex{})
	lock := value.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return operation()
}

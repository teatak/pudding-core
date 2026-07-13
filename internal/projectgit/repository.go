package projectgit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/teatak/pudding-core/internal/projectpath"
)

const commandTimeout = 10 * time.Second

func Discover(ctx context.Context, projectRoot string) (Repository, error) {
	projectRoot = filepath.Clean(strings.TrimSpace(projectRoot))
	resolvedProjectRoot, err := filepath.EvalSymlinks(projectRoot)
	if err != nil {
		return Repository{}, newError(CodeNotRepository, "Project root is unavailable", err)
	}
	info, err := os.Stat(resolvedProjectRoot)
	if err != nil || !info.IsDir() {
		return Repository{}, newError(CodeNotRepository, "Project root is not a directory", err)
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	result := run(commandCtx, resolvedProjectRoot, 4096, "rev-parse", "--show-toplevel")
	if result.err != nil {
		if exitCode(result.err) == 128 && strings.Contains(strings.ToLower(result.stderr), "not a git repository") {
			return Repository{}, newError(CodeNotRepository, "Project root is not a Git repository", result.err)
		}
		return Repository{}, commandError(commandCtx, CodeNotRepository, result)
	}
	if result.truncated {
		return Repository{}, newError(CodeOutputTooLarge, "Git repository path exceeded the safety limit", nil)
	}
	repoRoot, err := filepath.EvalSymlinks(strings.TrimSpace(result.stdout))
	if err != nil {
		return Repository{}, newError(CodeNotRepository, "Git repository root is unavailable", err)
	}
	if !projectpath.Inside(repoRoot, resolvedProjectRoot) {
		return Repository{}, newError(CodeRepositoryOutsideRoot, "Git repository root is outside the authorized project directory", nil)
	}
	return Repository{ProjectRoot: resolvedProjectRoot, Root: repoRoot}, nil
}

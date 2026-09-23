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
		return Repository{}, newError(CodeDiscoveryFailed, "", err)
	}
	info, err := os.Stat(resolvedProjectRoot)
	if err != nil {
		return Repository{}, newError(CodeDiscoveryFailed, "", err)
	}
	if !info.IsDir() {
		return Repository{}, newError(CodeDiscoveryFailed, "Project root is not a directory", nil)
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	result := run(commandCtx, resolvedProjectRoot, 4096, "rev-parse", "--show-toplevel")
	if result.err != nil {
		return Repository{}, DiscoveryError(commandCtx, result.err, result.stderr)
	}
	if result.truncated {
		return Repository{}, newError(CodeOutputTooLarge, "Git repository path exceeded the safety limit", nil)
	}
	repoRoot, err := filepath.EvalSymlinks(strings.TrimSpace(result.stdout))
	if err != nil {
		return Repository{}, newError(CodeDiscoveryFailed, "", err)
	}
	if !projectpath.Inside(repoRoot, resolvedProjectRoot) {
		return Repository{}, newError(CodeRepositoryOutsideRoot, "Git repository root is outside the authorized project directory", nil)
	}
	return Repository{ProjectRoot: resolvedProjectRoot, Root: repoRoot}, nil
}

// DiscoveryError distinguishes an absent repository from a failed Git probe.
// Both project APIs and agent tools use this classification.
func DiscoveryError(ctx context.Context, err error, stderr string) error {
	if err == nil {
		return nil
	}
	code := CodeDiscoveryFailed
	if ctx.Err() == nil && exitCode(err) == 128 {
		for _, line := range strings.Split(strings.ToLower(stderr), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "fatal:") {
				continue
			}
			if suffix, ok := strings.CutPrefix(line, "fatal: not a git repository"); ok &&
				(suffix == "" || suffix[0] == ':' || suffix[0] == ' ' || suffix[0] == '\t') {
				code = CodeNotRepository
			}
			break
		}
	}
	return commandError(ctx, code, execResult{err: err, stderr: stderr})
}

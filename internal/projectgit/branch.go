package projectgit

import (
	"context"
	"os"
	"sort"
	"strings"
)

const branchMaxNameBytes = 1024

func ListBranches(ctx context.Context, repo Repository) ([]Branch, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	locals, err := listBranchRefs(commandCtx, repo.Root, false)
	if err != nil {
		return nil, err
	}
	remotes, err := listBranchRefs(commandCtx, repo.Root, true)
	if err != nil {
		return nil, err
	}
	branches := append(locals, remotes...)
	sort.SliceStable(branches, func(i, j int) bool {
		if branches[i].Remote != branches[j].Remote {
			return !branches[i].Remote
		}
		return branches[i].Name < branches[j].Name
	})
	return branches, nil
}

func CreateBranch(ctx context.Context, repo Repository, rawName string) (Status, error) {
	name, err := validateBranchName(ctx, repo.Root, rawName)
	if err != nil {
		return Status{}, err
	}
	err = withRepositoryLock(repo.Root, func() error {
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		result := runWithoutExternalFilters(commandCtx, repo.Root, metadataLimitBytes,
			"-c", "core.hooksPath="+os.DevNull,
			"switch", "-c", name,
		)
		if result.err != nil {
			return commandError(commandCtx, CodeBranchCreateFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

func SwitchBranch(ctx context.Context, repo Repository, name string) (Status, error) {
	branches, err := ListBranches(ctx, repo)
	if err != nil {
		return Status{}, err
	}
	var selected *Branch
	for index := range branches {
		if branches[index].Name == name {
			selected = &branches[index]
			break
		}
	}
	if selected == nil {
		return Status{}, newError(CodeInvalidBranch, "Git branch is unavailable", nil)
	}
	if selected.Current {
		return ReadStatus(ctx, repo)
	}
	err = withRepositoryLock(repo.Root, func() error {
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		args := []string{"-c", "core.hooksPath=" + os.DevNull, "switch"}
		if selected.Remote {
			args = append(args, "--track")
		}
		args = append(args, "--", selected.Name)
		result := runWithoutExternalFilters(commandCtx, repo.Root, metadataLimitBytes, args...)
		if result.err != nil {
			return commandError(commandCtx, CodeBranchSwitchFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

func RenameCurrentBranch(ctx context.Context, repo Repository, rawName string) (Status, error) {
	name, err := validateBranchName(ctx, repo.Root, rawName)
	if err != nil {
		return Status{}, err
	}
	err = withRepositoryLock(repo.Root, func() error {
		status, err := ReadStatus(ctx, repo)
		if err != nil {
			return err
		}
		if status.Detached || status.Branch == "" {
			return newError(CodeBranchRenameFailed, "A detached HEAD cannot be renamed", nil)
		}
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		result := run(commandCtx, repo.Root, metadataLimitBytes, "branch", "-m", name)
		if result.err != nil {
			return commandError(commandCtx, CodeBranchRenameFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

func DeleteBranch(ctx context.Context, repo Repository, name string) (Status, error) {
	branches, err := ListBranches(ctx, repo)
	if err != nil {
		return Status{}, err
	}
	valid := false
	for _, branch := range branches {
		if branch.Name == name && !branch.Remote && !branch.Current {
			valid = true
			break
		}
	}
	if !valid {
		return Status{}, newError(CodeInvalidBranch, "Only an inactive local branch can be deleted", nil)
	}
	err = withRepositoryLock(repo.Root, func() error {
		commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
		defer cancel()
		result := run(commandCtx, repo.Root, metadataLimitBytes, "branch", "-d", "--", name)
		if result.err != nil {
			return commandError(commandCtx, CodeBranchDeleteFailed, result)
		}
		return nil
	})
	if err != nil {
		return Status{}, err
	}
	return ReadStatus(ctx, repo)
}

func listBranchRefs(ctx context.Context, root string, remote bool) ([]Branch, error) {
	format := "%(refname:short)%00%(upstream:short)%00%(HEAD)%00%(symref)"
	prefix := "refs/heads"
	if remote {
		prefix = "refs/remotes"
	}
	result := run(ctx, root, metadataLimitBytes, "for-each-ref", "--format="+format, prefix)
	if result.err != nil {
		return nil, commandError(ctx, CodeBranchListFailed, result)
	}
	if result.truncated {
		return nil, newError(CodeOutputTooLarge, "Git branch output exceeded the safety limit", nil)
	}
	branches := make([]Branch, 0)
	for _, line := range strings.Split(result.stdout, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\x00")
		if len(fields) != 4 {
			return nil, newError(CodeBranchListFailed, "Unable to parse Git branches", nil)
		}
		if remote && fields[3] != "" {
			continue
		}
		branches = append(branches, Branch{
			Name: fields[0], Upstream: fields[1], Current: fields[2] == "*", Remote: remote,
		})
	}
	return branches, nil
}

func validateBranchName(ctx context.Context, root, rawName string) (string, error) {
	name := strings.TrimSpace(rawName)
	if name == "" || len(name) > branchMaxNameBytes || strings.ContainsRune(name, '\x00') {
		return "", newError(CodeInvalidBranch, "Git branch name is invalid", nil)
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	result := run(commandCtx, root, metadataLimitBytes, "check-ref-format", "--branch", name)
	if result.err != nil {
		return "", newError(CodeInvalidBranch, "Git branch name is invalid", result.err)
	}
	return name, nil
}

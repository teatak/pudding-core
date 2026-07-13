package projectgit

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

func ReadStatus(ctx context.Context, repo Repository) (Status, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	result := runWithoutExternalFilters(commandCtx, repo.Root, metadataLimitBytes,
		"status", "--porcelain=v2", "--branch", "-z", "--untracked-files=all")
	if result.err != nil {
		return Status{}, commandError(commandCtx, CodeStatusFailed, result)
	}
	if result.truncated {
		return Status{}, newError(CodeOutputTooLarge, "Git status output exceeded the safety limit", nil)
	}
	status, err := ParseStatus(result.stdout)
	if err != nil {
		return Status{}, newError(CodeParseFailed, err.Error(), err)
	}
	return status, nil
}

func ParseStatus(raw string) (Status, error) {
	var snapshot Status
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
				return snapshot, errors.New("malformed ordinary Git status entry")
			}
			snapshot.Files = append(snapshot.Files, newStatusFile(fields[8], "", fields[1], false))
		case strings.HasPrefix(record, "2 "):
			fields := strings.SplitN(record, " ", 10)
			if len(fields) != 10 || len(fields[1]) != 2 || i+1 >= len(records) {
				return snapshot, errors.New("malformed renamed Git status entry")
			}
			i++
			snapshot.Files = append(snapshot.Files, newStatusFile(fields[9], records[i], fields[1], false))
		case strings.HasPrefix(record, "u "):
			fields := strings.SplitN(record, " ", 11)
			if len(fields) != 11 || len(fields[1]) != 2 {
				return snapshot, errors.New("malformed conflicted Git status entry")
			}
			snapshot.Files = append(snapshot.Files, newStatusFile(fields[10], "", fields[1], true))
		case strings.HasPrefix(record, "? "):
			snapshot.Files = append(snapshot.Files, newStatusFile(strings.TrimPrefix(record, "? "), "", "??", false))
		case strings.HasPrefix(record, "! "):
			continue
		default:
			return snapshot, errors.New("unknown Git status entry")
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

func newStatusFile(path, originalPath, status string, conflicted bool) StatusFile {
	indexStatus := status[:1]
	worktreeStatus := status[1:]
	return StatusFile{
		Path:           strings.ToValidUTF8(path, "�"),
		OriginalPath:   strings.ToValidUTF8(originalPath, "�"),
		Kind:           statusKind(indexStatus, worktreeStatus, conflicted),
		IndexStatus:    indexStatus,
		WorktreeStatus: worktreeStatus,
	}
}

func statusKind(indexStatus, worktreeStatus string, conflicted bool) string {
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

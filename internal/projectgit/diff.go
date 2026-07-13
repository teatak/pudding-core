package projectgit

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/projectpath"
)

func ReadPatchDiff(ctx context.Context, repo Repository, staged bool, patchLimit int) (PatchDiff, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	baseArgs := []string{"diff", "--no-ext-diff", "--no-textconv", "--no-color", "--find-renames=50%"}
	if staged {
		baseArgs = append(baseArgs, "--cached")
	}
	numstatArgs := append(append([]string(nil), baseArgs...), "--numstat", "-z")
	stats := runWithoutExternalFilters(commandCtx, repo.Root, metadataLimitBytes, numstatArgs...)
	if stats.err != nil {
		return PatchDiff{}, commandError(commandCtx, CodeDiffFailed, stats)
	}
	if stats.truncated {
		return PatchDiff{}, newError(CodeOutputTooLarge, "Git diff statistics exceeded the safety limit", nil)
	}
	files, additions, deletions, err := ParseNumstat(stats.stdout)
	if err != nil {
		return PatchDiff{}, newError(CodeParseFailed, err.Error(), err)
	}
	patchArgs := append(append([]string(nil), baseArgs...), "--src-prefix=a/", "--dst-prefix=b/")
	patch := runWithoutExternalFilters(commandCtx, repo.Root, patchLimit, patchArgs...)
	if patch.err != nil {
		return PatchDiff{}, commandError(commandCtx, CodeDiffFailed, patch)
	}
	return PatchDiff{Patch: patch.stdout, Truncated: patch.truncated, Files: files, Additions: additions, Deletions: deletions}, nil
}

func ParseNumstat(raw string) ([]DiffFile, int, int, error) {
	records := strings.Split(raw, "\x00")
	files := make([]DiffFile, 0, len(records))
	totalAdditions := 0
	totalDeletions := 0
	for i := 0; i < len(records); i++ {
		record := records[i]
		if record == "" {
			continue
		}
		fields := strings.SplitN(record, "\t", 3)
		if len(fields) != 3 {
			return nil, 0, 0, errors.New("malformed Git numstat entry")
		}
		path := fields[2]
		originalPath := ""
		if path == "" {
			if i+2 >= len(records) {
				return nil, 0, 0, errors.New("malformed renamed Git numstat entry")
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
				return nil, 0, 0, errors.New("invalid Git numstat additions")
			}
			deletions, err = strconv.Atoi(fields[1])
			if err != nil {
				return nil, 0, 0, errors.New("invalid Git numstat deletions")
			}
		}
		totalAdditions += additions
		totalDeletions += deletions
		files = append(files, DiffFile{
			Path: strings.ToValidUTF8(path, "�"), OriginalPath: strings.ToValidUTF8(originalPath, "�"),
			Additions: additions, Deletions: deletions, Binary: binary,
		})
	}
	return files, totalAdditions, totalDeletions, nil
}

func ReadFileDiff(ctx context.Context, repo Repository, rawPath string, staged bool, contentLimit int) (FileDiff, error) {
	path, err := cleanGitPath(repo.Root, rawPath)
	if err != nil {
		return FileDiff{}, err
	}
	status, err := ReadStatus(ctx, repo)
	if err != nil {
		return FileDiff{}, err
	}
	var file *StatusFile
	for i := range status.Files {
		if status.Files[i].Path == path {
			file = &status.Files[i]
			break
		}
	}
	if file == nil {
		return FileDiff{}, newError(CodeInvalidPath, "Git change is no longer available", nil)
	}
	diff := FileDiff{Path: file.Path, OriginalPath: file.OriginalPath, Staged: staged}
	var oldData, newData fileData
	if staged {
		if file.IndexStatus == "." || file.IndexStatus == "?" {
			return FileDiff{}, newError(CodeInvalidPath, "File has no staged changes", nil)
		}
		oldPath := file.Path
		if file.OriginalPath != "" {
			oldPath = file.OriginalPath
		}
		oldData, err = readGitObject(ctx, repo.Root, "HEAD:"+oldPath, contentLimit, file.IndexStatus == "A")
		if err == nil {
			newData, err = readGitObject(ctx, repo.Root, ":"+file.Path, contentLimit, file.IndexStatus == "D")
		}
	} else {
		if file.Kind == "untracked" {
			oldData = fileData{}
		} else if file.Kind == "conflicted" {
			oldData, err = readGitObject(ctx, repo.Root, "HEAD:"+file.Path, contentLimit, true)
		} else {
			if file.WorktreeStatus == "." {
				return FileDiff{}, newError(CodeInvalidPath, "File has no unstaged changes", nil)
			}
			oldData, err = readGitObject(ctx, repo.Root, ":"+file.Path, contentLimit, false)
		}
		if err == nil {
			newData, err = readWorktreeFile(repo.Root, file.Path, contentLimit, file.WorktreeStatus == "D")
		}
	}
	if err != nil {
		return FileDiff{}, err
	}
	diff.TooLarge = oldData.tooLarge || newData.tooLarge
	diff.Binary = oldData.binary || newData.binary
	if !diff.TooLarge && !diff.Binary {
		diff.OldContent = string(oldData.content)
		diff.NewContent = string(newData.content)
	}
	return diff, nil
}

type fileData struct {
	content  []byte
	binary   bool
	tooLarge bool
}

func readGitObject(ctx context.Context, root, spec string, limit int, allowMissing bool) (fileData, error) {
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	result := runWithoutExternalFilters(commandCtx, root, limit+1, "show", "--no-textconv", "--format=", spec)
	if result.err != nil {
		if allowMissing && (exitCode(result.err) == 1 || exitCode(result.err) == 128) {
			return fileData{}, nil
		}
		return fileData{}, commandError(commandCtx, CodeDiffFailed, result)
	}
	if result.truncated || len(result.stdout) > limit {
		return fileData{tooLarge: true}, nil
	}
	data := []byte(result.stdout)
	return fileData{content: data, binary: !utf8.Valid(data) || containsNUL(data)}, nil
}

func readWorktreeFile(root, path string, limit int, allowMissing bool) (fileData, error) {
	target := filepath.Join(root, filepath.FromSlash(path))
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) && allowMissing {
		return fileData{}, nil
	}
	if err != nil {
		return fileData{}, newError(CodeDiffFailed, "Read working tree file: "+err.Error(), err)
	}
	var data []byte
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(target)
		if err != nil {
			return fileData{}, newError(CodeDiffFailed, "Read working tree symlink: "+err.Error(), err)
		}
		data = []byte(link)
	} else {
		if !info.Mode().IsRegular() {
			return fileData{binary: true}, nil
		}
		resolved, err := filepath.EvalSymlinks(target)
		if err != nil || !projectpath.Inside(resolved, root) {
			return fileData{}, newError(CodeInvalidPath, "Working tree path is outside the repository", err)
		}
		if info.Size() > int64(limit) {
			return fileData{tooLarge: true}, nil
		}
		data, err = os.ReadFile(resolved)
		if err != nil {
			return fileData{}, newError(CodeDiffFailed, "Read working tree file: "+err.Error(), err)
		}
	}
	if len(data) > limit {
		return fileData{tooLarge: true}, nil
	}
	return fileData{content: data, binary: !utf8.Valid(data) || containsNUL(data)}, nil
}

func cleanGitPath(root, raw string) (string, error) {
	if strings.ContainsRune(raw, 0) || filepath.IsAbs(filepath.FromSlash(raw)) {
		return "", newError(CodeInvalidPath, "Git path is not authorized", nil)
	}
	clean := filepath.Clean(filepath.FromSlash(strings.TrimSpace(raw)))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", newError(CodeInvalidPath, "Git path is not authorized", nil)
	}
	target := filepath.Join(root, clean)
	if !projectpath.Inside(target, root) {
		return "", newError(CodeInvalidPath, "Git path is not authorized", nil)
	}
	return filepath.ToSlash(clean), nil
}

func containsNUL(data []byte) bool {
	return strings.IndexByte(string(data), 0) >= 0
}

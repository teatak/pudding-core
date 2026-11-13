package tool

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var unifiedHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@(?: .*)?$`)
var unifiedIndexHeader = regexp.MustCompile(`^index [0-9a-fA-F]+\.\.[0-9a-fA-F]+(?: (?:100644|100755))?$`)

// Parse text only. No shell, Git config, path guessing or fuzzy matching is
// involved; the resulting operations use the normal patch approval transaction.
func parseUnifiedPatch(diff string, relocate bool) ([]patchFileArg, *toolArgumentError) {
	if len(diff) > patchMaxTotalBytes {
		return nil, unifiedPatchError(1, "diff exceeds the 2 MiB input limit; split it into smaller batches")
	}
	if diff == "" || !isToolText([]byte(diff)) {
		return nil, unifiedPatchError(1, "diff must be non-empty UTF-8 text without NUL bytes")
	}
	lines := strings.Split(diff, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	var files []patchFileArg
	for at := 0; at < len(lines); {
		if len(files) == patchMaxFiles {
			return nil, unifiedPatchError(at+1, "at most 16 files are supported")
		}
		mode := ""
		// Standard Git text metadata is accepted, but ---/+++ remain the
		// authoritative target names shown in the existing approval diff.
		if strings.HasPrefix(lines[at], "diff --git ") {
			at++
			for at < len(lines) && !strings.HasPrefix(lines[at], "--- ") {
				line := strings.TrimSuffix(lines[at], "\r")
				switch {
				case unifiedIndexHeader.MatchString(line):
				case line == "new file mode 100644" && mode == "":
					mode = "create"
				case (line == "deleted file mode 100644" || line == "deleted file mode 100755") && mode == "":
					mode = "delete"
				default:
					return nil, unifiedPatchError(at+1, "expected text file headers; binary, rename/copy, mode changes and executable creation are unsupported")
				}
				at++
			}
		}
		if at+1 >= len(lines) || !strings.HasPrefix(lines[at], "--- ") || !strings.HasPrefix(lines[at+1], "+++ ") {
			return nil, unifiedPatchError(at+1, "expected consecutive --- old-path and +++ new-path headers")
		}
		oldPath, err := unifiedPatchPath(strings.TrimPrefix(lines[at], "--- "), at+1)
		if err != nil {
			return nil, err
		}
		newPath, err := unifiedPatchPath(strings.TrimPrefix(lines[at+1], "+++ "), at+2)
		if err != nil {
			return nil, err
		}
		at += 2
		if (strings.HasPrefix(oldPath, "a/") || oldPath == "/dev/null") && (strings.HasPrefix(newPath, "b/") || newPath == "/dev/null") {
			oldPath = strings.TrimPrefix(oldPath, "a/")
			newPath = strings.TrimPrefix(newPath, "b/")
		}
		file := patchFileArg{Path: oldPath, Action: "edit"}
		switch {
		case oldPath == "/dev/null" && newPath == "/dev/null":
			return nil, unifiedPatchError(at, "both paths cannot be /dev/null")
		case oldPath == "/dev/null":
			file.Path, file.Action = newPath, "create"
		case newPath == "/dev/null":
			file.Action = "delete"
		case oldPath != newPath:
			return nil, unifiedPatchError(at, "renames are unsupported; old and new paths must identify the same file")
		}
		if file.Path == "" || mode != "" && mode != file.Action {
			return nil, unifiedPatchError(at, "file mode metadata and paths disagree")
		}
		var hunks []patchHunkArg
		delta, previousEnd := 0, 0
		for at < len(lines) && strings.HasPrefix(lines[at], "@@ ") {
			if len(hunks) == patchMaxHunksPerFile {
				return nil, unifiedPatchError(at+1, "at most 64 hunks per file are supported")
			}
			header := unifiedHunkHeader.FindStringSubmatch(strings.TrimSuffix(lines[at], "\r"))
			if header == nil {
				return nil, unifiedPatchError(at+1, "invalid @@ -old[,count] +new[,count] @@ header")
			}
			coordinates := [4]int{}
			for i, value := range header[1:] {
				if value == "" {
					value = "1"
				}
				n, parseErr := strconv.ParseUint(value, 10, 31)
				if parseErr != nil {
					return nil, unifiedPatchError(at+1, "hunk coordinate is out of range")
				}
				coordinates[i] = int(n)
			}
			oldStart, oldCount, newStart, newCount := coordinates[0], coordinates[1], coordinates[2], coordinates[3]
			if oldCount > patchMaxFileBytes || newCount > patchMaxFileBytes || oldCount+newCount == 0 || oldCount > 0 && oldStart == 0 || newCount > 0 && newStart == 0 {
				return nil, unifiedPatchError(at+1, "invalid hunk range or line count")
			}
			if oldCount == 0 {
				oldStart++
			}
			if newCount == 0 {
				newStart++
			}
			if oldStart < previousEnd || newStart != oldStart+delta {
				return nil, unifiedPatchError(at+1, "hunk ranges overlap, are out of order, or new coordinates disagree with earlier hunks")
			}
			previousEnd, delta = oldStart+oldCount, delta+newCount-oldCount
			at++
			hunk, next, err := parseUnifiedHunk(lines, at, oldStart, oldCount, newCount, relocate)
			if err != nil {
				return nil, err
			}
			at = next
			hunks = append(hunks, hunk)
		}
		if len(hunks) == 0 {
			return nil, unifiedPatchError(at+1, "expected at least one @@ hunk; use files input for empty-file creation or deletion")
		}
		if file.Action == "edit" {
			file.Hunks = hunks
		} else {
			if len(hunks) != 1 || hunks[0].StartLine != 1 {
				return nil, unifiedPatchError(at, "creation and deletion must describe the complete file in one hunk")
			}
			h := hunks[0]
			if file.Action == "create" {
				if len(*h.OldLines) != 0 {
					return nil, unifiedPatchError(at, "creation cannot contain source lines")
				}
				content := unifiedPatchContent(*h.NewLines, h.newEndings)
				file.Content = &content
			} else {
				if len(*h.NewLines) != 0 {
					return nil, unifiedPatchError(at, "deletion cannot contain destination lines")
				}
				content := unifiedPatchContent(*h.OldLines, h.oldEndings)
				file.deleteContent = &content
			}
		}
		files = append(files, file)
	}
	return files, nil
}

func parseUnifiedHunk(lines []string, at, start, oldCount, newCount int, relocate bool) (patchHunkArg, int, *toolArgumentError) {
	oldLines, newLines := []string{}, []string{}
	h := patchHunkArg{StartLine: start, Relocate: relocate, OldLines: &oldLines, NewLines: &newLines, oldEndings: []string{}, newEndings: []string{}}
	var previous byte
	changed := false
	for at < len(lines) {
		line := lines[at]
		if strings.TrimSuffix(line, "\r") == `\ No newline at end of file` {
			if previous == 0 {
				return h, at, unifiedPatchError(at+1, "newline marker must immediately follow a source or destination line")
			}
			if previous != '+' {
				last := len(oldLines) - 1
				if h.oldEndings[last] == "\r\n" {
					oldLines[last] += "\r"
				}
				h.oldEndings[last] = ""
			}
			if previous != '-' {
				last := len(newLines) - 1
				if h.newEndings[last] == "\r\n" {
					newLines[last] += "\r"
				}
				h.newEndings[last] = ""
			}
			previous = 0
			at++
			continue
		}
		if len(oldLines) == oldCount && len(newLines) == newCount {
			break
		}
		if len(line) == 0 || !strings.ContainsRune(" +-", rune(line[0])) {
			return h, at, unifiedPatchError(at+1, "hunk ended before its declared line counts; every body line needs a space, + or - prefix")
		}
		prefix, text, ending := line[0], line[1:], "\n"
		if strings.HasSuffix(text, "\r") {
			text, ending = strings.TrimSuffix(text, "\r"), "\r\n"
		}
		if prefix != '+' {
			if len(oldLines) >= oldCount || len(h.oldEndings) > 0 && h.oldEndings[len(h.oldEndings)-1] == "" {
				return h, at, unifiedPatchError(at+1, "source line count exceeded or a source line follows its no-newline marker")
			}
			oldLines, h.oldEndings = append(oldLines, text), append(h.oldEndings, ending)
		}
		if prefix != '-' {
			if len(newLines) >= newCount || len(h.newEndings) > 0 && h.newEndings[len(h.newEndings)-1] == "" {
				return h, at, unifiedPatchError(at+1, "destination line count exceeded or a destination line follows its no-newline marker")
			}
			newLines, h.newEndings = append(newLines, text), append(h.newEndings, ending)
		}
		changed = changed || prefix != ' '
		previous = prefix
		at++
	}
	if len(oldLines) != oldCount || len(newLines) != newCount || !changed {
		return h, at, unifiedPatchError(at+1, "hunk line counts do not match its body, or the hunk contains no change")
	}
	return h, at, nil
}

func unifiedPatchContent(lines, endings []string) string {
	var content strings.Builder
	for i, line := range lines {
		content.WriteString(line)
		content.WriteString(endings[i])
	}
	return content.String()
}

func unifiedPatchPath(header string, line int) (string, *toolArgumentError) {
	header = strings.TrimSuffix(header, "\r")
	var name string
	if strings.HasPrefix(header, `"`) {
		end := 1
		for end < len(header) {
			if header[end] == '\\' {
				end += 2
				continue
			}
			if header[end] == '"' {
				break
			}
			end++
		}
		if end >= len(header) || end+1 < len(header) && header[end+1] != '\t' {
			return "", unifiedPatchError(line, "invalid quoted path or timestamp separator")
		}
		var err error
		name, err = strconv.Unquote(header[:end+1])
		if err != nil {
			return "", unifiedPatchError(line, "invalid quoted path")
		}
	} else {
		name, _, _ = strings.Cut(header, "\t")
	}
	if name == "" || name != strings.TrimSpace(name) || strings.ContainsAny(name, "\r\n") || !isToolText([]byte(name)) {
		return "", unifiedPatchError(line, "invalid file path")
	}
	return name, nil
}

func unifiedPatchError(line int, detail string) *toolArgumentError {
	return &toolArgumentError{kind: "invalid_diff", field: "diff", detail: fmt.Sprintf("Unified diff line %d: %s", line, detail), hint: "Provide complete ---/+++ headers and exact @@ hunks with matching counts. Use files input for unsupported operations; no files were changed."}
}

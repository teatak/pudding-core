package contextbuilder

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	projectInstructionMaxFiles     = 64
	projectInstructionMaxFileBytes = 64 << 10
	projectInstructionMaxTotal     = 256 << 10
)

type projectInstructionContext struct {
	path        string
	projectRoot string
	content     string
	truncated   bool
}

// loadProjectRootInstructions reads AGENTS.md from each authorized Project
// root before a Code turn. Nested rules are not globally injected because
// their scope depends on the concrete target path.
func loadProjectRootInstructions(projectDirs []string) []projectInstructionContext {
	out := make([]projectInstructionContext, 0, min(len(projectDirs), projectInstructionMaxFiles))
	seen := make(map[string]bool, len(projectDirs))
	totalBytes := 0
	for _, root := range projectDirs {
		if len(out) >= projectInstructionMaxFiles || totalBytes >= projectInstructionMaxTotal {
			break
		}
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil || seen[resolvedRoot] {
			continue
		}
		seen[resolvedRoot] = true
		path := filepath.Join(resolvedRoot, "AGENTS.md")
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		remaining := min(projectInstructionMaxFileBytes, projectInstructionMaxTotal-totalBytes)
		content, truncated, ok := readProjectInstruction(path, info.Size(), remaining)
		if !ok || strings.TrimSpace(content) == "" {
			continue
		}
		totalBytes += len([]byte(content))
		out = append(out, projectInstructionContext{
			path:        "AGENTS.md",
			projectRoot: root,
			content:     content,
			truncated:   truncated,
		})
	}
	return out
}

func readProjectInstruction(path string, size int64, limit int) (string, bool, bool) {
	if limit <= 0 {
		return "", true, true
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit+utf8.UTFMax)))
	if err != nil {
		return "", false, false
	}
	truncated := len(data) > limit || size > int64(limit)
	if len(data) > limit {
		data = data[:limit]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", false, false
	}
	return string(data), truncated, true
}

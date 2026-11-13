package tool

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

const maxDiscoveryIgnoreBytes = 1 << 20

type discoveryIgnoreRules struct {
	parent   *discoveryIgnoreRules
	patterns []gitignore.Pattern
}

// Rules are read only within the authorized root, without Git config or global
// excludes. Each directory inherits its parents, with nearer rules winning.
type fileDiscoveryFilter struct {
	baseRoot       string
	includeIgnored bool
	skipHidden     bool
	root           *os.Root
	rules          map[string]*discoveryIgnoreRules
	ruleBytes      int
}

func newFileDiscoveryFilter(baseRoot string, includeIgnored, skipHidden bool) (*fileDiscoveryFilter, error) {
	canonical, err := filepath.EvalSymlinks(baseRoot)
	if err != nil {
		return nil, err
	}
	baseRoot = canonical
	f := &fileDiscoveryFilter{baseRoot: baseRoot, includeIgnored: includeIgnored, skipHidden: skipHidden}
	if !includeIgnored {
		var err error
		f.root, err = os.OpenRoot(baseRoot)
		if err != nil {
			return nil, err
		}
		f.rules = make(map[string]*discoveryIgnoreRules)
	}
	return f, nil
}

func (f *fileDiscoveryFilter) close() {
	if f.root != nil {
		f.root.Close()
	}
}

func (f *fileDiscoveryFilter) loadRules(dir string) (*discoveryIgnoreRules, error) {
	if rules, ok := f.rules[dir]; ok {
		return rules, nil
	}
	rules := &discoveryIgnoreRules{}
	if dir != "." {
		var err error
		rules.parent, err = f.loadRules(filepath.Dir(dir))
		if err != nil {
			return nil, err
		}
	}
	name := filepath.Join(dir, ".gitignore")
	info, err := f.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		f.rules[dir] = rules
		return rules, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("ignore rules must be a regular file: %s", name)
	}
	file, err := f.root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxDiscoveryIgnoreBytes-f.ruleBytes+1)))
	if err != nil {
		return nil, err
	}
	f.ruleBytes += len(data)
	if f.ruleBytes > maxDiscoveryIgnoreBytes {
		return nil, errors.New("ignore rules exceed 1 MiB; narrow the directory or use include_ignored=true")
	}
	if !isToolText(data) {
		return nil, fmt.Errorf("ignore rules must be UTF-8 text: %s", name)
	}
	var domain []string
	if dir != "." {
		domain = strings.Split(filepath.ToSlash(dir), "/")
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
			rules.patterns = append(rules.patterns, gitignore.ParsePattern(line, domain))
		}
	}
	f.rules[dir] = rules
	return rules, nil
}

func (f *fileDiscoveryFilter) accept(path string, entry fs.DirEntry) (bool, error) {
	if f.skipHidden && strings.HasPrefix(entry.Name(), ".") {
		return false, nil
	}
	if entry.IsDir() {
		switch entry.Name() {
		case ".git", ".hg", ".svn":
			return false, nil
		}
		if _, skip := fileSearchSkipDirs[entry.Name()]; skip && !f.includeIgnored {
			return false, nil
		}
	}
	if f.includeIgnored {
		return true, nil
	}
	rel, err := filepath.Rel(f.baseRoot, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, errors.New("discovery path is outside its authorized root")
	}
	rules, err := f.loadRules(filepath.Dir(rel))
	if err != nil {
		return false, &TextFileSearchError{Code: "ignore_rules_unavailable", Detail: err.Error()}
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for ; rules != nil; rules = rules.parent {
		for i := len(rules.patterns) - 1; i >= 0; i-- {
			if match := rules.patterns[i].Match(parts, entry.IsDir()); match != gitignore.NoMatch {
				return match != gitignore.Exclude, nil
			}
		}
	}
	return true, nil
}

func (r *BuiltinRunner) fileListRecursive(ctx context.Context, out Result, resolved resolvedFilePath, maxEntries int, include, exclude []string, includeIgnored bool, cursor string) Result {
	filter, err := newFileDiscoveryFilter(resolved.root, includeIgnored, !resolved.project)
	if err != nil {
		return toolJSONError(out, "read_dir_failed", err.Error())
	}
	defer filter.close()
	page, err := newFilePage(resolved.target, cursor, []any{"list", resolved.target, filter.baseRoot, filter.skipHidden, include, exclude, includeIgnored, resolved.widgetID})
	if err != nil {
		return toolJSONError(out, "invalid_cursor", err.Error())
	}
	items := make([]map[string]any, 0)
	var lastItem filePagePosition
	skipped := make(map[string]int)
	capped := false
	options := fileSearchOptions{baseRoot: filter.baseRoot, includeGlobs: include, excludeGlobs: exclude}
	err = filepath.WalkDir(resolved.target, func(path string, entry fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			skipped["read_error"]++
			return nil
		}
		if path == resolved.target {
			if !entry.IsDir() {
				return errors.New("recursive listing requires a directory")
			}
			return nil
		}
		if page.skip(path, entry.IsDir()) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !page.visit(path) {
			capped = true
			return filepath.SkipAll
		}
		allowed, err := filter.accept(path, entry)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(filter.baseRoot, path)
		if err != nil {
			return err
		}
		if resolved.widgetID != "" && !widgetVisiblePath(filepath.ToSlash(rel), entry.IsDir()) {
			allowed = false
		}
		if !allowed {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !searchPathAllowed(path, options) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			skipped["symlink"]++
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			skipped["read_error"]++
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if len(items) == maxEntries {
			capped = true
			page.last = lastItem
			return filepath.SkipAll
		}
		outputPath := path
		if !resolved.project {
			outputPath = filepath.ToSlash(rel)
		}
		items = append(items, map[string]any{"name": entry.Name(), "path": outputPath, "type": "file", "size": info.Size()})
		lastItem = page.position(path, 0)
		return nil
	})
	if err != nil {
		var searchErr *TextFileSearchError
		if errors.As(err, &searchErr) {
			return toolJSONError(out, searchErr.Code, searchErr.Detail)
		}
		return toolJSONError(out, "read_dir_failed", err.Error())
	}
	nextCursor := ""
	if capped {
		nextCursor = page.next()
	}
	out = toolJSON(out, true, resolved.payload(map[string]any{"ok": true, "entries": items, "recursive": true, "returnedCount": len(items), "truncated": capped, "nextCursor": nextCursor, "complete": !capped && len(skipped) == 0, "skipped": skipped, "includeIgnored": includeIgnored}))
	out.SummaryKind, out.SummaryCount = SummaryReturnedItems, len(items)
	return out
}

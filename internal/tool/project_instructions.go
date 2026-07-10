package tool

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	projectInstructionMaxTargets   = 32
	projectInstructionMaxFiles     = 64
	projectInstructionMaxFileBytes = 64 << 10
	projectInstructionMaxTotal     = 256 << 10
)

type projectInstructionsArgs struct {
	Scope string   `json:"scope"`
	Paths []string `json:"paths"`
}

type projectInstructionTargetView struct {
	Path        string `json:"path"`
	ProjectRoot string `json:"projectRoot"`
	Directory   string `json:"directory"`
	Exists      bool   `json:"exists"`
}

type projectInstructionFileView struct {
	Path        string   `json:"path"`
	ProjectRoot string   `json:"projectRoot"`
	ScopePath   string   `json:"scopePath"`
	Kind        string   `json:"kind"`
	Content     string   `json:"content"`
	Chars       int      `json:"chars"`
	Size        int64    `json:"size"`
	Truncated   bool     `json:"truncated"`
	AppliesTo   []string `json:"appliesTo"`
	Order       int      `json:"order"`
}

type projectInstructionWarning struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

type projectInstructionCandidate struct {
	path      string
	rel       string
	root      string
	scopePath string
	kind      string
	depth     int
	priority  int
	appliesTo map[string]bool
}

type projectInstructionSpec struct {
	name     string
	kind     string
	priority int
}

var projectInstructionSpecs = []projectInstructionSpec{
	{name: "CONTRIBUTING.md", kind: "contributing_guide", priority: 0},
	{name: "CLAUDE.md", kind: "assistant_instructions", priority: 1},
	{name: "AGENTS.md", kind: "agent_instructions", priority: 2},
}

func (r *BuiltinRunner) projectInstructions(call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args projectInstructionsArgs
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	if strings.TrimSpace(args.Scope) != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "project instructions scope must be project")
	}
	if len(args.Paths) == 0 || len(args.Paths) > projectInstructionMaxTargets {
		return toolJSONError(out, "invalid_arguments", "paths must contain between 1 and 32 project targets")
	}

	targets := make([]projectInstructionTargetView, 0, len(args.Paths))
	candidates := make(map[string]*projectInstructionCandidate)
	seenTargets := make(map[string]bool)
	for _, rawPath := range args.Paths {
		target, discovered, err := discoverProjectInstructionsForTarget(call.ProjectDirs, rawPath)
		if err != nil {
			return filePathError(out, managedScopeProject, err)
		}
		targetKey := target.ProjectRoot + "\x00" + target.Path
		if seenTargets[targetKey] {
			continue
		}
		seenTargets[targetKey] = true
		targets = append(targets, target)
		for _, candidate := range discovered {
			existing := candidates[candidate.path]
			if existing == nil {
				candidate.appliesTo = map[string]bool{target.Path: true}
				candidates[candidate.path] = candidate
				continue
			}
			existing.appliesTo[target.Path] = true
		}
	}

	ordered := make([]*projectInstructionCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].root != ordered[j].root {
			return ordered[i].root < ordered[j].root
		}
		if ordered[i].depth != ordered[j].depth {
			return ordered[i].depth < ordered[j].depth
		}
		if ordered[i].priority != ordered[j].priority {
			return ordered[i].priority < ordered[j].priority
		}
		return ordered[i].rel < ordered[j].rel
	})

	views := make([]projectInstructionFileView, 0, min(len(ordered), projectInstructionMaxFiles))
	warnings := make([]projectInstructionWarning, 0)
	totalBytes := 0
	for _, candidate := range ordered {
		if len(views) >= projectInstructionMaxFiles {
			warnings = append(warnings, projectInstructionWarning{Path: candidate.rel, Reason: "instruction_limit_reached", Detail: "at most 64 project instruction files are returned"})
			break
		}
		remaining := projectInstructionMaxTotal - totalBytes
		view, warning := readProjectInstructionCandidate(candidate, remaining)
		if warning != nil {
			warnings = append(warnings, *warning)
			continue
		}
		view.Order = len(views) + 1
		totalBytes += len([]byte(view.Content))
		views = append(views, view)
	}

	payload := map[string]any{
		"ok":               true,
		"scope":            managedScopeProject,
		"targets":          targets,
		"targetCount":      len(targets),
		"instructions":     views,
		"instructionCount": len(views),
		"warnings":         warnings,
		"warningCount":     len(warnings),
		"contentBytes":     totalBytes,
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryReadFiles, len(views))
}

func discoverProjectInstructionsForTarget(projectDirs []string, rawPath string) (projectInstructionTargetView, []*projectInstructionCandidate, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return projectInstructionTargetView{}, nil, errProjectFilePathRequired
	}
	root, _, rel, err := resolveProjectPath(projectDirs, rawPath, true, true)
	if err != nil {
		return projectInstructionTargetView{}, nil, err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return projectInstructionTargetView{}, nil, errProjectPathNotAllowed
	}
	targetPath := filepath.Join(resolvedRoot, filepath.FromSlash(rel))
	info, statErr := os.Stat(targetPath)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return projectInstructionTargetView{}, nil, errProjectPathNotAllowed
	}
	targetDir := targetPath
	if !exists || !info.IsDir() {
		targetDir = filepath.Dir(targetPath)
	}
	if !pathInsideRoot(targetDir, resolvedRoot) {
		return projectInstructionTargetView{}, nil, errProjectPathNotAllowed
	}
	directoryRel, err := filepath.Rel(resolvedRoot, targetDir)
	if err != nil {
		return projectInstructionTargetView{}, nil, errProjectPathNotAllowed
	}
	directoryRel = filepath.ToSlash(directoryRel)
	if directoryRel == "" {
		directoryRel = "."
	}
	target := projectInstructionTargetView{
		Path:        filepath.ToSlash(rel),
		ProjectRoot: root,
		Directory:   directoryRel,
		Exists:      exists,
	}

	directories := projectInstructionDirectories(resolvedRoot, targetDir)
	candidates := make([]*projectInstructionCandidate, 0)
	for depth, directory := range directories {
		scopeRel, _ := filepath.Rel(resolvedRoot, directory)
		scopeRel = filepath.ToSlash(scopeRel)
		if scopeRel == "" {
			scopeRel = "."
		}
		for _, spec := range projectInstructionSpecs {
			candidatePath := filepath.Join(directory, spec.name)
			if _, err := os.Lstat(candidatePath); err != nil {
				continue
			}
			relPath, _ := filepath.Rel(resolvedRoot, candidatePath)
			candidates = append(candidates, &projectInstructionCandidate{
				path:      candidatePath,
				rel:       filepath.ToSlash(relPath),
				root:      root,
				scopePath: scopeRel,
				kind:      spec.kind,
				depth:     depth,
				priority:  spec.priority,
			})
		}
	}
	return target, candidates, nil
}

func projectInstructionDirectories(root, targetDir string) []string {
	rel, err := filepath.Rel(root, targetDir)
	if err != nil || rel == "." || rel == "" {
		return []string{root}
	}
	directories := []string{root}
	current := root
	for _, segment := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		current = filepath.Join(current, segment)
		directories = append(directories, current)
	}
	return directories
}

func readProjectInstructionCandidate(candidate *projectInstructionCandidate, remaining int) (projectInstructionFileView, *projectInstructionWarning) {
	view := projectInstructionFileView{
		Path:        candidate.rel,
		ProjectRoot: candidate.root,
		ScopePath:   candidate.scopePath,
		Kind:        candidate.kind,
		AppliesTo:   sortedInstructionTargets(candidate.appliesTo),
	}
	info, err := os.Lstat(candidate.path)
	if err != nil {
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "instruction_unavailable", Detail: err.Error()}
	}
	view.Size = info.Size()
	if info.Mode()&os.ModeSymlink != 0 {
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "symlink_unsupported", Detail: "project instruction files must not be symbolic links"}
	}
	if !info.Mode().IsRegular() {
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "regular_file_required", Detail: "project instruction path must be a regular file"}
	}
	if binary, _, err := probeBinaryFile(candidate.path); err != nil || binary {
		detail := "project instruction file must be UTF-8 text"
		if err != nil {
			detail = err.Error()
		}
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "binary_file", Detail: detail}
	}
	limit := min(projectInstructionMaxFileBytes, max(0, remaining))
	if limit == 0 {
		view.Truncated = true
		return view, nil
	}
	file, err := os.Open(candidate.path)
	if err != nil {
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "read_failed", Detail: err.Error()}
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(limit+utf8.UTFMax)))
	if err != nil {
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "read_failed", Detail: err.Error()}
	}
	view.Truncated = len(data) > limit || info.Size() > int64(limit)
	if len(data) > limit {
		data = data[:limit]
	}
	for len(data) > 0 && !utf8.Valid(data) {
		data = data[:len(data)-1]
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return view, &projectInstructionWarning{Path: candidate.rel, Reason: "binary_file", Detail: "project instruction file must be UTF-8 text without NUL bytes"}
	}
	view.Content = string(data)
	view.Chars = utf8.RuneCount(data)
	return view, nil
}

func sortedInstructionTargets(targets map[string]bool) []string {
	out := make([]string, 0, len(targets))
	for target := range targets {
		out = append(out, target)
	}
	sort.Strings(out)
	return out
}

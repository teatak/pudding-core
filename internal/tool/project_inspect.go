package tool

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	projectInspectMaxDepth         = 6
	projectInspectMaxFiles         = 5000
	projectInspectMaxManifestBytes = 256 << 10
)

type projectInspectArgs struct {
	Scope string `json:"scope"`
	Path  string `json:"path,omitempty"`
}

type projectLanguage struct {
	Name      string `json:"name"`
	FileCount int    `json:"fileCount"`
}

type projectManifest struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type projectInstruction struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type projectSuggestedCommand struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	Argv   []string `json:"argv"`
	CWD    string   `json:"cwd"`
	Source string   `json:"source"`
}

type projectInspectSnapshot struct {
	languages    []projectLanguage
	manifests    []projectManifest
	instructions []projectInstruction
	commands     []projectSuggestedCommand
	filesScanned int
	scanCapped   bool
}

func (r *BuiltinRunner) projectInspect(ctx context.Context, call Call) Result {
	out := Result{CallID: call.CallID, Name: call.Name}
	var args projectInspectArgs
	if err := decodeStructToolArgs(call.Args, &args); err != nil {
		return toolJSONError(out, "invalid_arguments", err.Error())
	}
	scope := strings.TrimSpace(args.Scope)
	if scope == "" {
		scope = managedScopeProject
	}
	if scope != managedScopeProject {
		return toolJSONError(out, "invalid_scope", "project inspection scope must be project")
	}
	root, target, rel, err := resolveProjectPath(call.ProjectDirs, args.Path, true, false)
	if err != nil {
		return filePathError(out, managedScopeProject, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return toolJSONError(out, "inspect_path_unavailable", err.Error())
	}
	if !info.IsDir() {
		return toolJSONError(out, "inspect_path_not_directory", "project inspection path must be a directory")
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return toolJSONError(out, "project_root_unavailable", err.Error())
	}

	snapshot, err := inspectProjectTree(ctx, resolvedRoot, target)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return toolJSONError(out, "inspection_cancelled", err.Error())
		}
		return toolJSONError(out, "inspection_failed", err.Error())
	}
	gitRoot := ""
	gitCWD := strings.TrimSpace(args.Path)
	if gitCWD == "" {
		gitCWD = "."
	}
	if repo, failed := resolveGitRepository(ctx, call, managedScopeProject, gitCWD); failed == nil {
		gitRoot = repo.Root
	}
	payload := map[string]any{
		"ok":                true,
		"scope":             managedScopeProject,
		"projectRoot":       root,
		"inspectPath":       target,
		"relativePath":      rel,
		"gitRoot":           gitRoot,
		"languages":         snapshot.languages,
		"manifests":         snapshot.manifests,
		"instructions":      snapshot.instructions,
		"suggestedCommands": snapshot.commands,
		"filesScanned":      snapshot.filesScanned,
		"scanCapped":        snapshot.scanCapped,
	}
	return withResultSummary(toolJSON(out, true, payload), SummaryReturnedFields, len(payload))
}

func inspectProjectTree(ctx context.Context, projectRoot, target string) (projectInspectSnapshot, error) {
	snapshot := projectInspectSnapshot{
		languages:    []projectLanguage{},
		manifests:    []projectManifest{},
		instructions: []projectInstruction{},
		commands:     []projectSuggestedCommand{},
	}
	languageCounts := make(map[string]int)
	err := filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if walkErr != nil {
			return nil
		}
		relTarget, err := filepath.Rel(target, path)
		if err != nil {
			return nil
		}
		depth := pathDepth(relTarget)
		if entry.IsDir() {
			if path != target {
				if _, skip := fileSearchSkipDirs[entry.Name()]; skip {
					return filepath.SkipDir
				}
				if depth > projectInspectMaxDepth {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if depth > projectInspectMaxDepth {
			return nil
		}
		if snapshot.filesScanned >= projectInspectMaxFiles {
			snapshot.scanCapped = true
			return filepath.SkipAll
		}
		snapshot.filesScanned++
		relProject, err := filepath.Rel(projectRoot, path)
		if err != nil {
			return nil
		}
		relProject = filepath.ToSlash(relProject)
		if language := projectLanguageForPath(path); language != "" {
			languageCounts[language]++
		}
		if kind := projectManifestKind(entry.Name()); kind != "" {
			snapshot.manifests = append(snapshot.manifests, projectManifest{Path: relProject, Kind: kind})
		}
		if kind := projectInstructionKind(entry.Name()); kind != "" {
			snapshot.instructions = append(snapshot.instructions, projectInstruction{Path: relProject, Kind: kind})
		}
		return nil
	})
	if err != nil {
		return projectInspectSnapshot{}, err
	}
	for language, count := range languageCounts {
		snapshot.languages = append(snapshot.languages, projectLanguage{Name: language, FileCount: count})
	}
	sort.Slice(snapshot.languages, func(i, j int) bool {
		if snapshot.languages[i].FileCount != snapshot.languages[j].FileCount {
			return snapshot.languages[i].FileCount > snapshot.languages[j].FileCount
		}
		return snapshot.languages[i].Name < snapshot.languages[j].Name
	})
	sort.Slice(snapshot.manifests, func(i, j int) bool { return snapshot.manifests[i].Path < snapshot.manifests[j].Path })
	sort.Slice(snapshot.instructions, func(i, j int) bool { return snapshot.instructions[i].Path < snapshot.instructions[j].Path })
	snapshot.commands = suggestedProjectCommands(projectRoot, snapshot.manifests)
	return snapshot, nil
}

func pathDepth(rel string) int {
	if rel == "." || rel == "" {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func projectLanguageForPath(path string) string {
	extension := strings.ToLower(filepath.Ext(path))
	languages := map[string]string{
		".c": "C", ".cc": "C++", ".cpp": "C++", ".cs": "C#", ".css": "CSS",
		".dart": "Dart", ".ex": "Elixir", ".exs": "Elixir", ".go": "Go", ".h": "C",
		".hpp": "C++", ".html": "HTML", ".java": "Java", ".js": "JavaScript",
		".jsx": "JavaScript", ".kt": "Kotlin", ".kts": "Kotlin", ".lua": "Lua",
		".php": "PHP", ".py": "Python", ".rb": "Ruby", ".rs": "Rust", ".scala": "Scala",
		".sh": "Shell", ".sql": "SQL", ".svelte": "Svelte", ".swift": "Swift",
		".ts": "TypeScript", ".tsx": "TypeScript", ".vue": "Vue",
	}
	return languages[extension]
}

func projectManifestKind(name string) string {
	switch strings.ToLower(name) {
	case "go.mod":
		return "go_module"
	case "go.work":
		return "go_workspace"
	case "package.json":
		return "node_package"
	case "cargo.toml":
		return "cargo_package"
	case "pyproject.toml":
		return "python_project"
	case "requirements.txt":
		return "python_requirements"
	case "pom.xml":
		return "maven_project"
	case "build.gradle", "build.gradle.kts":
		return "gradle_project"
	case "composer.json":
		return "php_package"
	case "gemfile":
		return "ruby_bundle"
	case "mix.exs":
		return "elixir_project"
	case "package.swift":
		return "swift_package"
	case "cmakelists.txt":
		return "cmake_project"
	case "makefile":
		return "makefile"
	default:
		return ""
	}
}

func projectInstructionKind(name string) string {
	switch strings.ToLower(name) {
	case "agents.md":
		return "agent_instructions"
	case "claude.md":
		return "assistant_instructions"
	case "contributing.md":
		return "contributing_guide"
	default:
		return ""
	}
}

func suggestedProjectCommands(projectRoot string, manifests []projectManifest) []projectSuggestedCommand {
	commands := make([]projectSuggestedCommand, 0)
	seen := make(map[string]bool)
	add := func(command projectSuggestedCommand) {
		key := command.CWD + "\x00" + strings.Join(command.Argv, "\x00")
		if !seen[key] {
			seen[key] = true
			commands = append(commands, command)
		}
	}
	for _, manifest := range manifests {
		cwd := filepath.ToSlash(filepath.Dir(manifest.Path))
		if cwd == "" {
			cwd = "."
		}
		sourcePath := filepath.Join(projectRoot, filepath.FromSlash(manifest.Path))
		switch manifest.Kind {
		case "go_module", "go_workspace":
			add(projectSuggestedCommand{Kind: "test", Name: "Go test", Argv: []string{"go", "test", "./..."}, CWD: cwd, Source: manifest.Path})
			add(projectSuggestedCommand{Kind: "lint", Name: "Go vet", Argv: []string{"go", "vet", "./..."}, CWD: cwd, Source: manifest.Path})
		case "cargo_package":
			add(projectSuggestedCommand{Kind: "test", Name: "Cargo test", Argv: []string{"cargo", "test"}, CWD: cwd, Source: manifest.Path})
			add(projectSuggestedCommand{Kind: "check", Name: "Cargo check", Argv: []string{"cargo", "check"}, CWD: cwd, Source: manifest.Path})
		case "node_package":
			for _, command := range packageJSONCommands(sourcePath, cwd, manifest.Path) {
				add(command)
			}
		case "maven_project":
			add(projectSuggestedCommand{Kind: "test", Name: "Maven test", Argv: []string{"mvn", "test"}, CWD: cwd, Source: manifest.Path})
		case "gradle_project":
			if _, err := os.Stat(filepath.Join(filepath.Dir(sourcePath), "gradlew")); err == nil {
				add(projectSuggestedCommand{Kind: "test", Name: "Gradle test", Argv: []string{"./gradlew", "test"}, CWD: cwd, Source: manifest.Path})
			}
		}
	}
	sort.Slice(commands, func(i, j int) bool {
		if commands[i].CWD != commands[j].CWD {
			return commands[i].CWD < commands[j].CWD
		}
		if commands[i].Kind != commands[j].Kind {
			return commands[i].Kind < commands[j].Kind
		}
		return commands[i].Name < commands[j].Name
	})
	return commands
}

func packageJSONCommands(path, cwd, source string) []projectSuggestedCommand {
	info, err := os.Stat(path)
	if err != nil || info.Size() > projectInspectMaxManifestBytes {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var manifest struct {
		Scripts map[string]string `json:"scripts"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil
	}
	manager := packageManager(filepath.Dir(path))
	priority := []string{"test", "lint", "typecheck", "type-check", "check", "build"}
	commands := make([]projectSuggestedCommand, 0, len(priority))
	for _, script := range priority {
		if strings.TrimSpace(manifest.Scripts[script]) == "" {
			continue
		}
		commands = append(commands, projectSuggestedCommand{
			Kind:   packageScriptKind(script),
			Name:   script,
			Argv:   []string{manager, "run", script},
			CWD:    cwd,
			Source: source,
		})
	}
	return commands
}

func packageManager(dir string) string {
	for _, candidate := range []struct {
		files []string
		name  string
	}{
		{files: []string{"pnpm-lock.yaml"}, name: "pnpm"},
		{files: []string{"yarn.lock"}, name: "yarn"},
		{files: []string{"bun.lock", "bun.lockb"}, name: "bun"},
	} {
		for _, file := range candidate.files {
			if _, err := os.Stat(filepath.Join(dir, file)); err == nil {
				return candidate.name
			}
		}
	}
	return "npm"
}

func packageScriptKind(script string) string {
	switch script {
	case "test":
		return "test"
	case "build":
		return "build"
	case "lint":
		return "lint"
	default:
		return "check"
	}
}

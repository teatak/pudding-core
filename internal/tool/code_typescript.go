package tool

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/teatak/pudding-core/internal/lsp"
)

const typeScriptServerKind = "typescript-language-server"

type TypeScriptServerResolver func(languageRoot, projectRoot string) (lsp.ServerSpec, error)

func (r *BuiltinRunner) resolveTypeScriptCodeTarget(target, documentLanguageID, start, projectRoot string) (resolvedCodeTarget, error) {
	languageRoot, fallback := resolveTypeScriptLanguageRoot(start, projectRoot)
	resolver := r.typeScriptServerResolver
	if resolver == nil {
		resolver = defaultTypeScriptServerResolver
	}
	spec, err := resolver(languageRoot, projectRoot)
	if err != nil {
		return resolvedCodeTarget{}, err
	}
	if filepath.Clean(spec.Key.LanguageRoot) != languageRoot || spec.Key.ServerKind != typeScriptServerKind {
		return resolvedCodeTarget{}, errors.New("TypeScript server resolver returned an invalid process key")
	}
	if documentLanguageID == "" {
		documentLanguageID = "typescript"
	}
	return resolvedCodeTarget{
		path:               target,
		language:           "typescript",
		documentLanguageID: documentLanguageID,
		languageRoot:       languageRoot,
		rootFallback:       fallback,
		spec:               spec,
	}, nil
}

func resolveTypeScriptLanguageRoot(start, projectRoot string) (string, bool) {
	for _, marker := range []string{"tsconfig.json", "jsconfig.json", "package.json"} {
		if root := findMarkerRoot(start, projectRoot, marker); root != "" {
			return root, false
		}
	}
	return projectRoot, true
}

func defaultTypeScriptServerResolver(languageRoot, projectRoot string) (lsp.ServerSpec, error) {
	return resolveTypeScriptServer(languageRoot, projectRoot, bundledLanguageServerPath(typeScriptServerKind))
}

func resolveTypeScriptServer(languageRoot, projectRoot, bundledExecutable string) (lsp.ServerSpec, error) {
	checked := make([]string, 0)
	if bundledExecutable != "" {
		checked = append(checked, "bundled:"+bundledExecutable)
		return typeScriptServerSpec(bundledExecutable, languageRoot)
	}
	for _, candidate := range typeScriptServerCandidates(languageRoot, projectRoot) {
		checked = append(checked, candidate)
		if !isExecutableFile(candidate) {
			continue
		}
		return typeScriptServerSpec(candidate, languageRoot)
	}
	checked = append(checked, "PATH:"+typeScriptServerKind)
	executable, err := exec.LookPath(typeScriptServerKind)
	if err != nil {
		return lsp.ServerSpec{}, &languageServerUnavailableError{
			language: "typescript",
			server:   typeScriptServerKind,
			checked:  checked,
			hint:     "Reinstall Pudding or configure typescript-language-server and TypeScript in the project or PATH, then retry. Pudding does not download them at runtime.",
		}
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return lsp.ServerSpec{}, err
	}
	return typeScriptServerSpec(filepath.Clean(executable), languageRoot)
}

func typeScriptServerCandidates(languageRoot, projectRoot string) []string {
	name := typeScriptServerKind
	if runtime.GOOS == "windows" {
		name += ".cmd"
	}
	seen := map[string]bool{}
	candidates := make([]string, 0)
	for dir := filepath.Clean(languageRoot); pathInsideRoot(dir, projectRoot); dir = filepath.Dir(dir) {
		candidate := filepath.Join(dir, "node_modules", ".bin", name)
		if !seen[candidate] {
			seen[candidate] = true
			candidates = append(candidates, candidate)
		}
		if dir == filepath.Clean(projectRoot) {
			break
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return candidates
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	return runtime.GOOS == "windows" || info.Mode().Perm()&0o111 != 0
}

func typeScriptServerSpec(executable, languageRoot string) (lsp.ServerSpec, error) {
	env, err := commandEnvironment(nil)
	if err != nil {
		return lsp.ServerSpec{}, err
	}
	command := executable
	args := []string{"--stdio"}
	if runtime.GOOS == "windows" {
		extension := strings.ToLower(filepath.Ext(executable))
		if extension == ".cmd" || extension == ".bat" {
			command = strings.TrimSpace(os.Getenv("COMSPEC"))
			if command == "" {
				command = "cmd.exe"
			}
			args = []string{"/d", "/s", "/c", `"` + executable + `" --stdio`}
		}
	}
	return lsp.ServerSpec{
		Key:     lsp.ProcessKey{LanguageRoot: languageRoot, ServerKind: typeScriptServerKind},
		Command: command,
		Args:    args,
		Dir:     languageRoot,
		Env:     env,
	}, nil
}

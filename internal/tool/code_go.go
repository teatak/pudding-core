package tool

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/teatak/pudding-core/internal/lsp"
)

const maxCodeDocumentBytes = 2 << 20

type GoServerResolver func(languageRoot string) (lsp.ServerSpec, error)

type resolvedCodeTarget struct {
	path         string
	language     string
	languageRoot string
	rootFallback bool
	spec         lsp.ServerSpec
}

type languageServerUnavailableError struct {
	language string
	server   string
	checked  []string
}

func (e *languageServerUnavailableError) Error() string {
	return e.server + " was not found"
}

func defaultGoServerResolver(languageRoot string) (lsp.ServerSpec, error) {
	const server = "gopls"
	executable, err := exec.LookPath(server)
	if err != nil {
		return lsp.ServerSpec{}, &languageServerUnavailableError{
			language: "go",
			server:   server,
			checked:  []string{"PATH:gopls"},
		}
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return lsp.ServerSpec{}, err
	}
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	env, err := commandEnvironment(map[string]string{
		"GOPROXY":     "off",
		"GOTOOLCHAIN": "local",
	})
	if err != nil {
		return lsp.ServerSpec{}, err
	}
	return lsp.ServerSpec{
		Key:     lsp.ProcessKey{LanguageRoot: languageRoot, ServerKind: server},
		Command: executable,
		Args:    []string{"serve"},
		Dir:     languageRoot,
		Env:     env,
	}, nil
}

func (r *BuiltinRunner) resolveCodeTarget(call Call, rawPath, language string, allowDirectory bool) (resolvedCodeTarget, error) {
	language = strings.ToLower(strings.TrimSpace(language))
	if language != "" && language != "go" {
		return resolvedCodeTarget{}, fmt.Errorf("language_not_supported: %s", language)
	}
	root, target, _, err := resolveProjectPath(call.ProjectDirs, rawPath, allowDirectory, false)
	if err != nil {
		return resolvedCodeTarget{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return resolvedCodeTarget{}, err
	}
	if info.IsDir() {
		if !allowDirectory {
			return resolvedCodeTarget{}, errors.New("target must be a Go source file")
		}
	} else if !info.Mode().IsRegular() {
		return resolvedCodeTarget{}, errors.New("target must be a regular file or directory")
	} else if filepath.Ext(target) != ".go" {
		return resolvedCodeTarget{}, errors.New("language_not_supported: target is not a Go source file")
	}
	if language == "" {
		language = "go"
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return resolvedCodeTarget{}, err
	}
	resolvedRoot = filepath.Clean(resolvedRoot)
	start := target
	if !info.IsDir() {
		start = filepath.Dir(target)
	}
	languageRoot, fallback := resolveGoLanguageRoot(start, resolvedRoot)
	resolver := r.goServerResolver
	if resolver == nil {
		resolver = defaultGoServerResolver
	}
	spec, err := resolver(languageRoot)
	if err != nil {
		return resolvedCodeTarget{}, err
	}
	if filepath.Clean(spec.Key.LanguageRoot) != languageRoot || spec.Key.ServerKind != "gopls" {
		return resolvedCodeTarget{}, errors.New("Go server resolver returned an invalid process key")
	}
	return resolvedCodeTarget{
		path:         target,
		language:     language,
		languageRoot: languageRoot,
		rootFallback: fallback,
		spec:         spec,
	}, nil
}

func resolveGoLanguageRoot(start, projectRoot string) (string, bool) {
	if root := findMarkerRoot(start, projectRoot, "go.work"); root != "" {
		return root, false
	}
	if root := findMarkerRoot(start, projectRoot, "go.mod"); root != "" {
		return root, false
	}
	return projectRoot, true
}

func findMarkerRoot(start, stop, marker string) string {
	start = filepath.Clean(start)
	stop = filepath.Clean(stop)
	if !pathInsideRoot(start, stop) {
		return ""
	}
	for dir := start; ; dir = filepath.Dir(dir) {
		if info, err := os.Stat(filepath.Join(dir, marker)); err == nil && info.Mode().IsRegular() {
			return dir
		}
		if dir == stop {
			return ""
		}
		next := filepath.Dir(dir)
		if next == dir || !pathInsideRoot(next, stop) {
			return ""
		}
	}
}

func readCodeDocument(path string) (string, []string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, errors.New("target must be a regular file")
	}
	if info.Size() > maxCodeDocumentBytes {
		return "", nil, errors.New("document_too_large")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	if !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
		return "", nil, errors.New("document must be UTF-8 text")
	}
	text := string(data)
	return text, strings.Split(text, "\n"), nil
}

func codeFileURI(path string) string {
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func codePathFromURI(rawURI string) (string, bool) {
	u, err := url.Parse(rawURI)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
		return "", false
	}
	path := filepath.FromSlash(u.Path)
	if runtime.GOOS == "windows" && len(path) >= 3 && path[0] == filepath.Separator && path[2] == ':' {
		path = path[1:]
	}
	if !filepath.IsAbs(path) {
		return "", false
	}
	return filepath.Clean(path), true
}

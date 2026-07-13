package browser

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/teatak/pudding-core/internal/projectpath"
	"github.com/teatak/pudding-core/internal/store"
)

var ErrFileURLNotAllowed = errors.New("file URL is outside the session project")

type AuthorizedURL struct {
	URL      string
	FileRoot string
}

type FileURLAuthorizer func(ctx context.Context, sessionID string, parsed *url.URL) (AuthorizedURL, error)

type ProjectFileScope interface {
	GetSession(ctx context.Context, id string) (*store.Session, error)
	GetProject(ctx context.Context, id string) (*store.Project, error)
}

func ProjectFileURLAuthorizer(scope ProjectFileScope) FileURLAuthorizer {
	return func(ctx context.Context, sessionID string, parsed *url.URL) (AuthorizedURL, error) {
		if scope == nil || parsed == nil || parsed.Scheme != "file" || (parsed.Host != "" && parsed.Host != "localhost") {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		session, err := scope.GetSession(ctx, strings.TrimSpace(sessionID))
		if err != nil || strings.TrimSpace(session.ProjectID) == "" {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		project, err := scope.GetProject(ctx, session.ProjectID)
		if err != nil {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		rawPath := filepath.FromSlash(parsed.Path)
		if runtime.GOOS == "windows" && len(rawPath) >= 3 && (rawPath[0] == '/' || rawPath[0] == '\\') && rawPath[2] == ':' {
			rawPath = rawPath[1:]
		}
		root, target, _, err := projectpath.Resolve(project.RootDirs, rawPath, false, false)
		if err != nil {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		canonical := (&url.URL{Scheme: "file", Path: filepath.ToSlash(target), RawQuery: parsed.RawQuery, Fragment: parsed.Fragment}).String()
		return AuthorizedURL{URL: canonical, FileRoot: resolvedRoot}, nil
	}
}

func fileURLAllowed(rawURL string, roots []string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	switch parsed.Scheme {
	case "http", "https":
		return parsed.Host != ""
	case "about":
		return parsed.String() == "about:blank"
	case "file":
		// Continue with project root validation below.
	default:
		return false
	}
	if parsed.Host != "" && parsed.Host != "localhost" {
		return false
	}
	rawPath := filepath.FromSlash(parsed.Path)
	if runtime.GOOS == "windows" && len(rawPath) >= 3 && (rawPath[0] == '/' || rawPath[0] == '\\') && rawPath[2] == ':' {
		rawPath = rawPath[1:]
	}
	target, err := filepath.EvalSymlinks(rawPath)
	if err != nil {
		return false
	}
	for _, root := range roots {
		resolvedRoot, err := filepath.EvalSymlinks(root)
		if err == nil && projectpath.Inside(target, resolvedRoot) {
			return true
		}
	}
	return false
}

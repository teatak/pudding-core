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
		urlPath := filepath.ToSlash(target)
		if !strings.HasPrefix(urlPath, "/") {
			urlPath = "/" + urlPath
		}
		canonical := (&url.URL{Scheme: "file", Path: urlPath, RawQuery: parsed.RawQuery, Fragment: parsed.Fragment}).String()
		return AuthorizedURL{URL: canonical, FileRoot: resolvedRoot}, nil
	}
}

func normalizeURL(raw string) (string, error) {
	authorized, err := normalizeAuthorizedURL(context.Background(), "", raw, nil)
	return authorized.URL, err
}

func normalizeAuthorizedURL(ctx context.Context, sessionID, raw string, fileURLs FileURLAuthorizer) (AuthorizedURL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return AuthorizedURL{}, errors.New("url is required")
	}
	if raw == "about:blank" {
		return AuthorizedURL{URL: raw}, nil
	}
	if !strings.Contains(raw, "://") {
		scheme := "https"
		if strings.HasPrefix(raw, "localhost") || strings.HasPrefix(raw, "127.") || strings.HasPrefix(raw, "[::1]") {
			scheme = "http"
		}
		raw = scheme + "://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return AuthorizedURL{}, err
	}
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return AuthorizedURL{}, errors.New("url host is required")
		}
		return AuthorizedURL{URL: u.String()}, nil
	case "file":
		if fileURLs == nil || u.User != nil {
			return AuthorizedURL{}, ErrFileURLNotAllowed
		}
		return fileURLs(ctx, sessionID, u)
	default:
		return AuthorizedURL{}, errors.New("only http, https and authorized file URLs are supported")
	}
}

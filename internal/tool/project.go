package tool

import (
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/teatak/pudding-core/internal/projectpath"
)

type invalidScopeError struct {
	Field   string
	Allowed []string
}

func (e *invalidScopeError) Error() string {
	return e.Field + " must be one of: " + strings.Join(e.Allowed, ", ")
}

var (
	errProjectDirsRequired         = projectpath.ErrRootsRequired
	errProjectAbsolutePathRequired = projectpath.ErrAbsolutePathRequired
	errProjectPathNotAllowed       = projectpath.ErrPathNotAllowed
	errProjectFilePathRequired     = projectpath.ErrFileRequired
)

func projectPathErrorReason(err error) string {
	switch {
	case errors.Is(err, errProjectDirsRequired):
		return "project_dirs_required"
	case errors.Is(err, errProjectAbsolutePathRequired):
		return "absolute_path_required"
	case errors.Is(err, errProjectPathNotAllowed):
		return "path_not_authorized"
	case errors.Is(err, errProjectFilePathRequired):
		return "path_not_allowed"
	case errors.Is(err, os.ErrNotExist):
		return "path_not_found"
	default:
		return "path_unavailable"
	}
}

func projectPathErrorRoots(err error) []string {
	var required *projectpath.AbsolutePathRequiredError
	if errors.As(err, &required) {
		return required.Roots
	}
	return nil
}

func normalizeProjectDirs(dirs []string) []string {
	return projectpath.NormalizeRoots(dirs)
}

func resolveProjectPath(roots []string, rawPath string, allowRoot, allowMissing bool) (string, string, string, error) {
	return projectpath.Resolve(roots, rawPath, allowRoot, allowMissing)
}

func pathInsideRoot(path, root string) bool {
	return projectpath.Inside(path, root)
}

func resolveExistingParent(path string) (string, error) {
	return projectpath.ResolveExistingParent(path)
}

func jsonString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `{"ok":false,"reason":"encode_error"}`
	}
	return string(b)
}

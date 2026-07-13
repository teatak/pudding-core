package tool

import (
	"encoding/json"

	"github.com/teatak/pudding-core/internal/projectpath"
)

var (
	errProjectDirsRequired     = projectpath.ErrRootsRequired
	errProjectPathNotAllowed   = projectpath.ErrPathNotAllowed
	errProjectFilePathRequired = projectpath.ErrFileRequired
)

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

package tool

import (
	"os"
	"path/filepath"
)

// Project-local Python libraries and virtualenv metadata do not extend the
// authority of code already allowed inside this sandbox. Keep every other
// sensitive environment override subject to the existing approval policy.
func commandEnvironmentRequiresApprovalForProject(env map[string]string, roots []string) bool {
	for key, value := range env {
		if !commandEnvironmentRequiresApproval(map[string]string{key: value}) {
			continue
		}
		if (key != "PYTHONPATH" && key != "VIRTUAL_ENV") || value == "" {
			return true
		}
		paths := []string{value}
		if key == "PYTHONPATH" {
			paths = filepath.SplitList(value)
		}
		for _, path := range paths {
			if !filepath.IsAbs(path) {
				return true
			}
			_, resolved, _, err := resolveProjectPath(roots, path, true, false)
			if err != nil {
				return true
			}
			info, err := os.Stat(resolved)
			if err != nil || !info.IsDir() {
				return true
			}
		}
	}
	return false
}

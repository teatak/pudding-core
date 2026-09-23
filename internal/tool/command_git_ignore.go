package tool

import (
	"os"
	"path/filepath"
)

// Authorize only the inherited default ignore file. A tool-supplied HOME/XDG
// override must not turn this grant into a way to read another external file.
type sandboxReadFile struct {
	path   string
	lookup string
}

func inheritedGitIgnoreFiles(inherited, effective []string) []sandboxReadFile {
	path := defaultGitIgnorePath(inherited)
	if path == "" || path != defaultGitIgnorePath(effective) {
		return nil
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return []sandboxReadFile{{path: resolved, lookup: path}}
}

func defaultGitIgnorePath(env []string) string {
	config := executableEnvValue(env, "XDG_CONFIG_HOME")
	if config == "" {
		home := executableEnvValue(env, "HOME")
		if home == "" {
			return ""
		}
		config = filepath.Join(home, ".config")
	}
	if !filepath.IsAbs(config) {
		return ""
	}
	return filepath.Join(config, "git", "ignore")
}

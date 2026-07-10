package tool

import (
	"os"
	"path/filepath"
	"runtime"
)

const bundledLanguageServersDir = "language-servers"

func bundledLanguageServerPath(server string) string {
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	return bundledLanguageServerPathForExecutable(executable, server)
}

func bundledLanguageServerPathForExecutable(daemonExecutable, server string) string {
	if daemonExecutable == "" || server == "" {
		return ""
	}
	name := bundledLanguageServerName(server, runtime.GOOS)
	daemonDir := filepath.Dir(filepath.Clean(daemonExecutable))
	candidates := []string{
		filepath.Join(daemonDir, bundledLanguageServersDir, name),
		filepath.Join(filepath.Dir(daemonDir), bundledLanguageServersDir, name),
	}
	for _, candidate := range candidates {
		if !isExecutableFile(candidate) {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			candidate = resolved
		}
		return filepath.Clean(candidate)
	}
	return ""
}

func bundledLanguageServerName(server, goos string) string {
	name := server
	if goos == "windows" {
		if server == "gopls" {
			name += ".exe"
		} else {
			name += ".cmd"
		}
	}
	return name
}

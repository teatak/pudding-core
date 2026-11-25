//go:build darwin

package tool

import (
	"errors"
	"go/version"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Reuse read-only host downloads while keeping Go's writable caches in the
// project state. Named toolchain executables let Go perform its usual version
// selection without downloading and unpacking an already installed toolchain.
func prepareSandboxGoEnvironment(env []string, stateRoot, moduleCache string) ([]string, error) {
	out := append([]string(nil), env...)
	archives := filepath.Join(moduleCache, "cache", "download")
	if info, err := os.Stat(archives); err == nil && info.IsDir() && sandboxEnvValue(out, "GOPROXY") == "" && sandboxEnvValue(out, "GOENV") == "" {
		proxy := (&url.URL{Scheme: "file", Path: archives}).String()
		out = sandboxSetEnv(out, "GOPROXY", proxy+",https://proxy.golang.org,direct")
	}
	entries, err := os.ReadDir(filepath.Join(moduleCache, "golang.org"))
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	bin := filepath.Join(stateRoot, "go", "toolchains")
	linked := false
	for _, entry := range entries {
		name, hasPrefix := strings.CutPrefix(entry.Name(), "toolchain@v0.0.1-")
		name, hasSuffix := strings.CutSuffix(name, "."+runtime.GOOS+"-"+runtime.GOARCH)
		if !entry.IsDir() || !hasPrefix || !hasSuffix || !version.IsValid(name) {
			continue
		}
		target := filepath.Join(moduleCache, "golang.org", entry.Name(), "bin", "go")
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&0111 == 0 {
			continue
		}
		for _, dir := range []string{filepath.Dir(bin), bin} {
			if err := ensureSandboxStateDir(dir); err != nil {
				return nil, err
			}
		}
		link := filepath.Join(bin, name)
		if err := os.Symlink(target, link); err != nil {
			if existing, readErr := os.Readlink(link); readErr != nil || existing != target {
				return nil, err
			}
		}
		linked = true
	}
	if !linked {
		return out, nil
	}
	// Existing version-specific executables retain their PATH precedence.
	return sandboxSetEnv(out, "PATH", sandboxEnvValue(out, "PATH")+string(os.PathListSeparator)+bin), nil
}

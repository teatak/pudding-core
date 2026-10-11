//go:build darwin

package tool

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestMacOSToolchainEnvironmentUsesSelectedLayoutAndPreservesOverrides(t *testing.T) {
	for _, xcode := range []bool{false, true} {
		root := t.TempDir()
		developer := filepath.Join(root, "Developer with spaces")
		sdkDir := filepath.Join(developer, "SDKs")
		if xcode {
			sdkDir = filepath.Join(developer, "Platforms", "MacOSX.platform", "Developer", "SDKs")
		}
		sdk := filepath.Join(sdkDir, "MacOSX27.0.sdk")
		general := filepath.Join(developer, "usr", "bin")
		compiler := filepath.Join(developer, "Toolchains", "XcodeDefault.xctoolchain", "usr", "bin")
		dirs := []string{sdk, general}
		if xcode {
			dirs = append(dirs, compiler)
		}
		for _, dir := range dirs {
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(sdk, filepath.Join(sdkDir, "MacOSX.sdk")); err != nil {
			t.Fatal(err)
		}
		input := []string{"DEVELOPER_DIR=" + developer, "PATH=/custom/bin:/usr/bin:/bin", "CC=/custom/clang", "CXX=/custom/clang++"}
		snapshot := slices.Clone(input)
		env, err := platformCommandEnvironment(input)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(input, snapshot) || executableEnvValue(env, "SDKROOT") != mustResolveToolchainPath(t, sdk) || executableEnvValue(env, "DEVELOPER_DIR") != developer || executableEnvValue(env, "CC") != "/custom/clang" || executableEnvValue(env, "CXX") != "/custom/clang++" {
			t.Fatalf("toolchain defaults mutated explicit configuration: %q", env)
		}
		wantDirs := []string{"/custom/bin"}
		if xcode {
			wantDirs = append(wantDirs, compiler)
		}
		wantPath := strings.Join(append(wantDirs, general, "/usr/bin", "/bin"), ":")
		if executableEnvValue(env, "PATH") != wantPath {
			t.Fatalf("PATH=%q want %q", executableEnvValue(env, "PATH"), wantPath)
		}
		explicit := append(slices.Clone(input), "SDKROOT=/explicit/sdk", "TOOLCHAINS=custom")
		env, err = platformCommandEnvironment(explicit)
		if err != nil {
			t.Fatal(err)
		}
		if executableEnvValue(env, "SDKROOT") != "/explicit/sdk" || strings.Contains(executableEnvValue(env, "PATH"), compiler) {
			t.Fatalf("explicit SDK/toolchain was replaced: %q", env)
		}
	}
}

func TestMacOSToolchainEnvironmentRejectsInvalidExplicitDeveloper(t *testing.T) {
	if _, err := platformCommandEnvironment([]string{"DEVELOPER_DIR=" + filepath.Join(t.TempDir(), "missing")}); err == nil {
		t.Fatal("invalid explicit developer directory must not select another toolchain")
	}
}

func TestMacOSToolchainDiscoveryDoesNotRunCallerToolsOnHost(t *testing.T) {
	root := t.TempDir()
	marker := filepath.Join(root, "executed")
	if err := os.WriteFile(filepath.Join(root, "xcode-select"), []byte("#!/bin/sh\ntouch "+quoteShellArg(marker)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := platformCommandEnvironment([]string{"PATH=" + root + ":/usr/bin", "DYLD_INSERT_LIBRARIES=/missing/caller.dylib"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("discovery executed a caller's host program: %v", err)
	}
}

func TestMacOSCommandSandboxCompilesWithConsistentDefaultToolchain(t *testing.T) {
	project := t.TempDir()
	env, err := commandEnvironment(map[string]string{"SDKROOT": "", "DEVELOPER_DIR": ""})
	if err != nil {
		t.Fatal(err)
	}
	if executableEnvValue(env, "SDKROOT") == "" {
		t.Skip("Apple SDK is not installed")
	}
	if err := os.WriteFile(filepath.Join(project, "probe.c"), []byte("int main(void) { return 0; }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"git --version", "clang probe.c -o probe && ./probe"} {
		result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
			Executable: "/bin/sh", Args: []string{"-c", command}, CWD: project, Env: env, ProjectDirs: []string{project},
		})
		if result.exitCode != 0 || strings.Contains(result.stderr, "xcrun_db") || strings.Contains(result.stderr, "unknown architecture") || strings.Contains(result.stderr, "malformed file") || strings.Contains(strings.ToLower(result.stderr), "operation not permitted") {
			t.Fatalf("default toolchain failed: %s: exit=%d stdout=%q stderr=%q", command, result.exitCode, result.stdout, result.stderr)
		}
	}
}

func TestMacOSCommandSandboxPackageManagersUsePrivateStateWithoutNPMWarnings(t *testing.T) {
	home, project := t.TempDir(), t.TempDir()
	env := mustCommandEnvironment(t)
	for _, command := range []struct {
		executable string
		args       []string
	}{
		{"npm", []string{"--version"}}, {"pnpm", []string{"store", "path"}},
	} {
		// Exercise the installed CLI, not a captured login shell's Corepack
		// download shim. Configuration isolation must not depend on a registry.
		executable, err := exec.LookPath(command.executable)
		if err != nil {
			t.Skipf("%s is not installed", command.executable)
		}
		if command.executable == "pnpm" {
			executable = installedPNPMForTest(t, project)
		}
		resolvedProject, err := filepath.EvalSymlinks(project)
		if err != nil {
			t.Fatal(err)
		}
		requireTrustedSandboxCommand(t, executable, resolvedProject, env)
		result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(home), commandSpec{Executable: executable, Args: command.args, CWD: project, Env: env, ProjectDirs: []string{project}})
		if result.exitCode != 0 || strings.Contains(result.stderr, "Unknown env config") {
			t.Fatalf("package manager configuration failed: %s: stdout=%q stderr=%q", command.executable, result.stdout, result.stderr)
		}
		if command.executable == "pnpm" {
			resolvedHome, err := filepath.EvalSymlinks(home)
			if err != nil {
				t.Fatal(err)
			}
			store := filepath.Clean(strings.TrimSpace(result.stdout))
			if !pathInsideRoot(store, resolvedHome) {
				t.Fatalf("pnpm store escaped private state: %q", store)
			}
		}
	}
	if got := sandboxEnvValue(sandboxEnvironment(env, t.TempDir()), "npm_config_store_dir"); got != "" {
		t.Fatalf("sandbox injected a pnpm-only npm setting: %q", got)
	}
}

func installedPNPMForTest(t *testing.T, project string) string {
	t.Helper()
	for _, dir := range filepath.SplitList(mergedExecutablePATH(os.Getenv("PATH"))) {
		entry, err := filepath.EvalSymlinks(filepath.Join(dir, "pnpm"))
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(entry)), "package.json"))
		var pkg struct{ Name string }
		if err == nil && json.Unmarshal(data, &pkg) == nil && pkg.Name == "pnpm" {
			return entry
		}
	}
	cache := os.Getenv("COREPACK_HOME")
	if cache == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		cache = filepath.Join(home, ".cache", "node", "corepack")
	}
	data, err := os.ReadFile(filepath.Join(cache, "lastKnownGood.json"))
	var active map[string]string
	if err != nil || json.Unmarshal(data, &active) != nil || active["pnpm"] == "" {
		t.Skip("pnpm is not installed in the Corepack cache")
	}
	version := strings.Split(active["pnpm"], "+")[0]
	if filepath.Base(version) != version {
		t.Fatal("invalid cached pnpm version")
	}
	root := filepath.Join(cache, "v1", "pnpm", version)
	data, err = os.ReadFile(filepath.Join(root, "package.json"))
	var pkg struct {
		Name string
		Bin  map[string]string
	}
	if err != nil || json.Unmarshal(data, &pkg) != nil || pkg.Name != "pnpm" {
		t.Skip("active cached pnpm package is unavailable")
	}
	// The cache is intentionally outside product sandbox grants. Copy public
	// CLI files into the temporary project rather than widening those grants.
	fixture := filepath.Join(project, "pnpm-fixture")
	if err := os.CopyFS(fixture, os.DirFS(root)); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(fixture, pkg.Bin["pnpm"])
	if !pathInsideRoot(entry, fixture) {
		t.Fatal("pnpm bin is outside the fixture")
	}
	return entry
}

func mustResolveToolchainPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

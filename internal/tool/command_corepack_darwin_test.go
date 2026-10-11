//go:build darwin

package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSandboxCorepackDefaultsPreserveExplicitConfiguration(t *testing.T) {
	for _, explicit := range []string{"", "0", "1"} {
		input := []string{"COREPACK_DEFAULT_TO_LATEST=" + explicit, "COREPACK_ENABLE_NETWORK=0"}
		env := sandboxEnvironment(input, t.TempDir())
		want := explicit
		if want == "" {
			want = "0"
		}
		if got := sandboxEnvValue(env, "COREPACK_DEFAULT_TO_LATEST"); got != want {
			t.Fatalf("latest default=%q want %q", got, want)
		}
		if input[0] != "COREPACK_DEFAULT_TO_LATEST="+explicit || sandboxEnvValue(env, "COREPACK_ENABLE_NETWORK") != "0" {
			t.Fatal("explicit configuration was mutated")
		}
	}
}

// Exercise the installed shim's version selection with an empty private cache.
// Network is explicitly disabled, so a request for /latest proves a regression;
// a versioned tarball proves the bundled or project version was selected. The
// separate real-CLI test covers pnpm's private store configuration.
func TestMacOSCommandSandboxCorepackColdCacheHonorsVersionSelection(t *testing.T) {
	corepack, err := exec.LookPath("corepack")
	if err != nil {
		t.Skip("Corepack is not installed")
	}
	for _, pin := range []string{"", "pnpm@10.33.2"} {
		home, project := t.TempDir(), t.TempDir()
		if pin != "" {
			if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(`{"packageManager":"`+pin+`"}`), 0600); err != nil {
				t.Fatal(err)
			}
		}
		env, err := commandEnvironment(map[string]string{
			"COREPACK_DEFAULT_TO_LATEST": "", "COREPACK_ENABLE_NETWORK": "0", "COREPACK_ENABLE_DOWNLOAD_PROMPT": "0",
		})
		if err != nil {
			t.Fatal(err)
		}
		resolvedProject := mustResolveToolchainPath(t, project)
		requireTrustedSandboxCommand(t, corepack, resolvedProject, env)
		result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(home), commandSpec{
			Executable: corepack, Args: []string{"pnpm", "store", "path"}, CWD: project, Env: env, ProjectDirs: []string{project},
		})
		if result.exitCode == 0 || !strings.Contains(result.stderr, "Network access disabled") || strings.Contains(result.stderr, "/latest") || !strings.Contains(result.stderr, "/pnpm-") || !strings.Contains(result.stderr, ".tgz") {
			t.Fatalf("cold cache selected latest instead of a known version: pin=%q exit=%d stderr=%q", pin, result.exitCode, result.stderr)
		}
		if pin != "" && !strings.Contains(result.stderr, "/pnpm-10.33.2.tgz") {
			t.Fatalf("project packageManager was replaced: %q", result.stderr)
		}
		if strings.Contains(result.stderr, "Operation not permitted") || strings.Contains(result.stderr, "sandbox: deny") {
			t.Fatalf("version selection failed at the sandbox boundary: %q", result.stderr)
		}
	}
}

//go:build darwin

package tool

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teatak/pudding-core/internal/home"
)

func TestMacOSCommandSandboxOnlyGrantsCurrentSessionArtifacts(t *testing.T) {
	homeDir, project := t.TempDir(), t.TempDir()
	paths := map[string]string{}
	for _, sessionID := range []string{"session-a", "session-b"} {
		root, path, err := home.OpenSessionArtifacts(homeDir, sessionID)
		if err != nil {
			t.Fatal(err)
		}
		if err := root.WriteFile("data.txt", []byte(sessionID), 0o600); err != nil {
			t.Fatal(err)
		}
		root.Close()
		paths[sessionID] = filepath.Join(path, "data.txt")
	}
	globalTemp := filepath.Join(home.TempPath(homeDir), "unrelated.txt")
	if err := os.WriteFile(globalTemp, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	otherScratch, err := home.PrepareCodeScratch(homeDir, "session-b")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherScratch, "private.txt"), []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := newPlatformCommandRunner(homeDir)
	spec := commandSpec{SessionID: "session-a", Executable: "/bin/sh", Args: []string{"-c", `cat "$1"; printf analyzed > "$2"`, "sh", paths["session-a"], filepath.Join(filepath.Dir(paths["session-a"]), "analysis.txt")}, CWD: project, Env: mustCommandEnvironment(t), ProjectDirs: []string{project}}
	result := runMacOSSandboxTestCommand(t, runner, spec)
	if result.exitCode != 0 || strings.TrimSpace(result.stdout) != "session-a" {
		t.Fatalf("current artifacts unavailable: %+v", result)
	}
	for _, path := range []string{paths["session-b"], globalTemp, filepath.Join(otherScratch, "private.txt")} {
		spec.Executable, spec.Args = "/bin/cat", []string{path}
		result := runMacOSSandboxTestCommand(t, runner, spec)
		if result.exitCode == 0 {
			t.Fatalf("other area was authorized: %s %+v", path, result)
		}
	}
	link := filepath.Join(filepath.Dir(paths["session-a"]), "other-link")
	if err := os.Symlink(paths["session-b"], link); err != nil {
		t.Fatal(err)
	}
	spec.Executable, spec.Args = "/bin/cat", []string{link}
	if result := runMacOSSandboxTestCommand(t, runner, spec); result.exitCode == 0 {
		t.Fatalf("artifact symlink escaped: %+v", result)
	}
}

func TestMacOSCommandSandboxPreservesDefaultGitIgnoreWithoutDirectoryGrant(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	userHome, project, homeDir := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", userHome)
	saved := commandEnvironmentSnapshot()
	setCapturedCommandEnvironment(nil)
	t.Cleanup(func() { setCapturedCommandEnvironment(saved) })
	ignore := filepath.Join(userHome, ".config", "git", "ignore")
	if err := os.MkdirAll(filepath.Dir(ignore), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignore, []byte("*.ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(filepath.Dir(ignore), "private.txt")
	if err := os.WriteFile(private, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("git", "init", "--quiet", project).CombinedOutput(); err != nil {
		t.Fatalf("initialize Git fixture: %v: %s", err, output)
	}
	for _, name := range []string{"one.ignored", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(project, name), []byte("data"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runner := newPlatformCommandRunner(homeDir)
	spec := commandSpec{Executable: "git", CWD: project, Env: mustCommandEnvironment(t), ProjectDirs: []string{project}}
	for _, tc := range []struct {
		name string
		exit int
	}{{"one.ignored", 0}, {"keep.txt", 1}} {
		spec.Args = []string{"check-ignore", tc.name}
		result := runMacOSSandboxTestCommand(t, runner, spec)
		if result.exitCode != tc.exit || strings.Contains(result.stderr, ignore) {
			t.Fatalf("default ignore semantics/noise changed for %s: %+v", tc.name, result)
		}
	}
	spec.Args = []string{"status", "--short"}
	status := runMacOSSandboxTestCommand(t, runner, spec)
	if status.exitCode != 0 || strings.Contains(status.stdout, "one.ignored") || !strings.Contains(status.stdout, "keep.txt") || strings.Contains(status.stderr, ignore) {
		t.Fatalf("git status lost ignore rules: %+v", status)
	}
	spec.Executable, spec.Args = "/bin/cat", []string{private}
	if result := runMacOSSandboxTestCommand(t, runner, spec); result.exitCode == 0 {
		t.Fatalf("ignore grant exposed parent directory: %+v", result)
	}
}

func TestSandboxProfileUsesLiteralIgnoreFileGrants(t *testing.T) {
	lookup := filepath.Join(t.TempDir(), ".config", "git", "ignore")
	file := filepath.Join(t.TempDir(), "actual-ignore")
	profile, definitions := sandboxProfile(nil, nil, []sandboxReadFile{{path: file, lookup: lookup}})
	if !strings.Contains(profile, `(literal (param "READ_FILE_0"))`) || strings.Contains(profile, `(subpath (param "READ_FILE_0"))`) {
		t.Fatalf("ignore file is not a literal grant: %s", profile)
	}
	for _, definition := range definitions {
		if strings.HasPrefix(definition.key, "READ_FILE") && definition.path != file {
			t.Fatalf("noncanonical file got a data grant: %+v", definition)
		}
		if strings.HasPrefix(definition.key, "WRITE") || strings.HasPrefix(definition.key, "READ_ROOT") {
			t.Fatalf("ignore granted a writable path/directory: %+v", definition)
		}
	}
}

//go:build darwin

package tool

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMacOSCommandSandboxProjectBoundary(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "project with spaces")
	if err := os.Mkdir(project, 0o700); err != nil {
		t.Fatal(err)
	}
	secondProject := t.TempDir()
	secondFile := filepath.Join(secondProject, "shared.txt")
	if err := os.WriteFile(secondFile, []byte("shared"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	runner := newPlatformCommandRunner(home)
	projectResult := runMacOSSandboxTestCommand(t, runner, commandSpec{
		Executable:  "/bin/sh",
		Args:        []string{"-c", `printf project > created.txt && cat created.txt && cat "$1"`, "sh", secondFile},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project, secondProject},
	})
	if projectResult.exitCode != 0 || strings.TrimSpace(projectResult.stdout) != "projectshared" {
		t.Fatalf("project command failed: exit=%d err=%v stdout=%q stderr=%q", projectResult.exitCode, projectResult.err, projectResult.stdout, projectResult.stderr)
	}

	denied := runMacOSSandboxTestCommand(t, runner, commandSpec{
		Executable:  "/bin/cat",
		Args:        []string{outside},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if denied.exitCode == 0 || !commandSandboxDenied(denied.stdout+denied.stderr, denied.err) {
		t.Fatalf("outside read was not denied: %+v", denied)
	}

	link := filepath.Join(project, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	deniedLink := runMacOSSandboxTestCommand(t, runner, commandSpec{
		Executable:  "/bin/cat",
		Args:        []string{link},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if deniedLink.exitCode == 0 {
		t.Fatalf("symlink escape was not denied: %+v", deniedLink)
	}

	deniedHardLink := runMacOSSandboxTestCommand(t, runner, commandSpec{
		Executable:  "/bin/sh",
		Args:        []string{"-c", `ln "$1" outside-hard-link && cat outside-hard-link`, "sh", outside},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if deniedHardLink.exitCode == 0 || !commandSandboxDenied(deniedHardLink.stdout+deniedHardLink.stderr, deniedHardLink.err) {
		t.Fatalf("hard-link escape was not denied: %+v", deniedHardLink)
	}

	externalExecutable := filepath.Join(filepath.Dir(outside), "outside-command")
	if err := os.WriteFile(externalExecutable, []byte("#!/bin/sh\nprintf outside"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Prepare(commandSpec{
		Executable:  externalExecutable,
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	}); err == nil {
		t.Fatal("external executable should require full access")
	}
}

func TestMacOSCommandSandboxFullBypassesBoundary(t *testing.T) {
	project := t.TempDir()
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "/bin/cat",
		Args:        []string{outside},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
		SandboxMode: CommandSandboxBypass,
	})
	if result.exitCode != 0 || strings.TrimSpace(result.stdout) != "private" || result.sandboxed {
		t.Fatalf("full access did not bypass the sandbox: %+v", result)
	}
}

func TestMacOSCommandSandboxAllowsLoopbackServer(t *testing.T) {
	if os.Getenv("PUDDING_SANDBOX_LOOPBACK_HELPER") == "1" {
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		accepted := make(chan error, 1)
		go func() {
			connection, acceptErr := listener.Accept()
			if acceptErr == nil {
				_ = connection.Close()
			}
			accepted <- acceptErr
		}()
		connection, err := net.DialTimeout("tcp4", fmt.Sprintf("localhost:%d", port), time.Second)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = listener.Close()
			os.Exit(3)
		}
		_ = connection.Close()
		if err := <-accepted; err != nil {
			fmt.Fprintln(os.Stderr, err)
			_ = listener.Close()
			os.Exit(4)
		}
		fmt.Fprintln(os.Stdout, listener.Addr().String())
		_ = listener.Close()
		os.Exit(0)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	executable = copySandboxTestExecutable(t, executable, project)
	env := mustCommandEnvironment(t)
	env = append(env, "PUDDING_SANDBOX_LOOPBACK_HELPER=1")
	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  executable,
		Args:        []string{"-test.run=^TestMacOSCommandSandboxAllowsLoopbackServer$"},
		CWD:         project,
		Env:         env,
		ProjectDirs: []string{project},
	})
	if result.exitCode != 0 || !strings.Contains(result.stdout, "127.0.0.1:") {
		t.Fatalf("loopback server failed: exit=%d err=%v stdout=%q stderr=%q", result.exitCode, result.err, result.stdout, result.stderr)
	}
}

func TestMacOSCommandSandboxAllowsExternalNetwork(t *testing.T) {
	if os.Getenv("PUDDING_SANDBOX_EXTERNAL_NETWORK_HELPER") == "1" {
		connection, err := net.Dial("udp4", "1.1.1.1:53")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		_ = connection.Close()
		os.Exit(0)
	}

	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	executable = copySandboxTestExecutable(t, executable, project)
	env := mustCommandEnvironment(t)
	env = append(env, "PUDDING_SANDBOX_EXTERNAL_NETWORK_HELPER=1")
	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  executable,
		Args:        []string{"-test.run=^TestMacOSCommandSandboxAllowsExternalNetwork$"},
		CWD:         project,
		Env:         env,
		ProjectDirs: []string{project},
	})
	if result.exitCode != 0 {
		t.Fatalf("external network was not allowed: %+v", result)
	}
}

func TestMacOSCommandSandboxRunsGoTest(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go is not installed")
	}
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, "go.mod"), []byte("module sandbox.example/test\n\ngo 1.25\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, "sandbox_test.go"), []byte("package sandbox\n\nimport \"testing\"\n\nfunc TestSandbox(t *testing.T) {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "go",
		Args:        []string{"test", "./..."},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if result.exitCode != 0 {
		t.Fatalf("go test failed in sandbox: err=%v stdout=%q stderr=%q", result.err, result.stdout, result.stderr)
	}
}

func TestMacOSCommandSandboxRunsNPMProjectScript(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm is not installed")
	}
	project := t.TempDir()
	packageJSON := `{"private":true,"scripts":{"check":"node -e \"require('fs').writeFileSync('built.txt','ok')\""}}`
	if err := os.WriteFile(filepath.Join(project, "package.json"), []byte(packageJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "npm",
		Args:        []string{"run", "--silent", "check"},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if result.exitCode != 0 {
		t.Fatalf("npm project script failed in sandbox: err=%v stdout=%q stderr=%q", result.err, result.stdout, result.stderr)
	}
	if content, err := os.ReadFile(filepath.Join(project, "built.txt")); err != nil || string(content) != "ok" {
		t.Fatalf("npm project script did not write project output: content=%q err=%v", content, err)
	}
}

func TestMacOSCommandSandboxRunsPythonTest(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is not installed")
	}
	project := t.TempDir()
	testSource := "import unittest\n\nclass SandboxTest(unittest.TestCase):\n    def test_ok(self):\n        self.assertEqual(2 + 2, 4)\n"
	if err := os.WriteFile(filepath.Join(project, "test_sandbox.py"), []byte(testSource), 0o600); err != nil {
		t.Fatal(err)
	}
	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "python3",
		Args:        []string{"-m", "unittest", "test_sandbox.py"},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if result.exitCode != 0 {
		t.Fatalf("python test failed in sandbox: err=%v stdout=%q stderr=%q", result.err, result.stdout, result.stderr)
	}
}

func TestMacOSCommandSandboxRunsGitStatusWithoutUserConfig(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	project := t.TempDir()
	initCommand := exec.Command("git", "init", "--quiet", project)
	if output, err := initCommand.CombinedOutput(); err != nil {
		t.Fatalf("initialize Git fixture: %v: %s", err, output)
	}

	result := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "git",
		Args:        []string{"status", "--short"},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	if result.exitCode != 0 {
		t.Fatalf("git status failed without user config: err=%v stdout=%q stderr=%q", result.err, result.stdout, result.stderr)
	}
}

func TestMacOSCommandSandboxEnvironmentIsolatesGitConfig(t *testing.T) {
	stateRoot := t.TempDir()
	env := sandboxEnvironment([]string{
		"GIT_CONFIG_GLOBAL=/Users/example/.gitconfig",
		"GIT_CONFIG_NOSYSTEM=0",
		"TMP=/outside/tmp",
		"TEMP=/outside/temp",
	}, stateRoot)
	if got := sandboxEnvValue(env, "GIT_CONFIG_GLOBAL"); got != "/dev/null" {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q, want /dev/null", got)
	}
	if got := sandboxEnvValue(env, "GIT_CONFIG_NOSYSTEM"); got != "1" {
		t.Fatalf("GIT_CONFIG_NOSYSTEM = %q, want 1", got)
	}
	for _, key := range []string{"TMP", "TEMP"} {
		if got, want := sandboxEnvValue(env, key), filepath.Join(stateRoot, "tmp"); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}
}

func TestMacOSCommandSandboxAncestorPathsGrantMetadataOnlyToParents(t *testing.T) {
	got := sandboxAncestorPaths([]string{"/Users/example/work/project", "/opt/homebrew"})
	want := []string{"/Users", "/Users/example", "/Users/example/work", "/opt"}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("ancestor paths = %q, want %q", got, want)
	}
}

func TestMacOSCommandSandboxPolicyDoesNotExposeUserPreferences(t *testing.T) {
	for _, forbidden := range []string{"user-preference-read", "cfprefsd", "apple.cfprefs"} {
		if strings.Contains(macOSCommandSandboxBasePolicy, forbidden) {
			t.Fatalf("sandbox policy must not grant %q", forbidden)
		}
	}
}

func copySandboxTestExecutable(t *testing.T, source, project string) string {
	t.Helper()
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	target := filepath.Join(project, "sandbox-loopback-helper")
	output, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
	return target
}

type macOSSandboxTestResult struct {
	exitCode  int
	stdout    string
	stderr    string
	err       error
	sandboxed bool
}

func runMacOSSandboxTestCommand(t *testing.T, runner commandRunner, spec commandSpec) macOSSandboxTestResult {
	t.Helper()
	execution, err := runner.Prepare(spec)
	if err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	execution.Cmd.Stdout = &stdout
	execution.Cmd.Stderr = &stderr
	err = execution.Cmd.Run()
	exitCode := 0
	if execution.Cmd.ProcessState != nil {
		exitCode = execution.Cmd.ProcessState.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return macOSSandboxTestResult{
		exitCode:  exitCode,
		stdout:    stdout.String(),
		stderr:    stderr.String(),
		err:       err,
		sandboxed: execution.Sandboxed,
	}
}

func mustCommandEnvironment(t *testing.T) []string {
	t.Helper()
	env, err := commandEnvironment(nil)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

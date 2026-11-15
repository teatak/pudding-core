//go:build darwin

package tool

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestZZReviewSandboxUnixSocketOutsideProject(t *testing.T) {
	outside, err := os.MkdirTemp("/tmp", "zzrev")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)
	sock := filepath.Join(outside, "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			got <- "accept err " + err.Error()
			return
		}
		line, _ := bufio.NewReader(c).ReadString('\n')
		got <- line
		c.Close()
	}()
	project := t.TempDir()
	res := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "/bin/sh",
		Args:        []string{"-c", "echo ESCAPED | /usr/bin/nc -U " + sock},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	t.Logf("exit=%d stdout=%q stderr=%q sandboxed=%v", res.exitCode, res.stdout, res.stderr, res.sandboxed)
	select {
	case line := <-got:
		t.Logf("listener received: %q", line)
	case <-time.After(3 * time.Second):
		t.Logf("listener received nothing")
	}
}

func TestZZReviewSandboxHardlinkOutsideFile(t *testing.T) {
	outside, err := os.MkdirTemp("", "zzrevhl")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(outside)
	secret := filepath.Join(outside, "secret.txt")
	os.WriteFile(secret, []byte("TOPSECRET\n"), 0o600)
	project := t.TempDir()
	res := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "/bin/sh",
		Args:        []string{"-c", "ln " + secret + " ./link.txt; cat ./link.txt; cat " + secret},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	t.Logf("exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
}

func TestZZReviewSandboxBindAllInterfaces(t *testing.T) {
	project := t.TempDir()
	res := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "/usr/bin/python3",
		Args:        []string{"-c", "import socket;s=socket.socket();s.bind(('0.0.0.0',0));s.listen();print('BOUND',s.getsockname())"},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	t.Logf("exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
}

func TestZZReviewSandboxEnvLeak(t *testing.T) {
	project := t.TempDir()
	res := runMacOSSandboxTestCommand(t, newPlatformCommandRunner(t.TempDir()), commandSpec{
		Executable:  "/bin/sh",
		Args:        []string{"-c", "env | cut -d= -f1 | sort | tr '\\n' ' '; echo; ls -la $HOME 2>&1 | head -3; ls /Users 2>&1 | head -3; ls /private/var/folders 2>&1 | head -2"},
		CWD:         project,
		Env:         mustCommandEnvironment(t),
		ProjectDirs: []string{project},
	})
	t.Logf("exit=%d stdout=%q stderr=%q", res.exitCode, res.stdout, res.stderr)
}

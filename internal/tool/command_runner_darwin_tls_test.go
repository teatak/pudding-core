//go:build darwin

package tool

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Exercise the native macOS verifier without the network, a private keychain,
// or installing a test CA. It must reject a valid self-signed leaf as untrusted,
// rather than fail to communicate with the trust evaluation service.
func TestMacOSCommandSandboxSystemTrust(t *testing.T) {
	if certPath := os.Getenv("PUDDING_SANDBOX_TRUST_HELPER"); certPath != "" {
		der, err := os.ReadFile(certPath)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		_, err = cert.Verify(x509.VerifyOptions{DNSName: "sandbox.example.test"})
		var unknown x509.UnknownAuthorityError
		if !errors.As(err, &unknown) {
			t.Fatalf("expected untrusted certificate rejection, got %v", err)
		}
		return
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"sandbox.example.test"}, KeyUsage: x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true,
	}
	untrustedDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable = copySandboxTestExecutable(t, executable, project)
	runner := newPlatformCommandRunner(t.TempDir())
	certPath := filepath.Join(project, "untrusted.der")
	if err := os.WriteFile(certPath, untrustedDER, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mode CommandSandboxMode
	}{
		{"host", CommandSandboxBypass},
		{"sandbox", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := append(mustCommandEnvironment(t), "PUDDING_SANDBOX_TRUST_HELPER="+certPath)
			result := runMacOSSandboxTestCommand(t, runner, commandSpec{
				Executable: executable, Args: []string{"-test.run=^TestMacOSCommandSandboxSystemTrust$", "-test.timeout=15s"},
				CWD: project, Env: env, ProjectDirs: []string{project}, SandboxMode: tc.mode,
			})
			if result.exitCode != 0 || result.sandboxed != (tc.name == "sandbox") {
				t.Fatalf("%s verification: exit=%d stdout=%q stderr=%q", tc.name, result.exitCode, result.stdout, result.stderr)
			}
		})
	}
}

// Opt in explicitly: this test contacts the public Go module proxy and checksum
// database. All downloaded dependencies and build outputs stay in t.TempDir.
// GOSUMDB may explicitly select an accessible signed database endpoint for the
// test environment (e.g. Go's official sum.golang.google.cn alias), never off.
func TestMacOSCommandSandboxGoModuleTLS(t *testing.T) {
	if os.Getenv("PUDDING_SANDBOX_NETWORK_TEST") != "1" {
		t.Skip("set PUDDING_SANDBOX_NETWORK_TEST=1 to test real Go HTTPS downloads")
	}
	project, home := t.TempDir(), t.TempDir()
	writePatchTestFile(t, filepath.Join(project, "go.mod"), "module sandbox.example/tls\n\ngo 1.25\n\nrequire gopkg.in/yaml.v3 v3.0.1\n")
	writePatchTestFile(t, filepath.Join(project, "yaml_test.go"), `package sandbox
import (
    "testing"
    "gopkg.in/yaml.v3"
)
func TestDependency(t *testing.T) {
    var got map[string]int
    if err := yaml.Unmarshal([]byte("answer: 42"), &got); err != nil || got["answer"] != 42 {
        t.Fatalf("dependency failed: %v %v", got, err)
    }
}
`)
	sumdb := os.Getenv("GOSUMDB")
	if sumdb == "" {
		sumdb = "sum.golang.org"
	}
	if sumdb == "off" {
		t.Fatal("this test requires checksum database verification")
	}
	env, err := commandEnvironment(map[string]string{
		"GOPROXY": "https://proxy.golang.org", "GOSUMDB": sumdb, "GOTOOLCHAIN": "local",
	})
	if err != nil {
		t.Fatal(err)
	}
	requireTrustedSandboxCommand(t, "go", project, env)
	runner := newPlatformCommandRunner(home)
	runGo := func(args ...string) []byte {
		t.Helper()
		execution, err := runner.Prepare(commandSpec{
			Executable: "go", Args: args, CWD: project, Env: env,
			ProjectDirs: []string{project}, StateKey: "go-tls-regression",
		})
		if err != nil {
			t.Fatal(err)
		}
		if !execution.Sandboxed {
			t.Fatal("Go verification must run inside the project sandbox")
		}
		var output bytes.Buffer
		execution.Cmd.Stdout, execution.Cmd.Stderr = &output, &output
		if err := execution.Cmd.Start(); err != nil {
			t.Fatal(err)
		}
		timer := time.AfterFunc(60*time.Second, func() { _ = terminateCommandProcess(execution.Cmd) })
		err = execution.Cmd.Wait()
		timer.Stop()
		if err != nil {
			t.Fatalf("sandbox go %v: %v\n%s", args, err, output.String())
		}
		return output.Bytes()
	}
	var caches struct{ GOCACHE, GOMODCACHE string }
	if err := json.Unmarshal(runGo("env", "-json", "GOCACHE", "GOMODCACHE"), &caches); err != nil {
		t.Fatal(err)
	}
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	if !pathInsideRoot(caches.GOCACHE, resolvedHome) || !pathInsideRoot(caches.GOMODCACHE, resolvedHome) {
		t.Fatalf("Go caches escaped temporary sandbox state: %+v", caches)
	}
	// Go extracts modules into read-only directories. Let Go remove this exact
	// private cache before t.TempDir cleanup, including on an assertion failure.
	t.Cleanup(func() { runGo("clean", "-modcache") })
	moduleDir := filepath.Join(caches.GOMODCACHE, "gopkg.in", "yaml.v3@v3.0.1")
	if _, err := os.Stat(moduleDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected a cold module cache: %v", err)
	}
	var download struct{ Dir, Sum string }
	if err := json.Unmarshal(runGo("mod", "download", "-json", "gopkg.in/yaml.v3"), &download); err != nil {
		t.Fatal(err)
	}
	if download.Dir != moduleDir || !strings.HasPrefix(download.Sum, "h1:") {
		t.Fatalf("module was not downloaded and verified into the private cache: %+v", download)
	}
	if output := string(runGo("test", "-count=1", "./...")); !strings.Contains(output, "ok  \tsandbox.example/tls") {
		t.Fatalf("cold-cache test did not complete: %s", output)
	}
	t.Log("cold cache: HTTPS module download, checksum verification and go test passed")

	// A new runner/command must reuse the same private state. Disabling the proxy
	// here is a test assertion for warm-cache reuse, not a production default.
	runner = newPlatformCommandRunner(home)
	env = sandboxSetEnv(env, "GOPROXY", "off")
	var warmCaches struct{ GOCACHE, GOMODCACHE string }
	if err := json.Unmarshal(runGo("env", "-json", "GOCACHE", "GOMODCACHE"), &warmCaches); err != nil {
		t.Fatal(err)
	}
	if warmCaches != caches {
		t.Fatalf("cache paths changed between commands: cold=%+v warm=%+v", caches, warmCaches)
	}
	if output := string(runGo("test", "-count=1", "./...")); !strings.Contains(output, "ok  \tsandbox.example/tls") {
		t.Fatalf("warm-cache test did not complete: %s", output)
	}
	t.Log("warm cache: new runner reused module/build caches with GOPROXY=off")
}

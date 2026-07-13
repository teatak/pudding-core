//go:build !darwin

package tool

import "testing"

func TestUnsupportedCommandSandboxFailsClosed(t *testing.T) {
	runner := newPlatformCommandRunner(t.TempDir())
	spec := commandSpec{
		Executable: "go",
		Args:       []string{"version"},
		CWD:        t.TempDir(),
	}
	if _, err := runner.Prepare(spec); err == nil {
		t.Fatal("unsupported project sandbox must not silently run without isolation")
	}

	spec.SandboxMode = CommandSandboxBypass
	execution, err := runner.Prepare(spec)
	if err != nil {
		t.Fatalf("Full Access should keep direct command execution available: %v", err)
	}
	if execution.Sandboxed {
		t.Fatal("Full Access must not report a sandboxed execution")
	}
}

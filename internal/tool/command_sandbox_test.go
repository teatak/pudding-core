package tool

import (
	"errors"
	"testing"
)

func TestCommandSandboxDeniedRequiresSpecificEvidenceAndFailure(t *testing.T) {
	for _, output := range []string{
		"operation not permitted", "xcrun_db: operation not permitted\nld: malformed file",
		"permission denied", "",
	} {
		if commandSandboxDenied(output, errors.New("exit status 1")) || commandSandboxDenialOutput(output) {
			t.Fatalf("generic command failure is not a sandbox denial: %q", output)
		}
	}
	for _, output := range []string{"sandbox: deny(1) file-read-data", "sandbox-exec: sandbox_apply: Operation not permitted"} {
		if commandSandboxDenied(output, nil) {
			t.Fatalf("successful command must keep its outcome: %q", output)
		}
		if !commandSandboxDenied(output, errors.New("exit status 1")) || !commandSandboxDenialOutput(output) {
			t.Fatalf("specific denial evidence was not recognized: %q", output)
		}
	}
}

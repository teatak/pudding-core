package tool

import (
	"errors"
	"testing"
)

func TestCommandSandboxDeniedRequiresFailureForGenericEPERM(t *testing.T) {
	if commandSandboxDenied("operation not permitted", nil) {
		t.Fatal("successful output must not be labeled as a sandbox denial from generic EPERM text")
	}
	if !commandSandboxDenied("operation not permitted", errors.New("exit status 1")) {
		t.Fatal("failed command with EPERM text must be labeled as a sandbox denial")
	}
	if !commandSandboxDenied("sandbox: deny(1) file-read-data", nil) {
		t.Fatal("strong sandbox denial marker must be recognized without an exit error")
	}
}

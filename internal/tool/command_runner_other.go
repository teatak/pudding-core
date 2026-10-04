//go:build !darwin

package tool

import (
	"fmt"
	"runtime"
)

type unsupportedSandboxCommandRunner struct {
	direct commandRunner
}

func newPlatformCommandRunner(string) commandRunner {
	return unsupportedSandboxCommandRunner{direct: newDirectCommandRunner()}
}

func (r unsupportedSandboxCommandRunner) Prepare(spec commandSpec) (*commandExecution, error) {
	if spec.SandboxMode == CommandSandboxBypass {
		return r.direct.Prepare(spec)
	}
	return nil, fmt.Errorf("project command sandbox is not supported on %s; request execution=host with a concrete host_access_reason and obtain approval for this invocation", runtime.GOOS)
}

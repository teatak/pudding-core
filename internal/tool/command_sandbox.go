package tool

import (
	"errors"
	"strings"
)

var errSandboxDenialProbe = errors.New("sandbox denial probe")

func commandSandboxDenied(output string, runErr error) bool {
	if runErr != nil {
		output += "\n" + runErr.Error()
	}
	output = strings.ToLower(output)
	for _, marker := range []string{
		"sandbox_apply",
		"sandbox: deny",
		"deny(1)",
	} {
		if strings.Contains(output, marker) {
			return true
		}
	}
	return runErr != nil && strings.Contains(output, "operation not permitted")
}

func commandSandboxDenialOutput(output string) bool {
	return commandSandboxDenied(output, errSandboxDenialProbe)
}

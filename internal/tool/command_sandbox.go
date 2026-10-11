package tool

import "strings"

func commandSandboxDenied(output string, runErr error) bool {
	if runErr == nil {
		return false
	}
	return commandSandboxDenialOutput(output + "\n" + runErr.Error())
}

func commandSandboxDenialOutput(output string) bool {
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
	// Generic EPERM text also comes from toolchains and host services. It is
	// preserved in the output, but cannot establish a sandbox denial.
	return false
}

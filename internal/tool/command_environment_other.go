//go:build !darwin

package tool

import "context"

func captureLoginShellEnvironment(context.Context) ([]string, string, bool, error) {
	return nil, "", false, nil
}

func platformCommandEnvironment(env []string) ([]string, error) { return env, nil }

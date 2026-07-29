package tool

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
)

const (
	commandEnvironmentBeginMarker = "\x1ePUDDING_COMMAND_ENV_BEGIN\x1f"
	commandEnvironmentEndMarker   = "\x1ePUDDING_COMMAND_ENV_END\x1f"
)

type CommandEnvironmentSnapshot struct {
	Shell         string
	VariableCount int
	Supported     bool
}

var capturedCommandEnvironment struct {
	sync.RWMutex
	entries []string
}

// CaptureCommandEnvironment records a filtered login-shell environment for
// later subprocesses. Command execution still uses the fixed non-login shell.
func CaptureCommandEnvironment(ctx context.Context) (CommandEnvironmentSnapshot, error) {
	entries, shell, supported, err := captureLoginShellEnvironment(ctx)
	if err != nil {
		setCapturedCommandEnvironment(nil)
		return CommandEnvironmentSnapshot{Shell: shell, Supported: supported}, err
	}
	if !supported {
		setCapturedCommandEnvironment(nil)
		return CommandEnvironmentSnapshot{}, nil
	}
	entries = filterCommandEnvironmentEntries(entries)
	setCapturedCommandEnvironment(entries)
	return CommandEnvironmentSnapshot{
		Shell:         shell,
		VariableCount: len(entries),
		Supported:     true,
	}, nil
}

func loginShellEnvironmentScript() string {
	return "printf '" + commandEnvironmentBeginMarker + "\\0'; /usr/bin/env -0; printf '" + commandEnvironmentEndMarker + "\\0'"
}

func loginShellEnvironmentArgs() []string {
	return []string{"-i", "-l", "-c", loginShellEnvironmentScript()}
}

func parseLoginShellEnvironment(output []byte) ([]string, error) {
	begin := append([]byte(commandEnvironmentBeginMarker), 0)
	end := append([]byte(commandEnvironmentEndMarker), 0)
	beginAt := bytes.LastIndex(output, begin)
	if beginAt < 0 {
		return nil, errors.New("login shell environment start marker was not found")
	}
	payloadStart := beginAt + len(begin)
	endOffset := bytes.Index(output[payloadStart:], end)
	if endOffset < 0 {
		return nil, errors.New("login shell environment end marker was not found")
	}
	payload := output[payloadStart : payloadStart+endOffset]
	parts := bytes.Split(payload, []byte{0})
	entries := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) == 0 {
			continue
		}
		entry := string(part)
		key, _, ok := strings.Cut(entry, "=")
		if !ok || !validCommandEnvKey(key) {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func filterCommandEnvironmentEntries(entries []string) []string {
	type envValue struct {
		key   string
		value string
	}
	values := make(map[string]envValue)
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !allowedCommandEnvKey(key) || strings.ContainsRune(value, 0) {
			continue
		}
		values[strings.ToUpper(key)] = envValue{key: key, value: value}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		filtered = append(filtered, value.key+"="+value.value)
	}
	return filtered
}

func setCapturedCommandEnvironment(entries []string) {
	capturedCommandEnvironment.Lock()
	capturedCommandEnvironment.entries = append([]string(nil), entries...)
	capturedCommandEnvironment.Unlock()
}

func commandEnvironmentSnapshot() []string {
	capturedCommandEnvironment.RLock()
	defer capturedCommandEnvironment.RUnlock()
	return append([]string(nil), capturedCommandEnvironment.entries...)
}

func commandEnvironment(custom map[string]string) ([]string, error) {
	type envValue struct {
		key   string
		value string
	}
	values := make(map[string]envValue)
	merge := func(entries []string) {
		for _, entry := range entries {
			key, value, ok := strings.Cut(entry, "=")
			if !ok || !allowedCommandEnvKey(key) {
				continue
			}
			values[strings.ToUpper(key)] = envValue{key: key, value: value}
		}
	}
	merge(os.Environ())
	merge(commandEnvironmentSnapshot())
	for key, value := range custom {
		key = strings.TrimSpace(key)
		if !validCommandEnvKey(key) {
			return nil, errors.New("env keys must match [A-Za-z_][A-Za-z0-9_]*")
		}
		if strings.ContainsRune(value, 0) {
			return nil, errors.New("env values must not contain NUL bytes")
		}
		values[strings.ToUpper(key)] = envValue{key: key, value: value}
	}
	path := values["PATH"]
	path.key = "PATH"
	path.value = mergedExecutablePATH(path.value)
	values["PATH"] = path
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		out = append(out, value.key+"="+value.value)
	}
	return out, nil
}

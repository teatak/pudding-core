//go:build windows

package tool

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// persistedWindowsPATH returns the machine and user PATH from the registry, in
// the order Windows composes them for a new logon process.
func persistedWindowsPATH() string {
	var parts []string
	for _, source := range []struct {
		root registry.Key
		path string
	}{
		{registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`},
		{registry.CURRENT_USER, `Environment`},
	} {
		key, err := registry.OpenKey(source.root, source.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		value, valueType, err := key.GetStringValue("Path")
		key.Close()
		if err != nil || value == "" {
			continue
		}
		if valueType == registry.EXPAND_SZ {
			if expanded, err := registry.ExpandString(value); err == nil {
				value = expanded
			}
		}
		parts = append(parts, value)
	}
	return strings.Join(parts, ";")
}

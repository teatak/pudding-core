package gitrename

import (
	"sort"
	"strings"
)

func ChangedPaths(output string) []string {
	var paths []string
	for _, line := range strings.Split(output, "\n") {
		if len(line) >= 4 {
			paths = append(paths, strings.TrimSpace(line[3:]))
		}
	}
	sort.Strings(paths)
	return paths
}

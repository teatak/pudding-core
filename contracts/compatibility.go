package contracts

import (
	_ "embed"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

//go:embed plugin.json
var pluginJSON []byte

var pluginPolicy = func() struct {
	APIVersion string `json:"apiVersion"`
} { var p struct {
	APIVersion string `json:"apiVersion"`
}; if err := json.Unmarshal(pluginJSON, &p); err != nil {
	panic(err)
}; return p }()

func PluginAPIVersion() string { return pluginPolicy.APIVersion }

var stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

func ParseAPIVersion(value string) ([3]uint64, bool) {
	if value == "1" {
		value = "1.0.0"
	}
	var parts [3]uint64
	if !stableVersion.MatchString(value) {
		return parts, false
	}
	for i, part := range strings.Split(value, ".") {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil || n > 9007199254740991 {
			return parts, false
		}
		parts[i] = n
	}
	return parts, true
}
func SupportsVersion(required, provided string) bool {
	wanted, valid := ParseAPIVersion(required)
	host, supported := ParseAPIVersion(provided)
	return valid && supported && wanted[0] == host[0] && (wanted[1] < host[1] || (wanted[1] == host[1] && wanted[2] <= host[2]))
}
func SupportsPluginAPI(required string) bool {
	return SupportsPluginAPIVersion(required, PluginAPIVersion())
}
func SupportsPluginAPIVersion(required, provided string) bool {
	if required == "" {
		required = "1.0.0"
	}
	caret := strings.HasPrefix(required, "^")
	version := strings.TrimPrefix(required, "^")
	if !SupportsVersion(version, provided) {
		return false
	}
	if !caret {
		return true
	}
	wanted, _ := ParseAPIVersion(version)
	host, _ := ParseAPIVersion(provided)
	return wanted[0] != 0 || (wanted[1] == host[1] && (wanted[1] != 0 || wanted[2] == host[2]))
}

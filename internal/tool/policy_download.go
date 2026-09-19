package tool

import (
	"net"
	"net/url"
	"strconv"
	"strings"
)

// commandDownload describes a narrow, fully parsed GET/HEAD invocation. This is
// approval classification, not an OS-enforced restriction on network effects.
type commandDownload struct {
	outputs []string
	plain   bool
}

func parseCommandDownload(argv []string) commandDownload {
	if len(argv) < 2 {
		return commandDownload{}
	}
	operation := commandOperation(argv[0])
	if operation != "curl" && operation != "wget" {
		return commandDownload{}
	}
	result := commandDownload{plain: true}
	args := argv[1:]
	// curl only disables default configuration when this is its first option.
	noConfig := operation == "curl" && (args[0] == "-q" || args[0] == "--disable")
	var urls []string
	optionsEnded := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !optionsEnded && arg == "--" {
			optionsEnded = true
			continue
		}
		if optionsEnded || !strings.HasPrefix(arg, "-") {
			urls = append(urls, arg)
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name, value, hasValue := strings.Cut(arg, "=")
			kind := downloadLongOption(operation, name)
			if kind == "" || (kind == "flag" && hasValue) {
				return commandDownload{}
			}
			if kind == "flag" {
				if operation == "wget" && name == "--no-config" {
					noConfig = true
				}
				continue
			}
			if !hasValue {
				i++
				if i >= len(args) {
					return commandDownload{}
				}
				value = args[i]
			}
			if !result.addValue(kind, value, &urls) {
				return commandDownload{}
			}
			continue
		}
		// Short flags may be clustered, with a value attached to the last flag.
		for j := 1; j < len(arg); j++ {
			kind := downloadShortOption(operation, arg[j])
			if kind == "" {
				return commandDownload{}
			}
			if kind == "flag" {
				continue
			}
			value := arg[j+1:]
			if value == "" {
				i++
				if i >= len(args) {
					return commandDownload{}
				}
				value = args[i]
			}
			if !result.addValue(kind, value, &urls) {
				return commandDownload{}
			}
			break
		}
		if arg == "-" {
			return commandDownload{}
		}
	}
	if len(urls) != 1 || (operation == "wget" && len(result.outputs) == 0) {
		return commandDownload{}
	}
	rawURL := urls[0]
	implicitHTTP := !strings.Contains(rawURL, "://")
	if implicitHTTP {
		rawURL = "http://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return commandDownload{}
	}
	// Preserve existing simple loopback probes. External download exemptions
	// require explicit config suppression, so hidden auth/upload options cannot
	// enter through .curlrc/wgetrc. Unknown CLI options never inherit exemption.
	ip := net.ParseIP(u.Hostname())
	loopback := strings.EqualFold(u.Hostname(), "localhost") || (ip != nil && ip.IsLoopback())
	if (!noConfig || implicitHTTP) && !loopback {
		return commandDownload{}
	}
	return result
}

func downloadCompanionsAreNonExecuting(commands [][]string) bool {
	for _, argv := range commands {
		argv = unwrapCommand(argv)
		if len(argv) == 0 {
			return false
		}
		switch commandOperation(argv[0]) {
		case "curl", "wget", "head", "tail", "wc", "cat":
			// These still undergo their regular argument and boundary checks.
		default:
			return false
		}
	}
	return true
}

func (d *commandDownload) addValue(kind, value string, urls *[]string) bool {
	if value == "" {
		return false
	}
	switch kind {
	case "output":
		if value != "-" {
			d.outputs = append(d.outputs, value)
		}
	case "url":
		*urls = append(*urls, value)
	case "number":
		if _, err := strconv.ParseUint(value, 10, 32); err != nil {
			return false
		}
	}
	return true
}

func downloadLongOption(operation, name string) string {
	if operation == "curl" {
		switch name {
		case "--disable", "--fail", "--silent", "--show-error", "--location", "--head", "--compressed", "--globoff":
			return "flag"
		case "--output":
			return "output"
		case "--url":
			return "url"
		case "--connect-timeout", "--max-time", "--retry", "--retry-delay", "--retry-max-time", "--max-redirs":
			return "number"
		}
	} else {
		switch name {
		case "--no-config", "--quiet", "--no-verbose", "--no-hsts":
			return "flag"
		case "--output-document":
			return "output"
		case "--timeout", "--dns-timeout", "--connect-timeout", "--read-timeout", "--tries", "--max-redirect":
			return "number"
		}
	}
	return ""
}

func downloadShortOption(operation string, option byte) string {
	if operation == "curl" {
		switch option {
		case 'q', 'f', 's', 'S', 'L', 'I', 'g':
			return "flag"
		case 'o':
			return "output"
		case 'm':
			return "number"
		}
	} else {
		switch option {
		case 'q':
			return "flag"
		case 'O':
			return "output"
		case 'T', 't':
			return "number"
		}
	}
	return ""
}

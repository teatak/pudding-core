package tool

type commandPolicyAnalysis struct {
	shellCommandAnalysis
	wrappers [][]string
}

// analyzeCommandPolicy expands only literal, non-login POSIX shell wrappers
// for policy inspection. Execution still receives the original command string.
// Keep the wrapper executable in the analysis so its own path is also checked.
func analyzeCommandPolicy(command string, depth int) (commandPolicyAnalysis, error) {
	parsed, err := analyzeShellCommand(command)
	analysis := commandPolicyAnalysis{shellCommandAnalysis: parsed}
	if err != nil || depth >= 4 {
		return analysis, err
	}
	var commands [][]string
	for _, raw := range analysis.Commands {
		argv := unwrapCommand(raw)
		script, ok := policyShellScript(argv)
		if !ok {
			commands = append(commands, raw)
			continue
		}
		nested, err := analyzeCommandPolicy(script, depth+1)
		if err != nil {
			commands = append(commands, raw)
			continue
		}
		analysis.wrappers = append(analysis.wrappers, argv)
		analysis.wrappers = append(analysis.wrappers, nested.wrappers...)
		commands = append(commands, nested.Commands...)
		analysis.Redirections = append(analysis.Redirections, nested.Redirections...)
		analysis.Dynamic = analysis.Dynamic || nested.Dynamic
		analysis.Background = analysis.Background || nested.Background
	}
	analysis.Commands = commands
	if len(analysis.wrappers) > 0 {
		for _, child := range commands {
			child = unwrapCommand(child)
			if len(child) == 0 {
				continue
			}
			switch commandOperation(child[0]) {
			case "cd", "pushd", "popd", "eval", "exec", "source", ".", "export", "unset", "set", "alias", "unalias", "read", "trap", "getopts", "builtin", "enable":
				// These can change cwd, environment or subsequent command meaning.
				analysis.Dynamic = true
			case "sh", "bash", "dash", "ksh", "zsh", "fish":
				// Any shell left here has not had its body expanded and checked.
				analysis.Dynamic = true
			}
		}
	}
	return analysis, nil
}

func policyShellScript(argv []string) (string, bool) {
	if len(argv) == 3 && argv[1] == "-c" {
		switch argv[0] {
		case "sh", "/bin/sh", "dash", "/bin/dash":
			return argv[2], true
		}
	}
	if len(argv) == 5 && argv[1] == "--noprofile" && argv[2] == "--norc" && argv[3] == "-c" &&
		(argv[0] == "bash" || argv[0] == "/bin/bash") {
		return argv[4], true
	}
	return "", false
}

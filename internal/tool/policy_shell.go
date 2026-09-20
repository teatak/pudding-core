package tool

import (
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// NUL cannot occur in valid command input. Preserve unknown argument positions
// without inventing paths or executing expansions. Never pass this to execution.
const unknownPolicyWord = "\x00"

type commandPolicyAnalysis struct {
	Commands     [][]string
	Redirections []shellRedirection
	Environments []map[string]string
	wrappers     [][]string
}

// Auto trusts code inside the authorized sandbox. Inspect for explicit hazards
// and resource requests, not to prove arbitrary code safe. Unknown values and
// control flow do not independently need approval. Execution, verification and
// reusable grants retain their strict analyzers.
func analyzeCommandPolicy(command string, depth int) (commandPolicyAnalysis, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(command), "policy")
	analysis := commandPolicyAnalysis{}
	if err != nil {
		return analysis, err
	}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.CallExpr:
			argv := make([]string, len(node.Args))
			for index, word := range node.Args {
				value, ok := staticShellWord(word)
				if !ok || (index == 0 && !isStaticCommandWord(word)) {
					value = unknownPolicyWord
				}
				argv[index] = value
			}
			if len(argv) > 0 {
				analysis.Commands = append(analysis.Commands, argv)
			}
			for _, assign := range node.Assigns {
				if assign.Name != nil {
					value, ok := staticShellWord(assign.Value)
					if !ok {
						value = unknownPolicyWord
					}
					// Keep each occurrence: later assignments cannot erase an
					// earlier explicit permission request or risky environment.
					analysis.Environments = append(analysis.Environments, map[string]string{assign.Name.Value: value})
				}
			}
		case *syntax.Redirect:
			if redirect, ok := staticShellRedirection(node); ok && redirect.Path != "" {
				analysis.Redirections = append(analysis.Redirections, redirect)
			}
		}
		return true // Includes calls inside loops, functions and substitutions.
	})
	// Bound inspection work, not execution authority. Literal nested bodies are
	// useful evidence; opaque/different-dialect scripts still use the sandbox.
	if depth < 4 {
		var commands [][]string
		for _, raw := range analysis.Commands {
			script, ok := policyShellScript(unwrapCommand(raw))
			if !ok {
				commands = append(commands, raw)
				continue
			}
			nested, err := analyzeCommandPolicy(script, depth+1)
			if err != nil {
				commands = append(commands, raw)
				continue
			}
			// Keep wrappers for executable/env checks, but do not count the
			// same shell body twice when checking download-and-execute chains.
			analysis.wrappers = append(analysis.wrappers, raw)
			analysis.wrappers = append(analysis.wrappers, nested.wrappers...)
			commands = append(commands, nested.Commands...)
			analysis.Redirections = append(analysis.Redirections, nested.Redirections...)
			analysis.Environments = append(analysis.Environments, nested.Environments...)
		}
		analysis.Commands = commands
	}
	return analysis, nil
}

func policyShellScript(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	switch commandOperation(argv[0]) {
	case "builtin":
		return policyShellScript(argv[1:])
	case "eval":
		if len(argv) > 1 && !strings.Contains(strings.Join(argv[1:], " "), unknownPolicyWord) {
			return strings.Join(argv[1:], " "), true
		}
	case "sh", "bash", "dash", "ksh", "zsh":
		for index := 1; index < len(argv); index++ {
			arg := argv[index]
			if arg == "--" || !strings.HasPrefix(arg, "-") {
				break
			}
			if !strings.HasPrefix(arg, "--") && strings.Contains(arg, "c") && index+1 < len(argv) {
				return argv[index+1], argv[index+1] != unknownPolicyWord
			}
		}
	}
	return "", false
}

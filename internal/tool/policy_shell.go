package tool

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// NUL cannot occur in valid command input. Preserve unknown argument positions
// without inventing paths or executing expansions. Never pass this to execution.
const unknownPolicyWord = "\x00"

type shellPolicyCommand struct {
	Argv []string
	CWD  string
	call *syntax.CallExpr
}

type shellPolicyRedirection struct {
	Redirection shellRedirection
	CWD         string
	call        *syntax.CallExpr
	operator    string
}

type shellPolicyEnvironment struct {
	Env        map[string]string
	CWD        string
	pathPrefix string
}

type commandPolicyAnalysis struct {
	Commands     []shellPolicyCommand
	Redirections []shellPolicyRedirection
	Environments []shellPolicyEnvironment
	wrappers     []shellPolicyCommand
}

// Auto trusts code inside the authorized sandbox. Inspect for explicit hazards
// and resource requests, not to prove arbitrary code safe. Unknown values and
// control flow do not independently need approval. Execution, verification and
// reusable grants retain their strict analyzers.
func analyzeCommandPolicy(command string, depth int) (commandPolicyAnalysis, error) {
	return analyzeCommandPolicyAt(command, "", depth)
}

func analyzeCommandPolicyAt(command, initialCWD string, depth int) (commandPolicyAnalysis, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(command), "policy")
	analysis := commandPolicyAnalysis{}
	if err != nil {
		return analysis, err
	}
	walkPolicyNode(file, initialCWD, true, &analysis)
	// Bound inspection work, not execution authority. Literal nested bodies are
	// useful evidence; opaque/different-dialect scripts still use the sandbox.
	if depth < 4 {
		var commands []shellPolicyCommand
		for _, raw := range analysis.Commands {
			script, ok := policyShellScript(unwrapCommand(raw.Argv))
			if !ok {
				commands = append(commands, raw)
				continue
			}
			nested, err := analyzeCommandPolicyAt(script, raw.CWD, depth+1)
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
	for _, environment := range analysis.Environments {
		if environment.pathPrefix != "" {
			if !inheritsCommandPATH(file) {
				for i := range analysis.Environments {
					analysis.Environments[i].pathPrefix = ""
				}
			}
			break
		}
	}
	return analysis, nil
}

// Limit the inherited suffix exemption to simple command lists. A shell that
// sources another script, unsets PATH, or computes an assignment may have a
// different PATH (including an empty entry for the working directory).
func inheritsCommandPATH(file *syntax.File) bool {
	known := true
	syntax.Walk(file, func(node syntax.Node) bool {
		switch n := node.(type) {
		case *syntax.Stmt:
			switch n.Cmd.(type) {
			case *syntax.CallExpr, *syntax.BinaryCmd, *syntax.Block, *syntax.Subshell:
			default:
				known = false
			}
		case *syntax.CallExpr:
			if len(n.Args) == 0 {
				known = false
				break
			}
			name := literalGrantWords(n.Args[:1])
			if len(name) == 0 {
				known = false
				break
			}
			switch name[0] {
			case ".", "source", "alias", "builtin", "command", "eval", "exec", "export", "getopts", "read", "readonly", "set", "trap", "unset", "let", "mapfile", "readarray", "typeset", "declare", "local":
				known = false
			case "printf":
				known = known && (len(n.Args) == 1 || printfHasFormatOperand(n.Args[1]))
			}
		case *syntax.ParamExp:
			if plainParameterName(n) == "" {
				known = false
			}
		case *syntax.ArithmExp:
			known = false
		}
		return known
	})
	return known
}

func printfHasFormatOperand(word *syntax.Word) bool {
	// A quoted format beginning with ordinary text stays one operand, even
	// when it contains printf escapes such as "%s\n".
	if quoted, ok := word.Parts[0].(*syntax.DblQuoted); ok && !quoted.Dollar && len(quoted.Parts) > 0 {
		if lit, ok := quoted.Parts[0].(*syntax.Lit); ok && lit.Value != "" && lit.Value[0] != '-' && lit.Value[0] != '\\' {
			return true
		}
	}
	args := literalGrantWords([]*syntax.Word{word})
	return len(args) == 1 && (args[0] == "--" || !strings.HasPrefix(args[0], "-"))
}

// walkPolicyNode 仅沿着顺序执行、`&&`、代码块、子 shell 与分支传播静态工作目录；
// 子 shell 与条件分支内部变动不会泄漏到外层，其余复合结构保守保持当前目录。
func walkPolicyNode(node syntax.Node, cwd string, allowCWDUpdate bool, out *commandPolicyAnalysis) string {
	if node == nil {
		return cwd
	}
	switch node := node.(type) {
	case *syntax.File:
		current := cwd
		for _, stmt := range node.Stmts {
			current = walkPolicyNode(stmt, current, allowCWDUpdate, out)
		}
		return current
	case *syntax.Stmt:
		collectPolicyRedirections(node, cwd, out)
		for _, redir := range node.Redirs {
			if redir.Word != nil {
				walkNestedPolicyNodes(redir.Word, cwd, out)
			}
			if redir.Hdoc != nil {
				walkNestedPolicyNodes(redir.Hdoc, cwd, out)
			}
		}
		if node.Cmd == nil {
			return cwd
		}
		if node.Background || node.Coprocess || node.Disown {
			walkPolicyNode(node.Cmd, cwd, false, out)
			return cwd
		}
		next := walkPolicyNode(node.Cmd, cwd, allowCWDUpdate, out)
		if allowCWDUpdate {
			return next
		}
		return cwd
	case *syntax.BinaryCmd:
		if node.Op == syntax.AndStmt {
			mid := walkPolicyNode(node.X, cwd, allowCWDUpdate, out)
			return walkPolicyNode(node.Y, mid, allowCWDUpdate, out)
		}
		walkPolicyNode(node.X, cwd, false, out)
		walkPolicyNode(node.Y, cwd, false, out)
		return cwd
	case *syntax.Block:
		current := cwd
		for _, stmt := range node.Stmts {
			current = walkPolicyNode(stmt, current, allowCWDUpdate, out)
		}
		return current
	case *syntax.Subshell:
		current := cwd
		for _, stmt := range node.Stmts {
			current = walkPolicyNode(stmt, current, true, out)
		}
		return cwd
	case *syntax.IfClause:
		for _, stmt := range node.Cond {
			walkPolicyNode(stmt, cwd, false, out)
		}
		thenCWD := cwd
		for _, stmt := range node.Then {
			thenCWD = walkPolicyNode(stmt, thenCWD, true, out)
		}
		if node.Else != nil {
			walkPolicyNode(node.Else, cwd, true, out)
		}
		return cwd
	case *syntax.CaseClause:
		if node.Word != nil {
			walkNestedPolicyNodes(node.Word, cwd, out)
		}
		for _, item := range node.Items {
			for _, pat := range item.Patterns {
				walkNestedPolicyNodes(pat, cwd, out)
			}
			branchCWD := cwd
			for _, stmt := range item.Stmts {
				branchCWD = walkPolicyNode(stmt, branchCWD, true, out)
			}
		}
		return cwd
	case *syntax.CallExpr:
		argv := collectPolicyCall(node, cwd, out)
		for _, word := range node.Args {
			walkNestedPolicyNodes(word, cwd, out)
		}
		for _, assign := range node.Assigns {
			if assign != nil {
				walkNestedPolicyNodes(assign, cwd, out)
			}
		}
		if !allowCWDUpdate {
			return cwd
		}
		if next, changed := nextPolicyCWD(cwd, unwrapCommand(argv)); changed {
			return next
		}
		return cwd
	default:
		walkNestedPolicyNodes(node, cwd, out)
		return cwd
	}
}

func walkNestedPolicyNodes(node syntax.Node, cwd string, out *commandPolicyAnalysis) {
	if node == nil {
		return
	}
	syntax.Walk(node, func(sub syntax.Node) bool {
		switch sub := sub.(type) {
		case *syntax.CallExpr:
			collectPolicyCall(sub, cwd, out)
		case *syntax.Stmt:
			collectPolicyRedirections(sub, cwd, out)
		}
		return true
	})
}

func collectPolicyRedirections(stmt *syntax.Stmt, cwd string, out *commandPolicyAnalysis) {
	call, _ := stmt.Cmd.(*syntax.CallExpr)
	for _, redir := range stmt.Redirs {
		if redirect, ok := staticShellRedirection(redir); ok && redirect.Path != "" {
			out.Redirections = append(out.Redirections, shellPolicyRedirection{
				Redirection: redirect, CWD: cwd, call: call, operator: redir.Op.String(),
			})
		}
	}
}

func collectPolicyCall(call *syntax.CallExpr, cwd string, out *commandPolicyAnalysis) []string {
	argv := make([]string, len(call.Args))
	for index, word := range call.Args {
		value, ok := staticShellWord(word)
		if !ok || (index == 0 && !isStaticCommandWord(word)) {
			value = unknownPolicyWord
		}
		argv[index] = value
	}
	if len(argv) > 0 {
		out.Commands = append(out.Commands, shellPolicyCommand{Argv: argv, CWD: cwd, call: call})
	}
	for _, assign := range call.Assigns {
		if assign.Name != nil {
			value, ok := staticShellWord(assign.Value)
			var prefix string
			if !ok {
				value = unknownPolicyWord
				if assign.Name.Value == "PATH" {
					if dirs, valid := inheritedPATHPrefix(assign.Value); valid {
						prefix = dirs
					}
				}
			}
			// Keep each occurrence: later assignments cannot erase an
			// earlier explicit permission request or risky environment.
			out.Environments = append(out.Environments, shellPolicyEnvironment{
				Env:        map[string]string{assign.Name.Value: value},
				CWD:        cwd,
				pathPrefix: prefix,
			})
		}
	}
	return argv
}

// Inspect only the added directories. The shell retains the inherited PATH
// at execution; a literal dollar sign or any other expansion is not equivalent.
func inheritedPATHPrefix(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	parts := word.Parts
	if len(parts) == 1 {
		if quoted, ok := parts[0].(*syntax.DblQuoted); ok && !quoted.Dollar {
			parts = quoted.Parts
		}
	}
	if len(parts) != 2 {
		return "", false
	}
	prefix, ok := staticShellWord(&syntax.Word{Parts: parts[:1]})
	tail := parts[1]
	if quoted, ok := tail.(*syntax.DblQuoted); ok && !quoted.Dollar && len(quoted.Parts) == 1 {
		tail = quoted.Parts[0]
	}
	parameter, expanded := tail.(*syntax.ParamExp)
	if !ok || !expanded || plainParameterName(parameter) != "PATH" || strings.ContainsAny(prefix, "$\\") {
		return "", false
	}
	dirs, separated := strings.CutSuffix(prefix, ":")
	if !separated || dirs == "" {
		return "", false
	}
	return dirs, true
}

func nextPolicyCWD(cwd string, unwrapped []string) (string, bool) {
	if len(unwrapped) == 0 {
		return cwd, false
	}
	op := commandOperation(unwrapped[0])
	if op == "popd" {
		// popd 目标依赖运行时目录栈，无法静态推导，重置为初始目录以防沿用旧子目录抵消越权路径。
		return "", true
	}
	if op != "cd" && op != "pushd" {
		return cwd, false
	}
	target, ok := policyCDTarget(unwrapped)
	if !ok {
		// 无法静态确定目录（如 cd -、cd、cd "$VAR"）时重置为初始目录，避免借用先前子目录放行 ../ 路径。
		return "", true
	}
	return joinPolicyCWD(cwd, target), true
}

func policyCDTarget(argv []string) (string, bool) {
	var targets []string
	optionsEnded := false
	for _, arg := range argv[1:] {
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.Contains(arg, unknownPolicyWord) {
			return "", false
		}
		if !optionsEnded && arg == "--" {
			optionsEnded = true
			continue
		}
		if !optionsEnded && (arg == "-L" || arg == "-P") {
			continue
		}
		if !optionsEnded && strings.HasPrefix(arg, "-") {
			return "", false
		}
		targets = append(targets, arg)
	}
	if len(targets) != 1 || sandboxManagedPath(targets[0]) || strings.Contains(targets[0], "$") {
		return "", false
	}
	return targets[0], true
}

func joinPolicyCWD(cwd, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	if cwd == "" {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(cwd, target))
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

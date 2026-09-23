package tool

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// An empty directory is unknown, never the initial command cwd. Keeping the
// two outcomes separate lets && use a successful cd without assuming cd cannot
// fail, or guessing which directory survives a semicolon or || join.
type mutationShellFlow struct {
	success string
	failure string
}

func mergeMutationCWD(a, b string) string {
	if a == b {
		return a
	}
	return ""
}

type mutationShellCollector struct {
	add       func(cwd, path string)
	functions map[string]bool
	opaqueCD  bool
}

func collectShellMutationTargets(file *syntax.File, cwd string, env map[string]string, add func(cwd, path string)) {
	c := mutationShellCollector{add: add, functions: make(map[string]bool)}
	for key := range env {
		if key = strings.TrimSpace(key); key == "CDPATH" || key == "PWD" {
			c.opaqueCD = true
		}
	}
	// Do not interpret functions, aliases, eval or shell environment changes.
	// They can replace cd or its path lookup even when a later cd looks literal.
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.FuncDecl:
			if node.Name != nil {
				c.functions[node.Name.Value] = true
			}
		case *syntax.CallExpr:
			if len(node.Args) > 0 {
				name, static := staticShellWord(node.Args[0])
				if !static || !isStaticCommandWord(node.Args[0]) || opaqueMutationShellCommand(name) || name == "command" || name == "builtin" {
					c.opaqueCD = true
				}
			}
			for _, assign := range node.Assigns {
				if assign.Name != nil && (assign.Name.Value == "CDPATH" || assign.Name.Value == "PWD") {
					c.opaqueCD = true
				}
			}
		}
		return true
	})
	c.statements(file.Stmts, cwd)
}

func (c *mutationShellCollector) statements(stmts []*syntax.Stmt, cwd string) mutationShellFlow {
	flow := mutationShellFlow{cwd, cwd}
	for _, stmt := range stmts {
		flow = c.statement(stmt, mergeMutationCWD(flow.success, flow.failure))
	}
	return flow
}

func (c *mutationShellCollector) statement(stmt *syntax.Stmt, cwd string) mutationShellFlow {
	flow := mutationShellFlow{cwd, cwd}
	// Compound-command redirections are opened before entering their body.
	for _, redirect := range stmt.Redirs {
		c.redirect(redirect, cwd)
	}
	switch cmd := stmt.Cmd.(type) {
	case nil:
		// A redirect-only statement does not change the shell directory.
	case *syntax.CallExpr:
		flow = c.call(cmd, cwd)
	case *syntax.BinaryCmd:
		switch cmd.Op {
		case syntax.AndStmt:
			left := c.statement(cmd.X, cwd)
			right := c.statement(cmd.Y, left.success)
			flow = mutationShellFlow{right.success, mergeMutationCWD(left.failure, right.failure)}
		case syntax.OrStmt:
			left := c.statement(cmd.X, cwd)
			right := c.statement(cmd.Y, left.failure)
			flow = mutationShellFlow{mergeMutationCWD(left.success, right.success), right.failure}
		default: // Fixed /bin/sh pipelines do not propagate a child's cd.
			c.statement(cmd.X, cwd)
			c.statement(cmd.Y, cwd)
		}
	case *syntax.Subshell:
		c.statements(cmd.Stmts, cwd)
	case *syntax.Block:
		flow = c.statements(cmd.Stmts, cwd)
	case *syntax.FuncDecl:
		// Defining a function does not execute its body or change the cwd.
	default:
		// Unsupported control flow can change cwd. Retain explicit absolute
		// targets, but never attribute its relative writes to the initial cwd.
		c.absoluteTargets(cmd)
		flow = mutationShellFlow{}
	}
	if len(stmt.Redirs) > 0 {
		flow.failure = mergeMutationCWD(flow.failure, cwd)
	}
	if stmt.Negated {
		flow.success, flow.failure = flow.failure, flow.success
	}
	return flow
}

func (c *mutationShellCollector) call(call *syntax.CallExpr, cwd string) mutationShellFlow {
	flow := mutationShellFlow{cwd, cwd}
	// Expansions can execute nested commands. Their cwd is deliberately not
	// inferred, but an explicit absolute write remains an observable target.
	for _, word := range call.Args {
		c.absoluteTargets(word)
	}
	for _, assign := range call.Assigns {
		if assign.Value != nil {
			c.absoluteTargets(assign.Value)
		}
	}
	if len(call.Args) == 0 {
		return flow
	}
	argv, static := staticCallArgv(call)
	if len(argv) == 0 || !isStaticCommandWord(call.Args[0]) {
		return mutationShellFlow{}
	}
	if c.functions[argv[0]] || opaqueMutationShellCommand(argv[0]) {
		return mutationShellFlow{}
	}
	if argv[0] == "cd" {
		flow.success = ""
		if !c.opaqueCD && static && len(argv) == 2 && argv[1] != "" &&
			!strings.HasPrefix(argv[1], "-") && !strings.ContainsAny(argv[1], "$*?[~\\") {
			if filepath.IsAbs(argv[1]) {
				flow.success = filepath.Clean(argv[1])
			} else if cwd != "" {
				// Preserve logical shell paths until the final target resolver;
				// resolving a symlink now would change the meaning of cd .. .
				flow.success = filepath.Join(cwd, argv[1])
			}
		}
		return flow
	}
	if argv[0] == "command" || argv[0] == "builtin" {
		// A wrapper may invoke cd or a shell function in the current shell.
		return mutationShellFlow{}
	}
	if static {
		for _, path := range commandMutationPaths(argv) {
			c.add(cwd, path)
		}
	}
	return flow
}

func opaqueMutationShellCommand(name string) bool {
	switch name {
	case "eval", ".", "source", "alias", "unalias", "export", "readonly", "unset", "set":
		return true
	}
	return false
}

func (c *mutationShellCollector) redirect(node *syntax.Redirect, cwd string) {
	if node.Word != nil {
		c.absoluteTargets(node.Word)
	}
	if node.Hdoc != nil {
		c.absoluteTargets(node.Hdoc)
	}
	redirect, static := staticShellRedirection(node)
	if static && redirect.Writes && !isSafeDeviceRedirection(redirect.Path) {
		c.add(cwd, redirect.Path)
	}
}

func (c *mutationShellCollector) absoluteTargets(node syntax.Node) {
	if node == nil {
		return
	}
	syntax.Walk(node, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.FuncDecl:
			return false
		case *syntax.Redirect:
			c.redirect(node, "")
		case *syntax.CallExpr:
			argv, static := staticCallArgv(node)
			if static && len(argv) > 0 && !c.functions[argv[0]] {
				for _, path := range commandMutationPaths(argv) {
					c.add("", path)
				}
			}
		}
		return true
	})
}

package tool

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

type shellCommandAnalysis struct {
	Commands     [][]string
	Redirections []shellRedirection
	Dynamic      bool
	Background   bool
}

type shellRedirection struct {
	Path   string
	Writes bool
}

func analyzeShellCommand(command string) (shellCommandAnalysis, error) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(command), "command")
	if err != nil {
		return shellCommandAnalysis{}, fmt.Errorf("invalid shell command: %w", err)
	}
	if len(file.Stmts) == 0 {
		return shellCommandAnalysis{}, errors.New("command is required")
	}

	analysis := shellCommandAnalysis{}
	syntax.Walk(file, func(node syntax.Node) bool {
		switch node := node.(type) {
		case *syntax.Stmt:
			if node.Background || node.Coprocess || node.Disown {
				analysis.Dynamic = true
				analysis.Background = true
			}
		case *syntax.CallExpr:
			argv, static := staticCallArgv(node)
			if len(argv) > 0 {
				analysis.Commands = append(analysis.Commands, argv)
			}
			if !static || len(node.Assigns) > 0 {
				analysis.Dynamic = true
			}
		case *syntax.Redirect:
			redirect, static := staticShellRedirection(node)
			if !static {
				analysis.Dynamic = true
			} else if redirect.Path != "" {
				analysis.Redirections = append(analysis.Redirections, redirect)
			}
		case *syntax.ParamExp, *syntax.CmdSubst, *syntax.ArithmExp, *syntax.ProcSubst,
			*syntax.ExtGlob, *syntax.BraceExp, *syntax.IfClause, *syntax.WhileClause,
			*syntax.ForClause, *syntax.CaseClause, *syntax.Block, *syntax.Subshell,
			*syntax.FuncDecl, *syntax.ArithmCmd, *syntax.TestClause, *syntax.DeclClause,
			*syntax.LetClause, *syntax.TimeClause, *syntax.CoprocClause, *syntax.TestDecl:
			analysis.Dynamic = true
		}
		return true
	})
	if len(analysis.Commands) == 0 {
		analysis.Dynamic = true
	}
	return analysis, nil
}

func staticCallArgv(call *syntax.CallExpr) ([]string, bool) {
	argv := make([]string, 0, len(call.Args))
	for _, word := range call.Args {
		value, ok := staticShellWord(word)
		if !ok {
			return argv, false
		}
		argv = append(argv, value)
	}
	return argv, len(argv) > 0
}

func staticShellWord(word *syntax.Word) (string, bool) {
	if word == nil {
		return "", false
	}
	var value strings.Builder
	for _, part := range word.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			value.WriteString(part.Value)
		case *syntax.SglQuoted:
			if part.Dollar {
				return "", false
			}
			value.WriteString(part.Value)
		case *syntax.DblQuoted:
			if part.Dollar {
				return "", false
			}
			for _, quotedPart := range part.Parts {
				literal, ok := quotedPart.(*syntax.Lit)
				if !ok {
					return "", false
				}
				value.WriteString(literal.Value)
			}
		default:
			return "", false
		}
	}
	return value.String(), true
}

func staticShellRedirection(redirect *syntax.Redirect) (shellRedirection, bool) {
	if redirect == nil {
		return shellRedirection{}, false
	}
	switch redirect.Op {
	case syntax.Hdoc, syntax.DashHdoc:
		return shellRedirection{}, true
	case syntax.DplIn, syntax.DplOut:
		target, ok := staticShellWord(redirect.Word)
		if !ok || (target != "-" && !isShellFileDescriptor(target)) {
			return shellRedirection{}, false
		}
		return shellRedirection{}, true
	case syntax.RdrIn:
		path, ok := staticShellWord(redirect.Word)
		return shellRedirection{Path: path}, ok
	case syntax.RdrOut, syntax.AppOut, syntax.RdrInOut, syntax.RdrClob,
		syntax.AppClob, syntax.RdrAll, syntax.RdrAllClob, syntax.AppAll,
		syntax.AppAllClob:
		path, ok := staticShellWord(redirect.Word)
		return shellRedirection{Path: path, Writes: true}, ok
	default:
		return shellRedirection{}, false
	}
}

func isShellFileDescriptor(value string) bool {
	if value == "" {
		return false
	}
	_, err := strconv.ParseUint(value, 10, 31)
	return err == nil
}

func commandVerificationArgv(command string) []string {
	analysis, err := analyzeShellCommand(command)
	if err != nil || analysis.Dynamic || len(analysis.Commands) != 1 {
		return nil
	}
	return analysis.Commands[0]
}

func joinShellCommand(argv []string) string {
	quoted := make([]string, 0, len(argv))
	for _, arg := range argv {
		quoted = append(quoted, quoteShellArg(arg))
	}
	return strings.Join(quoted, " ")
}

func quoteShellArg(arg string) string {
	if arg != "" && strings.IndexFunc(arg, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("_@%+=:,./-", r))
	}) == -1 {
		return arg
	}
	return "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
}

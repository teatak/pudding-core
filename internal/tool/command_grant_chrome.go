package tool

import (
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// Recognize only the local screenshot template, not arbitrary shell programs.
// Keep its harmless stderr redirection and completion marker in the invocation
// and grant key. TMPDIR is expanded once from the exact execution environment.
func chromeGrantCommand(command string, env []string) ([]string, string, bool) {
	file, err := syntax.NewParser(syntax.Variant(syntax.LangPOSIX)).Parse(strings.NewReader(command), "command")
	if err != nil || len(file.Stmts) < 1 || len(file.Stmts) > 2 {
		return nil, "", false
	}
	first := file.Stmts[0]
	call, ok := chromeGrantSimpleCall(first)
	if !ok {
		return nil, "", false
	}
	suffix := ""
	if len(first.Redirs) > 0 {
		if len(first.Redirs) != 1 {
			return nil, "", false
		}
		r := first.Redirs[0]
		path := literalGrantWords([]*syntax.Word{r.Word})
		if r.N == nil || r.N.Value != "2" || r.Op != syntax.RdrOut || len(path) != 1 || path[0] != "/dev/null" {
			return nil, "", false
		}
		suffix = " 2>/dev/null"
	}
	if len(file.Stmts) == 2 {
		last := file.Stmts[1]
		echo, ok := chromeGrantSimpleCall(last)
		if !ok || len(last.Redirs) > 0 {
			return nil, "", false
		}
		words := literalGrantWords(echo.Args)
		if len(words) != 2 || words[0] != "echo" || words[1] != "done" {
			return nil, "", false
		}
		suffix += "; echo done"
	}
	argv := make([]string, 0, len(call.Args))
	tempProfile := false
	for _, word := range call.Args {
		if literal := literalGrantWords([]*syntax.Word{word}); len(literal) == 1 {
			argv = append(argv, literal[0])
			continue
		}
		profile, ok := chromeGrantTempProfile(word, env)
		if !ok || tempProfile {
			return nil, "", false
		}
		tempProfile = true
		argv = append(argv, "--user-data-dir="+profile)
	}
	return argv, suffix, tempProfile
}

func chromeGrantSimpleCall(stmt *syntax.Stmt) (*syntax.CallExpr, bool) {
	if stmt.Negated || stmt.Background || stmt.Coprocess || stmt.Disown {
		return nil, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	return call, ok && len(call.Assigns) == 0 && len(call.Args) > 0
}

// Accept --user-data-dir="$TMPDIR/subdirectory" only. Unquoted expansion,
// substitutions, other variables and modifiers are not part of this lease.
func chromeGrantTempProfile(word *syntax.Word, env []string) (string, bool) {
	if len(word.Parts) != 2 {
		return "", false
	}
	prefix, ok := word.Parts[0].(*syntax.Lit)
	if !ok || prefix.Value != "--user-data-dir=" {
		return "", false
	}
	quoted, ok := word.Parts[1].(*syntax.DblQuoted)
	if !ok || quoted.Dollar || len(quoted.Parts) < 2 {
		return "", false
	}
	parameter, ok := quoted.Parts[0].(*syntax.ParamExp)
	if !ok {
		return "", false
	}
	value, ok := staticSandboxParameter(parameter)
	if !ok || value != "$TMPDIR" {
		return "", false
	}
	var suffix strings.Builder
	for _, part := range quoted.Parts[1:] {
		lit, ok := part.(*syntax.Lit)
		if !ok || strings.ContainsAny(lit.Value, "\\$`") {
			return "", false
		}
		suffix.WriteString(lit.Value)
	}
	if !strings.HasPrefix(suffix.String(), "/") {
		return "", false
	}
	root := executableEnvValue(env, "TMPDIR")
	if !filepath.IsAbs(root) {
		return "", false
	}
	return filepath.Join(root, strings.TrimPrefix(suffix.String(), "/")), true
}

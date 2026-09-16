package tool

import (
	"reflect"
	"testing"
)

func TestAnalyzeShellCommandCollectsPipelineAndRedirect(t *testing.T) {
	analysis, err := analyzeShellCommand(`rg 'TODO' internal | head -20 > report.txt`)
	if err != nil {
		t.Fatal(err)
	}
	wantCommands := [][]string{{"rg", "TODO", "internal"}, {"head", "-20"}}
	if !reflect.DeepEqual(analysis.Commands, wantCommands) || analysis.Dynamic || analysis.Background {
		t.Fatalf("unexpected analysis: %+v", analysis)
	}
	if len(analysis.Redirections) != 1 || analysis.Redirections[0].Path != "report.txt" || !analysis.Redirections[0].Writes {
		t.Fatalf("unexpected redirects: %+v", analysis.Redirections)
	}
}

func TestAnalyzeShellCommandMarksDynamicStructures(t *testing.T) {
	for _, command := range []string{
		`printf '%s' "$TOKEN"`,
		`printf '%s' "$(date)"`,
		`for file in *.go; do printf '%s\n' "$file"; done`,
	} {
		analysis, err := analyzeShellCommand(command)
		if err != nil {
			t.Fatalf("analyze %q: %v", command, err)
		}
		if !analysis.Dynamic {
			t.Fatalf("command must be dynamic: %q %+v", command, analysis)
		}
	}
}

func TestAnalyzeShellCommandAcceptsSandboxManagedPaths(t *testing.T) {
	analysis, err := analyzeShellCommand(`cat > "$TMPDIR/report.py" && python3 "${TMPDIR}/report.py"`)
	if err != nil {
		t.Fatal(err)
	}
	wantCommands := [][]string{{"cat"}, {"python3", "$TMPDIR/report.py"}}
	if !reflect.DeepEqual(analysis.Commands, wantCommands) || analysis.Dynamic {
		t.Fatalf("unexpected managed path analysis: %+v", analysis)
	}
	if len(analysis.Redirections) != 1 || analysis.Redirections[0].Path != "$TMPDIR/report.py" {
		t.Fatalf("unexpected managed path redirect: %+v", analysis.Redirections)
	}
}

func TestAnalyzeShellCommandParameterQuoting(t *testing.T) {
	tests := []struct {
		name    string
		command string
		dynamic bool
	}{
		{"unquoted working directory", `printf '%s' $PWD`, true},
		{"unquoted home suffix", `printf '%s' ${HOME}/file`, true},
		{"unquoted managed temporary path", `python3 $TMPDIR/script.py`, true},
		{"quoted managed paths", `printf '%s' "$PWD" "${HOME}" "$TMPDIR/file"`, false},
		{"unquoted caller-controlled old directory", `git config $OLDPWD`, true},
		{"quoted caller-controlled old directory", `git config "$OLDPWD"`, true},
		{"literal parameter text", `printf '%s' '$OLDPWD'`, false},
		{"redirection does not split fields", `printf ok > $TMPDIR/report.txt`, false},
		{"assignment does not split fields", `REPORT_DIR=$PWD printf ok`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			analysis, err := analyzeShellCommand(test.command)
			if err != nil {
				t.Fatal(err)
			}
			if analysis.Dynamic != test.dynamic {
				t.Fatalf("dynamic=%v, want %v: %+v", analysis.Dynamic, test.dynamic, analysis)
			}
		})
	}
}

func TestAnalyzeShellCommandRejectsMalformedAndBackgroundCommands(t *testing.T) {
	if _, err := analyzeShellCommand(`printf "unterminated`); err == nil {
		t.Fatal("malformed shell command must fail")
	}
	if err := validateCommandInput(commandRunArgs{Command: `sleep 10 &`}); err == nil {
		t.Fatal("foreground command must reject shell background operators")
	}
}

func TestCommandVerificationArgvRequiresOneStaticCommand(t *testing.T) {
	if got := commandVerificationArgv("go test ./..."); !reflect.DeepEqual(got, []string{"go", "test", "./..."}) {
		t.Fatalf("unexpected verification argv: %v", got)
	}
	for _, command := range []string{"go test ./... | tee test.log", `go test "$TARGET"`} {
		if got := commandVerificationArgv(command); got != nil {
			t.Fatalf("dynamic or compound command must not become verification argv: %q %v", command, got)
		}
	}
}

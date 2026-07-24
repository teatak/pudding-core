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

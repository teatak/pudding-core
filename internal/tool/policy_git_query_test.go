package tool

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestClassifyGitRefQueryOptions(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		lowRisk bool
	}{
		{name: "default list", lowRisk: true},
		{name: "multiple patterns", args: []string{"--list", "main", "topic"}, lowRisk: true},
		{name: "short list", args: []string{"-l", "main", "topic"}, lowRisk: true},
		{name: "contains separate", args: []string{"--contains", "HEAD", "main", "topic"}, lowRisk: true},
		{name: "contains attached", args: []string{"--contains=HEAD", "main", "topic"}, lowRisk: true},
		{name: "no contains separate", args: []string{"--no-contains", "HEAD", "main"}, lowRisk: true},
		{name: "no contains attached", args: []string{"--no-contains=HEAD", "main"}, lowRisk: true},
		{name: "merged separate", args: []string{"--merged", "HEAD", "main"}, lowRisk: true},
		{name: "merged attached", args: []string{"--merged=HEAD", "main"}, lowRisk: true},
		{name: "no merged separate", args: []string{"--no-merged", "HEAD", "main"}, lowRisk: true},
		{name: "no merged attached", args: []string{"--no-merged=HEAD", "main"}, lowRisk: true},
		{name: "points at separate", args: []string{"--points-at", "HEAD", "main", "topic"}, lowRisk: true},
		{name: "points at attached", args: []string{"--points-at=HEAD", "main", "topic"}, lowRisk: true},
		{name: "filter default at end", args: []string{"--contains"}, lowRisk: true},
		{name: "sort default list", args: []string{"--sort", "refname"}, lowRisk: true},
		{name: "negative sort", args: []string{"--sort", "-refname"}, lowRisk: true},
		{name: "format default list", args: []string{"--format", "%(refname)"}, lowRisk: true},
		{name: "empty format value", args: []string{"--format", ""}, lowRisk: true},
		{name: "formatted patterns", args: []string{"--list", "--sort=refname", "--format=%(refname)", "main", "topic"}, lowRisk: true},
		{name: "optional flags with list", args: []string{"--list", "--column", "--color", "main", "topic"}, lowRisk: true},
		{name: "attached optional values with list", args: []string{"--list", "--column=always", "--color=never", "main"}, lowRisk: true},
		{name: "new ref", args: []string{"newref"}},
		{name: "sort does not list", args: []string{"--sort", "refname", "newref"}},
		{name: "attached sort does not list", args: []string{"--sort=refname", "newref"}},
		{name: "format does not list", args: []string{"--format", "%(refname)", "newref"}},
		{name: "attached format does not list", args: []string{"--format=%(refname)", "newref"}},
		{name: "empty format does not list", args: []string{"--format", "", "newref"}},
		{name: "column does not consume ref", args: []string{"--column", "newref"}},
		{name: "attached column does not list", args: []string{"--column=always", "newref"}},
		{name: "color does not consume ref", args: []string{"--color", "newref"}},
		{name: "attached color does not list", args: []string{"--color=never", "newref"}},
		{name: "sort consumes list as value", args: []string{"--sort", "--list", "newref"}},
		{name: "format consumes list as value", args: []string{"--format", "--list", "newref"}},
		{name: "missing sort value", args: []string{"--sort"}},
		{name: "missing format value", args: []string{"--format"}},
		{name: "empty filter value", args: []string{"--contains=", "newref"}},
		{name: "filter consumes option as value", args: []string{"--contains", "--list", "newref"}},
		{name: "cancelled list", args: []string{"--list", "--no-list", "newref"}},
		{name: "cancelled points at", args: []string{"--points-at=HEAD", "--no-points-at", "newref"}},
		{name: "unknown filter suffix", args: []string{"--contains-extra=HEAD"}},
		{name: "unknown sort suffix", args: []string{"--sort-extra=refname"}},
		{name: "abbreviated option", args: []string{"--cont=HEAD"}},
		{name: "case sensitive option", args: []string{"--LIST"}},
		{name: "leading option whitespace", args: []string{" --list", "newref"}},
		{name: "trailing option whitespace", args: []string{"--list ", "newref"}},
		{name: "value on flag", args: []string{"--list=main"}},
		{name: "delete", args: []string{"--list", "-d", "main"}},
	}
	for _, subcommand := range []string{"branch", "tag"} {
		t.Run(subcommand, func(t *testing.T) {
			for _, test := range tests {
				t.Run(test.name, func(t *testing.T) {
					assertGitRefQueryRisk(t, append([]string{subcommand}, test.args...), test.lowRisk)
				})
			}
		})
	}
	for _, test := range []struct {
		name    string
		args    []string
		lowRisk bool
	}{
		{name: "branch combined list", args: []string{"branch", "-avl", "main", "topic"}, lowRisk: true},
		{name: "branch current", args: []string{"branch", "--show-current"}, lowRisk: true},
		{name: "branch current with ref", args: []string{"branch", "--show-current", "newref"}},
		{name: "branch cancelled current", args: []string{"branch", "--show-current", "--no-show-current", "newref"}},
		{name: "branch abbrev does not consume ref", args: []string{"branch", "--abbrev", "newref"}},
		{name: "branch attached abbrev does not list", args: []string{"branch", "--abbrev=8", "newref"}},
		{name: "branch abbrev with list", args: []string{"branch", "--list", "--abbrev", "main"}, lowRisk: true},
		{name: "branch missing points at value", args: []string{"branch", "--points-at"}},
		{name: "tag default points at", args: []string{"tag", "--points-at"}, lowRisk: true},
		{name: "tag annotation query", args: []string{"tag", "-n", "v1"}, lowRisk: true},
		{name: "tag combined annotation query", args: []string{"tag", "-ln", "v1"}, lowRisk: true},
		{name: "tag invalid optional count", args: []string{"tag", "-nl", "v1"}},
		{name: "tag unknown branch option", args: []string{"tag", "--abbrev"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertGitRefQueryRisk(t, test.args, test.lowRisk)
		})
	}
}

func TestGitRefQueryPolicyMatchesRefChanges(t *testing.T) {
	for _, subcommand := range []string{"branch", "tag"} {
		t.Run(subcommand, func(t *testing.T) {
			for _, test := range []struct {
				name    string
				args    []string
				lowRisk bool
			}{
				{name: "sort creates ref", args: []string{"--sort", "refname", "newref"}},
				{name: "attached sort creates ref", args: []string{"--sort=refname", "newref"}},
				{name: "format creates ref", args: []string{"--format", "%(refname)", "newref"}},
				{name: "attached format creates ref", args: []string{"--format=%(refname)", "newref"}},
				{name: "column creates ref", args: []string{"--column", "newref"}},
				{name: "color creates ref", args: []string{"--color", "newref"}},
				{name: "list multiple patterns", args: []string{"--list", "keep", "absent"}, lowRisk: true},
				{name: "attached contains", args: []string{"--contains=HEAD", "keep", "absent"}, lowRisk: true},
				{name: "separate contains", args: []string{"--contains", "HEAD", "keep", "absent"}, lowRisk: true},
				{name: "attached no contains", args: []string{"--no-contains=HEAD", "keep"}, lowRisk: true},
				{name: "attached merged", args: []string{"--merged=HEAD", "keep"}, lowRisk: true},
				{name: "attached no merged", args: []string{"--no-merged=HEAD", "keep"}, lowRisk: true},
				{name: "attached points at", args: []string{"--points-at=HEAD", "keep", "absent"}, lowRisk: true},
				{name: "separate points at", args: []string{"--points-at", "HEAD", "keep", "absent"}, lowRisk: true},
			} {
				t.Run(test.name, func(t *testing.T) {
					assertGitRefQueryEffects(t, append([]string{subcommand}, test.args...), test.lowRisk)
				})
			}
		})
	}
	for _, args := range [][]string{
		{"branch", "--abbrev", "newref"},
		{"branch", "--abbrev=8", "newref"},
		{"branch", "--sort", "--list", "newref"},
		{"tag", "--format", "--list", "newref"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			assertGitRefQueryEffects(t, args, false)
		})
	}
}

func assertGitRefQueryRisk(t *testing.T, args []string, lowRisk bool) {
	t.Helper()
	command := joinShellCommand(append([]string{"git"}, args...))
	encoded, err := json.Marshal(map[string]any{"scope": "project", "command": command})
	if err != nil {
		t.Fatal(err)
	}
	risk, ok := ClassifyToolCall(CommandRun, encoded)
	if !ok || risk.Class != RiskClassCommand || risk.Operation != "git" || risk.LowRisk != lowRisk {
		t.Fatalf("unexpected risk for git %q: %+v ok=%v, want LowRisk=%v", args, risk, ok, lowRisk)
	}
}

func assertGitRefQueryEffects(t *testing.T, args []string, lowRisk bool) {
	t.Helper()
	root := newGitTestRepository(t, true)
	runGitTest(t, root, args[0], "keep")
	refs := func() string {
		cmd := exec.Command("git", "for-each-ref", "--format=%(refname):%(objectname)")
		cmd.Dir = root
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("read temporary repository refs: %v\n%s", err, output)
		}
		return string(output)
	}
	before := refs()
	assertGitRefQueryRisk(t, args, lowRisk)
	runGitTest(t, root, args...)
	changed := before != refs()
	if changed == lowRisk {
		t.Fatalf("git %q changed refs=%v, want changed=%v", args, changed, !lowRisk)
	}
}

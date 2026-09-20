package tool

import (
	"encoding/json"
	"testing"
)

func TestCommandApprovalStaticShellWrappers(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		command string
		lowRisk bool
	}{
		{`sh -c 'go test ./...'`, true},
		{`sh -c 'go test ./... || exit 1'`, true},
		{`/bin/sh -c 'rg TODO . | head -20'`, true},
		{`bash --noprofile --norc -c 'npm run build'`, true},
		{`sh -c 'printf ok > output.txt'`, true},
		{`sh -c 'rm important.txt'`, false},
		{`sh -c 'git push origin main'`, false},
		{`sh -c 'cat /private/secret.txt'`, false},
		{`sh -c 'printf ok > ../outside.txt'`, false},
		{`sh -c 'cd .. && go test ./...'`, false},
		{`cd .. && sh -c 'go test ./...'`, false},
		{`sh -c 'eval "rm important.txt"'`, false},
		{`sh -c 'builtin eval "rm important.txt"'`, false},
		{`sh -c "sh -c 'go test ./...'"`, true},
		{`sh -c "sh -c 'rm important.txt'"`, false},
		// Stdin code is opaque, just like a Python heredoc or a project script.
		// Auto trusts the sandbox; it does not claim to interpret arbitrary data.
		{`sh -c 'printf "printf ok" | sh'`, true},
		{`sh -c 'printf "printf ok"' | sh -s`, true},
		{`sh -c 'for f in *; do rm "$f"; done'`, false},
		{`sh -c 'printf "%s" "$ARG"'`, true},
		{`sh -c 'go test ./...' ignored`, true},
		{`bash -lc 'go test ./...'`, true},
		{`zsh -c 'go test ./...'`, true},
		{`BASH_ENV=./startup sh -c 'go test ./...'`, false},
	} {
		t.Run(test.command, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"scope": "project", "command": test.command})
			risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{root})
			if !ok || risk.LowRisk != test.lowRisk {
				t.Fatalf("lowRisk=%v, want %v: %+v", risk.LowRisk, test.lowRisk, risk)
			}
		})
	}
}

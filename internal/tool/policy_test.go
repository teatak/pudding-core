package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyToolCallProjectFileWrites(t *testing.T) {
	for _, test := range []struct {
		name      string
		args      string
		operation string
	}{
		{name: FileWrite, args: `{"scope":"project","path":"main.go","content":"x"}`, operation: "write"},
		{name: FileMove, args: `{"scope":"project","from_path":"old.go","to_path":"new.go"}`, operation: "move"},
		{name: FileCopy, args: `{"scope":"project","from_path":"main.go","to_path":"copy.go"}`, operation: "copy"},
		{name: AttachmentExport, args: `{"scope":"project","attachmentKey":"sessions/s1/blobs/capture.png","path":"assets/capture.png"}`, operation: "attachment_export"},
	} {
		risk, ok := ClassifyToolCall(test.name, json.RawMessage(test.args))
		if !ok || risk.Class != RiskClassWrite || risk.Operation != test.operation || !risk.LowRisk {
			t.Fatalf("unexpected project write risk for %s: %+v ok=%v", test.name, risk, ok)
		}
	}
	deleteRisk, ok := ClassifyToolCall(FileDelete, json.RawMessage(`{"scope":"project","path":"main.go"}`))
	if !ok || deleteRisk.Class != RiskClassDestructive || deleteRisk.LowRisk {
		t.Fatalf("file delete must remain protected: %+v ok=%v", deleteRisk, ok)
	}
}

func TestClassifyToolCallRejectsLegacyWorkspaceScope(t *testing.T) {
	if risk, ok := ClassifyToolCall(FileWrite, json.RawMessage(`{"scope":"workspace","path":"main.go","content":"x"}`)); ok {
		t.Fatalf("legacy workspace scope must not be classified as a project write: %+v", risk)
	}
}

func TestClassifyToolCallIgnoresManagedWrites(t *testing.T) {
	if risk, ok := ClassifyToolCall(FileWrite, json.RawMessage(`{"scope":"skill","path":"demo/SKILL.md","content":"x"}`)); ok {
		t.Fatalf("managed writes should not use project approval: %+v", risk)
	}
}

func TestClassifyToolCallAppSaveRisk(t *testing.T) {
	for _, operation := range []string{"create", "update"} {
		risk, ok := ClassifyToolCall(AppSave, json.RawMessage(`{
			"operation":"`+operation+`",
			"app_id":"demo-app",
			"version":"0.1.0",
			"files":[{"path":"app.yaml","content":"id: demo-app"}]
		}`))
		if !ok || risk.Class != RiskClassWrite || risk.Operation != "app_save" || risk.Scope != "app" || risk.LowRisk || len(risk.Paths) != 1 || risk.Paths[0] != "demo-app" {
			t.Fatalf("unexpected App save risk for %s: %+v ok=%v", operation, risk, ok)
		}
	}
	if _, ok := ClassifyToolCall(AppSave, json.RawMessage(`{"operation":"create"}`)); ok {
		t.Fatal("invalid App save must not be classified")
	}
}

func TestClassifyToolCallGitReadRisk(t *testing.T) {
	for _, name := range []string{GitStatus, GitDiff, GitLog} {
		risk, ok := ClassifyToolCall(name, json.RawMessage(`{"scope":"project","cwd":"."}`))
		if !ok || risk.Class != RiskClassRead || !risk.LowRisk || risk.Scope != "project" || len(risk.Paths) != 1 || risk.Paths[0] != "." {
			t.Fatalf("unexpected git read risk for %s: %+v ok=%v", name, risk, ok)
		}
	}
	if risk, ok := ClassifyToolCall(GitStatus, json.RawMessage(`{"scope":"workspace"}`)); ok {
		t.Fatalf("legacy git scope must be rejected: %+v", risk)
	}
}

func TestClassifyToolCallCodeReadRisk(t *testing.T) {
	for _, test := range []struct {
		name string
		args string
	}{
		{name: CodeSymbols, args: `{"scope":"project","path":".","query":"Runner"}`},
		{name: CodeDefinition, args: `{"scope":"project","path":"main.go","line":1,"column":1}`},
		{name: CodeReferences, args: `{"scope":"project","path":"main.go","line":1,"column":1}`},
		{name: CodeDiagnostics, args: `{"scope":"project","paths":["main.go"]}`},
	} {
		risk, ok := ClassifyToolCall(test.name, json.RawMessage(test.args))
		if !ok || risk.Class != RiskClassRead || !risk.LowRisk || risk.Scope != managedScopeProject {
			t.Fatalf("unexpected code read risk for %s: %+v ok=%v", test.name, risk, ok)
		}
	}
	rename, ok := ClassifyToolCall(CodeRename, json.RawMessage(`{"scope":"project","path":"main.go","line":1,"column":1,"new_name":"renamed"}`))
	if !ok || rename.Class != RiskClassWrite || !rename.LowRisk || rename.Operation != "code_rename" || len(rename.Paths) != 1 {
		t.Fatalf("semantic rename must be classified as a project write: %+v ok=%v", rename, ok)
	}
}

func TestClassifyToolCallFilePatchRisk(t *testing.T) {
	risk, ok := ClassifyToolCall(FilePatch, json.RawMessage(`{"scope":"project","files":[{"path":"main.go","edits":[{"old_text":"a","new_text":"b"}]}]}`))
	if !ok || risk.Class != RiskClassWrite || risk.Operation != "file_patch" || risk.Scope != "project" || !risk.LowRisk || len(risk.Paths) != 1 {
		t.Fatalf("unexpected patch apply risk: %+v ok=%v", risk, ok)
	}
	destructive, ok := ClassifyToolCall(FilePatch, json.RawMessage(`{"scope":"project","files":[{"path":"old.go","delete":true}]}`))
	if !ok || destructive.Class != RiskClassDestructive || destructive.LowRisk {
		t.Fatalf("delete patch must remain destructive: %+v ok=%v", destructive, ok)
	}
	if _, ok := ClassifyToolCall(FilePatch, json.RawMessage(`{"scope":"project","files":[]}`)); ok {
		t.Fatal("empty patch must not be classified")
	}
}

func TestClassifyToolCallGitWriteRisk(t *testing.T) {
	for _, tt := range []struct {
		name      string
		args      string
		operation string
		paths     int
	}{
		{name: GitStage, args: `{"scope":"project","paths":["main.go"]}`, operation: "git_stage", paths: 1},
		{name: GitUnstage, args: `{"scope":"project","paths":["main.go"]}`, operation: "git_unstage", paths: 1},
		{name: GitCommit, args: `{"scope":"project","message":"test"}`, operation: "git_commit"},
	} {
		risk, ok := ClassifyToolCall(tt.name, json.RawMessage(tt.args))
		if !ok || risk.Class != RiskClassWrite || risk.Operation != tt.operation || risk.Scope != "project" || risk.LowRisk || len(risk.Paths) != tt.paths {
			t.Fatalf("unexpected Git write risk for %s: %+v ok=%v", tt.name, risk, ok)
		}
	}
	if _, ok := ClassifyToolCall(GitStage, json.RawMessage(`{"scope":"workspace","paths":["main.go"]}`)); ok {
		t.Fatal("legacy Git write scope must not be classified")
	}
}

func TestClassifyToolCallCommandRisk(t *testing.T) {
	tests := []struct {
		name      string
		args      string
		class     RiskClass
		operation string
		lowRisk   bool
	}{
		{name: "test", args: `{"scope":"project","command":"go test ./..."}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "search", args: `{"scope":"project","command":"rg TODO internal"}`, class: RiskClassCommand, operation: "rg", lowRisk: true},
		{name: "mkdir", args: `{"scope":"project","command":"mkdir -p internal/newpkg"}`, class: RiskClassCommand, operation: "mkdir", lowRisk: true},
		{name: "touch", args: `{"scope":"project","command":"touch internal/new.go"}`, class: RiskClassCommand, operation: "touch", lowRisk: true},
		{name: "copy", args: `{"scope":"project","command":"cp main.go main_copy.go"}`, class: RiskClassCommand, operation: "cp", lowRisk: true},
		{name: "move", args: `{"scope":"project","command":"mv main_copy.go main_moved.go"}`, class: RiskClassCommand, operation: "mv", lowRisk: true},
		{name: "slice", args: `{"scope":"project","command":"sed -n '1,80p' main.go"}`, class: RiskClassCommand, operation: "sed", lowRisk: true},
		{name: "find", args: `{"scope":"project","command":"find . -name '*.go'"}`, class: RiskClassCommand, operation: "find", lowRisk: true},
		{name: "find delete", args: `{"scope":"project","command":"find . -delete"}`, class: RiskClassCommand, operation: "find", lowRisk: false},
		{name: "fd exec", args: `{"scope":"project","command":"fd --exec=rm '{}'"}`, class: RiskClassCommand, operation: "fd", lowRisk: false},
		{name: "test with safe env", args: `{"scope":"project","command":"go test ./...","env":{"GOFLAGS":"-race"}}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "toolchain wrapper env", args: `{"scope":"project","command":"go test ./...","env":{"CC":"./scripts/compiler-wrapper"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "go toolexec env", args: `{"scope":"project","command":"go test ./...","env":{"GOFLAGS":"-toolexec=./scripts/wrapper"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "go env file override", args: `{"scope":"project","command":"go test ./...","env":{"GOENV":"./config/go-env"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "make eval environment", args: `{"scope":"project","command":"make test","env":{"MAKEFLAGS":"--eval=all:; ./scripts/run"}}`, class: RiskClassCommand, operation: "make", lowRisk: false},
		{name: "risky env", args: `{"scope":"project","command":"go test ./...","env":{"PATH":"./bin"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "test outside project", args: `{"scope":"project","command":"go test ../other"}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "unknown command", args: `{"scope":"project","command":"my-project-tool --check"}`, class: RiskClassCommand, operation: "my-project-tool", lowRisk: true},
		{name: "project interpreter", args: `{"scope":"project","command":"python3 script.py"}`, class: RiskClassCommand, operation: "python3", lowRisk: true},
		{name: "inline interpreter", args: `{"scope":"project","command":"python3 -c \"print('ok')\""}`, class: RiskClassCommand, operation: "python3", lowRisk: true},
		{name: "attached inline interpreter", args: `{"scope":"project","command":"node \"-econsole.log('ok')\""}`, class: RiskClassCommand, operation: "node", lowRisk: true},
		{name: "inline node", args: `{"scope":"project","command":"node \"--eval=console.log('ok')\""}`, class: RiskClassCommand, operation: "node", lowRisk: true},
		{name: "inline awk", args: `{"scope":"project","command":"awk 'BEGIN { system(\"rm -rf build\") }'"}`, class: RiskClassCommand, operation: "awk", lowRisk: false},
		{name: "awk script file", args: `{"scope":"project","command":"awk -f scripts/report.awk data.txt"}`, class: RiskClassCommand, operation: "awk", lowRisk: true},
		{name: "go run", args: `{"scope":"project","command":"go run ./cmd/server"}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "dev script", args: `{"scope":"project","command":"npm run dev"}`, class: RiskClassCommand, operation: "npm", lowRisk: true},
		{name: "dev server wildcard host", args: `{"scope":"project","command":"npm run dev -- --host 0.0.0.0"}`, class: RiskClassCommand, operation: "npm", lowRisk: false},
		{name: "dev server wildcard host env", args: `{"scope":"project","command":"npm run dev","env":{"HOST":"0.0.0.0"}}`, class: RiskClassCommand, operation: "npm", lowRisk: false},
		{name: "literal wildcard search", args: `{"scope":"project","command":"rg '*'"}`, class: RiskClassCommand, operation: "rg", lowRisk: true},
		{name: "publish", args: `{"scope":"project","command":"npm publish"}`, class: RiskClassCommand, operation: "npm", lowRisk: false},
		{name: "git write", args: `{"scope":"project","command":"git add main.go"}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git clone", args: `{"scope":"project","command":"git clone https://example.com/repo.git repo"}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git fetch", args: `{"scope":"project","command":"git fetch origin"}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git pull", args: `{"scope":"project","command":"git pull --ff-only"}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git pull strategy option", args: `{"scope":"project","command":"git pull -Xours"}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git push", args: `{"scope":"project","command":"git push origin main"}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git clone config injection", args: `{"scope":"project","command":"git clone -c 'core.fsmonitor=!touch compromised' https://example.com/repo.git"}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git pull exec", args: `{"scope":"project","command":"git pull --rebase --exec=./scripts/run"}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git pull short exec", args: `{"scope":"project","command":"git pull --rebase -x ./scripts/run"}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "local curl", args: `{"scope":"project","command":"curl http://127.0.0.1:3000/health"}`, class: RiskClassCommand, operation: "curl", lowRisk: true},
		{name: "local ipv6 curl", args: `{"scope":"project","command":"curl 'http://[::1]:3000/health'"}`, class: RiskClassCommand, operation: "curl", lowRisk: true},
		{name: "external curl", args: `{"scope":"project","command":"curl https://example.com"}`, class: RiskClassCommand, operation: "curl", lowRisk: false},
		{name: "wrapper", args: `{"scope":"project","command":"env rm -rf ."}`, class: RiskClassCommand, operation: "env", lowRisk: false},
		{name: "utility wrapper", args: `{"scope":"project","command":"stdbuf -oL go test ./..."}`, class: RiskClassCommand, operation: "stdbuf", lowRisk: false},
		{name: "pipeline", args: `{"scope":"project","command":"go test ./... | tee test.log"}`, class: RiskClassCommand, operation: "shell", lowRisk: true},
		{name: "dynamic expansion", args: `{"scope":"project","command":"printf '%s' \"$TOKEN\""}`, class: RiskClassCommand, operation: "printf", lowRisk: false},
		{name: "destructive", args: `{"scope":"project","command":"rm -rf build"}`, class: RiskClassDestructive, operation: "rm", lowRisk: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(tt.args))
			if !ok || risk.Class != tt.class || risk.Operation != tt.operation || risk.LowRisk != tt.lowRisk || risk.Scope != "project" {
				t.Fatalf("unexpected command risk: %+v ok=%v", risk, ok)
			}
		})
	}
}

func TestClassifyToolCallCommandUsesAuthorizedProjectPaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "golang study")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	inside := filepath.Join(root, "tutorials", "Day08_GORM")
	raw, _ := json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{"mkdir", "-p", inside})})
	risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("authorized absolute mkdir should be low risk: %+v ok=%v", risk, ok)
	}

	outside := filepath.Join(t.TempDir(), "Day08_GORM")
	raw, _ = json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{"mkdir", "-p", outside})})
	risk, ok = ClassifyToolCallForProject(CommandRun, raw, []string{root})
	if !ok || risk.LowRisk {
		t.Fatalf("outside absolute mkdir must require approval: %+v ok=%v", risk, ok)
	}

	raw, _ = json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{"git", "-C", root, "log", "--oneline", "-5"})})
	risk, ok = ClassifyToolCallForProject(CommandRun, raw, []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("read-only git with an authorized -C path should be low risk: %+v ok=%v", risk, ok)
	}

	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","command":"git -c 'core.fsmonitor=!touch compromised' status"}`), []string{root})
	if !ok || risk.LowRisk {
		t.Fatalf("git config injection must require approval: %+v ok=%v", risk, ok)
	}

	escapeRoot := t.TempDir()
	if err := os.Symlink(escapeRoot, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape", "nested")
	raw, _ = json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{"mkdir", "-p", escape})})
	risk, ok = ClassifyToolCallForProject(CommandRun, raw, []string{root})
	if !ok || risk.LowRisk {
		t.Fatalf("mkdir through an escaping symlink must require approval: %+v ok=%v", risk, ok)
	}
}

func TestClassifyToolCallCommandRedirectionBoundary(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "reports", "test.log")
	insideRaw, _ := json.Marshal(map[string]any{
		"scope":   "project",
		"command": "go test ./... > " + quoteShellArg(inside),
	})
	insideRisk, ok := ClassifyToolCallForProject(CommandRun, insideRaw, []string{root})
	if !ok || !insideRisk.LowRisk {
		t.Fatalf("project-local output redirection should be low risk: %+v ok=%v", insideRisk, ok)
	}

	outside := filepath.Join(t.TempDir(), "test.log")
	outsideRaw, _ := json.Marshal(map[string]any{
		"scope":   "project",
		"command": "go test ./... > " + quoteShellArg(outside),
	})
	outsideRisk, ok := ClassifyToolCallForProject(CommandRun, outsideRaw, []string{root})
	if !ok || outsideRisk.LowRisk {
		t.Fatalf("outside output redirection must require approval: %+v ok=%v", outsideRisk, ok)
	}

	dynamicRisk, ok := ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","command":"printf ok > \"$OUTPUT\""}`), []string{root})
	if !ok || dynamicRisk.LowRisk {
		t.Fatalf("dynamic output redirection must require approval: %+v ok=%v", dynamicRisk, ok)
	}

	tempRisk, ok := ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","command":"cat > \"$TMPDIR/report.py\""}`), []string{root})
	if !ok || !tempRisk.LowRisk || tempRisk.SandboxBypass {
		t.Fatalf("sandbox-managed temporary output should remain low risk: %+v ok=%v", tempRisk, ok)
	}

	tempScriptRisk, ok := ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","command":"python3 \"$TMPDIR/report.py\""}`), []string{root})
	if !ok || !tempScriptRisk.LowRisk || tempScriptRisk.SandboxBypass {
		t.Fatalf("sandbox-managed temporary script should remain low risk: %+v ok=%v", tempScriptRisk, ok)
	}
}

func TestClassifyToolCallCommandSeparatesApprovalFromSandboxBypass(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "report.txt")
	outsideScript := filepath.Join(t.TempDir(), "report.py")
	tests := []struct {
		name    string
		command string
		bypass  bool
	}{
		{name: "destructive project command", command: "rm -rf build"},
		{name: "inline Python", command: `python3 -c "print('ok')"`},
		{name: "external curl", command: "curl https://example.com"},
		{name: "host package manager", command: "brew install mysql-client", bypass: true},
		{name: "Git push credentials", command: "git push origin main", bypass: true},
		{name: "outside output", command: "printf ok > " + quoteShellArg(outside), bypass: true},
		{name: "outside read", command: "cat " + quoteShellArg(outside), bypass: true},
		{name: "outside script", command: "python3 " + quoteShellArg(outsideScript), bypass: true},
		{name: "outside destructive path", command: "rm -rf " + quoteShellArg(outside), bypass: true},
		{name: "outside PATH", command: "my-tool --check", bypass: true},
		{name: "absolute regex is not a path", command: `sed -n '/Users/p' README.md`},
		{name: "absolute search pattern is not a path", command: `rg '/Users' .`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := map[string]any{"scope": "project", "command": test.command}
			if test.name == "outside PATH" {
				args["env"] = map[string]string{"PATH": filepath.Dir(outside)}
			}
			raw, _ := json.Marshal(args)
			risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{root})
			if !ok || risk.SandboxBypass != test.bypass {
				t.Fatalf("sandbox bypass = %v, want %v: %+v", risk.SandboxBypass, test.bypass, risk)
			}
		})
	}

	wildcardRaw := json.RawMessage(`{"scope":"project","command":"npm run dev","env":{"HOST":"0.0.0.0"}}`)
	wildcardRisk, ok := ClassifyToolCallForProject(CommandRun, wildcardRaw, []string{root})
	if !ok || wildcardRisk.LowRisk || wildcardRisk.SandboxBypass {
		t.Fatalf("wildcard bind must require approval without full filesystem access: %+v ok=%v", wildcardRisk, ok)
	}
}

func TestClassifyToolCallCommandAcceptsCanonicalProjectRootAlias(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(base, "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{"find", realRoot, "-type", "f", "-name", "*.go"})})
	risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{aliasRoot})
	if !ok || !risk.LowRisk {
		t.Fatalf("read-only find through canonical project alias should be low risk: %+v ok=%v", risk, ok)
	}
}

func TestClassifyToolCallCommandExecutableBoundary(t *testing.T) {
	risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(`{"scope":"project","command":"/tmp/go test ./..."}`))
	if !ok || risk.LowRisk {
		t.Fatalf("external executable must require approval: %+v ok=%v", risk, ok)
	}

	root := t.TempDir()
	executable := filepath.Join(root, "scripts", "check")
	if err := os.MkdirAll(filepath.Dir(executable), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"scope": "project", "command": joinShellCommand([]string{executable})})
	risk, ok = ClassifyToolCallForProject(CommandRun, raw, []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("project executable should be auto-approved in the sandbox: %+v ok=%v", risk, ok)
	}
	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","command":"/bin/ls"}`), []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("system executable should be auto-approved in the sandbox: %+v ok=%v", risk, ok)
	}
}

func TestClassifyToolCallBackgroundCommandUsesSameRiskRules(t *testing.T) {
	for _, test := range []struct {
		name    string
		args    string
		class   RiskClass
		lowRisk bool
	}{
		{name: "dev server", args: `{"scope":"project","command":"npm run dev","background":true}`, class: RiskClassCommand, lowRisk: true},
		{name: "unknown server", args: `{"scope":"project","command":"my-server --watch","background":true}`, class: RiskClassCommand, lowRisk: true},
		{name: "pipeline", args: `{"scope":"project","command":"npm run dev | tee dev.log","background":true}`, class: RiskClassCommand, lowRisk: true},
		{name: "destructive", args: `{"scope":"project","command":"rm -rf build","background":true}`, class: RiskClassDestructive, lowRisk: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(test.args))
			if !ok || risk.Class != test.class || risk.LowRisk != test.lowRisk || risk.Operation != "process_start" {
				t.Fatalf("unexpected background command risk: %+v ok=%v", risk, ok)
			}
		})
	}
}

func TestClassifyToolCallRejectsLegacyCommandScope(t *testing.T) {
	if risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(`{"scope":"workspace","command":"go test ./..."}`)); ok {
		t.Fatalf("legacy command scope must be rejected: %+v", risk)
	}
}

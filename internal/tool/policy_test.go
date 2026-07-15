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
		{name: FilePatch, args: `{"scope":"project","path":"main.go","old_string":"a","new_string":"b"}`, operation: "patch"},
		{name: FileMove, args: `{"scope":"project","from_path":"old.go","to_path":"new.go"}`, operation: "move"},
		{name: FileCopy, args: `{"scope":"project","from_path":"main.go","to_path":"copy.go"}`, operation: "copy"},
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
	if risk, ok := ClassifyToolCall(FileWrite, json.RawMessage(`{"scope":"skill_draft","path":"demo/SKILL.md","content":"x"}`)); ok {
		t.Fatalf("managed writes should not use project approval: %+v", risk)
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
		{name: CodeRename, args: `{"scope":"project","path":"main.go","line":1,"column":1,"new_name":"renamed"}`},
	} {
		risk, ok := ClassifyToolCall(test.name, json.RawMessage(test.args))
		if !ok || risk.Class != RiskClassRead || !risk.LowRisk || risk.Scope != managedScopeProject {
			t.Fatalf("unexpected code read risk for %s: %+v ok=%v", test.name, risk, ok)
		}
	}
}

func TestClassifyToolCallPatchApplyRisk(t *testing.T) {
	risk, ok := ClassifyToolCall(PatchApply, json.RawMessage(`{"proposal_id":"patch_123"}`))
	if !ok || risk.Class != RiskClassWrite || risk.Operation != "patch_apply" || risk.Scope != "project" || !risk.LowRisk {
		t.Fatalf("unexpected patch apply risk: %+v ok=%v", risk, ok)
	}
	if _, ok := ClassifyToolCall(PatchApply, json.RawMessage(`{"proposal_id":""}`)); ok {
		t.Fatal("empty patch proposal id must not be classified")
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
		{name: "test", args: `{"scope":"project","argv":["go","test","./..."]}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "search", args: `{"scope":"project","argv":["rg","TODO","internal"]}`, class: RiskClassCommand, operation: "rg", lowRisk: true},
		{name: "mkdir", args: `{"scope":"project","argv":["mkdir","-p","internal/newpkg"]}`, class: RiskClassCommand, operation: "mkdir", lowRisk: true},
		{name: "touch", args: `{"scope":"project","argv":["touch","internal/new.go"]}`, class: RiskClassCommand, operation: "touch", lowRisk: true},
		{name: "copy", args: `{"scope":"project","argv":["cp","main.go","main_copy.go"]}`, class: RiskClassCommand, operation: "cp", lowRisk: true},
		{name: "move", args: `{"scope":"project","argv":["mv","main_copy.go","main_moved.go"]}`, class: RiskClassCommand, operation: "mv", lowRisk: true},
		{name: "slice", args: `{"scope":"project","argv":["sed","-n","1,80p","main.go"]}`, class: RiskClassCommand, operation: "sed", lowRisk: true},
		{name: "find", args: `{"scope":"project","argv":["find",".","-name","*.go"]}`, class: RiskClassCommand, operation: "find", lowRisk: true},
		{name: "find delete", args: `{"scope":"project","argv":["find",".","-delete"]}`, class: RiskClassCommand, operation: "find", lowRisk: false},
		{name: "fd exec", args: `{"scope":"project","argv":["fd","--exec=rm","{}"]}`, class: RiskClassCommand, operation: "fd", lowRisk: false},
		{name: "test with safe env", args: `{"scope":"project","argv":["go","test","./..."],"env":{"GOFLAGS":"-race"}}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "toolchain wrapper env", args: `{"scope":"project","argv":["go","test","./..."],"env":{"CC":"./scripts/compiler-wrapper"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "go toolexec env", args: `{"scope":"project","argv":["go","test","./..."],"env":{"GOFLAGS":"-toolexec=./scripts/wrapper"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "go env file override", args: `{"scope":"project","argv":["go","test","./..."],"env":{"GOENV":"./config/go-env"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "make eval environment", args: `{"scope":"project","argv":["make","test"],"env":{"MAKEFLAGS":"--eval=all:; ./scripts/run"}}`, class: RiskClassCommand, operation: "make", lowRisk: false},
		{name: "risky env", args: `{"scope":"project","argv":["go","test","./..."],"env":{"PATH":"./bin"}}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "test outside project", args: `{"scope":"project","argv":["go","test","../other"]}`, class: RiskClassCommand, operation: "go", lowRisk: false},
		{name: "unknown direct command", args: `{"scope":"project","argv":["my-project-tool","--check"]}`, class: RiskClassCommand, operation: "my-project-tool", lowRisk: true},
		{name: "project interpreter", args: `{"scope":"project","argv":["python3","script.py"]}`, class: RiskClassCommand, operation: "python3", lowRisk: true},
		{name: "inline interpreter", args: `{"scope":"project","argv":["python3","-c","print('ok')"]}`, class: RiskClassCommand, operation: "python3", lowRisk: false},
		{name: "attached inline interpreter", args: `{"scope":"project","argv":["node","-econsole.log('ok')"]}`, class: RiskClassCommand, operation: "node", lowRisk: false},
		{name: "inline node", args: `{"scope":"project","argv":["node","--eval=console.log('ok')"]}`, class: RiskClassCommand, operation: "node", lowRisk: false},
		{name: "inline awk", args: `{"scope":"project","argv":["awk","BEGIN { system(\"rm -rf build\") }"]}`, class: RiskClassCommand, operation: "awk", lowRisk: false},
		{name: "awk script file", args: `{"scope":"project","argv":["awk","-f","scripts/report.awk","data.txt"]}`, class: RiskClassCommand, operation: "awk", lowRisk: true},
		{name: "go run", args: `{"scope":"project","argv":["go","run","./cmd/server"]}`, class: RiskClassCommand, operation: "go", lowRisk: true},
		{name: "dev script", args: `{"scope":"project","argv":["npm","run","dev"]}`, class: RiskClassCommand, operation: "npm", lowRisk: true},
		{name: "dev server wildcard host", args: `{"scope":"project","argv":["npm","run","dev","--","--host","0.0.0.0"]}`, class: RiskClassCommand, operation: "npm", lowRisk: false},
		{name: "dev server wildcard host env", args: `{"scope":"project","argv":["npm","run","dev"],"env":{"HOST":"0.0.0.0"}}`, class: RiskClassCommand, operation: "npm", lowRisk: false},
		{name: "literal wildcard search", args: `{"scope":"project","argv":["rg","*"]}`, class: RiskClassCommand, operation: "rg", lowRisk: true},
		{name: "publish", args: `{"scope":"project","argv":["npm","publish"]}`, class: RiskClassCommand, operation: "npm", lowRisk: false},
		{name: "git write", args: `{"scope":"project","argv":["git","add","main.go"]}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git clone", args: `{"scope":"project","argv":["git","clone","https://example.com/repo.git","repo"]}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git fetch", args: `{"scope":"project","argv":["git","fetch","origin"]}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git pull", args: `{"scope":"project","argv":["git","pull","--ff-only"]}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git pull strategy option", args: `{"scope":"project","argv":["git","pull","-Xours"]}`, class: RiskClassCommand, operation: "git", lowRisk: true},
		{name: "git push", args: `{"scope":"project","argv":["git","push","origin","main"]}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git clone config injection", args: `{"scope":"project","argv":["git","clone","-c","core.fsmonitor=!touch compromised","https://example.com/repo.git"]}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git pull exec", args: `{"scope":"project","argv":["git","pull","--rebase","--exec=./scripts/run"]}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "git pull short exec", args: `{"scope":"project","argv":["git","pull","--rebase","-x","./scripts/run"]}`, class: RiskClassCommand, operation: "git", lowRisk: false},
		{name: "local curl", args: `{"scope":"project","argv":["curl","http://127.0.0.1:3000/health"]}`, class: RiskClassCommand, operation: "curl", lowRisk: true},
		{name: "local ipv6 curl", args: `{"scope":"project","argv":["curl","http://[::1]:3000/health"]}`, class: RiskClassCommand, operation: "curl", lowRisk: true},
		{name: "external curl", args: `{"scope":"project","argv":["curl","https://example.com"]}`, class: RiskClassCommand, operation: "curl", lowRisk: false},
		{name: "wrapper", args: `{"scope":"project","argv":["env","rm","-rf","."]}`, class: RiskClassCommand, operation: "env", lowRisk: false},
		{name: "utility wrapper", args: `{"scope":"project","argv":["stdbuf","-oL","go","test","./..."]}`, class: RiskClassCommand, operation: "stdbuf", lowRisk: false},
		{name: "shell", args: `{"scope":"project","script":"go test ./... | tee test.log"}`, class: RiskClassCommand, operation: "shell", lowRisk: false},
		{name: "destructive", args: `{"scope":"project","argv":["rm","-rf","build"]}`, class: RiskClassDestructive, operation: "rm", lowRisk: false},
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
	risk, ok := ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","argv":["mkdir","-p","`+inside+`"]}`), []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("authorized absolute mkdir should be low risk: %+v ok=%v", risk, ok)
	}

	outside := filepath.Join(t.TempDir(), "Day08_GORM")
	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","argv":["mkdir","-p","`+outside+`"]}`), []string{root})
	if !ok || risk.LowRisk {
		t.Fatalf("outside absolute mkdir must require approval: %+v ok=%v", risk, ok)
	}

	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","argv":["git","-C","`+root+`","log","--oneline","-5"]}`), []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("read-only git with an authorized -C path should be low risk: %+v ok=%v", risk, ok)
	}

	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","argv":["git","-c","core.fsmonitor=!touch compromised","status"]}`), []string{root})
	if !ok || risk.LowRisk {
		t.Fatalf("git config injection must require approval: %+v ok=%v", risk, ok)
	}

	escapeRoot := t.TempDir()
	if err := os.Symlink(escapeRoot, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(root, "escape", "nested")
	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","argv":["mkdir","-p","`+escape+`"]}`), []string{root})
	if !ok || risk.LowRisk {
		t.Fatalf("mkdir through an escaping symlink must require approval: %+v ok=%v", risk, ok)
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
	raw := json.RawMessage(`{"scope":"project","argv":["find","` + realRoot + `","-type","f","-name","*.go"]}`)
	risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{aliasRoot})
	if !ok || !risk.LowRisk {
		t.Fatalf("read-only find through canonical project alias should be low risk: %+v ok=%v", risk, ok)
	}
}

func TestClassifyToolCallCommandExecutableBoundary(t *testing.T) {
	risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(`{"scope":"project","argv":["/tmp/go","test","./..."]}`))
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
	raw, _ := json.Marshal(map[string]any{"scope": "project", "argv": []string{executable}})
	risk, ok = ClassifyToolCallForProject(CommandRun, raw, []string{root})
	if !ok || !risk.LowRisk {
		t.Fatalf("project executable should be auto-approved in the sandbox: %+v ok=%v", risk, ok)
	}
	risk, ok = ClassifyToolCallForProject(CommandRun, json.RawMessage(`{"scope":"project","argv":["/bin/ls"]}`), []string{root})
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
		{name: "dev server", args: `{"scope":"project","argv":["npm","run","dev"]}`, class: RiskClassCommand, lowRisk: true},
		{name: "unknown server", args: `{"scope":"project","argv":["my-server","--watch"]}`, class: RiskClassCommand, lowRisk: true},
		{name: "shell", args: `{"scope":"project","script":"npm run dev"}`, class: RiskClassCommand, lowRisk: false},
		{name: "destructive", args: `{"scope":"project","argv":["rm","-rf","build"]}`, class: RiskClassDestructive, lowRisk: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			risk, ok := ClassifyToolCall(CommandStart, json.RawMessage(test.args))
			if !ok || risk.Class != test.class || risk.LowRisk != test.lowRisk || risk.Operation != "process_start" {
				t.Fatalf("unexpected background command risk: %+v ok=%v", risk, ok)
			}
		})
	}
}

func TestClassifyToolCallRejectsLegacyCommandScope(t *testing.T) {
	if risk, ok := ClassifyToolCall(CommandRun, json.RawMessage(`{"scope":"workspace","argv":["go","test","./..."]}`)); ok {
		t.Fatalf("legacy command scope must be rejected: %+v", risk)
	}
}

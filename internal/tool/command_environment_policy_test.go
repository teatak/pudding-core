package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestProjectPythonEnvironmentApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows commands require host approval")
	}
	root, outside := t.TempDir(), t.TempDir()
	link := filepath.Join(root, "external")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, key, value string
		allowed          bool
	}{
		{"libraries", "PYTHONPATH", root, true}, {"venv", "VIRTUAL_ENV", root, true},
		{"multiple libraries", "PYTHONPATH", root + string(os.PathListSeparator) + root, true},
		{"outside", "PYTHONPATH", outside, false}, {"symlink escape", "VIRTUAL_ENV", link, false},
		{"implicit cwd", "PYTHONPATH", root + string(os.PathListSeparator), false},
		{"relative", "PYTHONPATH", "src", false}, {"missing", "VIRTUAL_ENV", filepath.Join(root, "missing"), false},
		{"empty", "VIRTUAL_ENV", "", false}, {"loader", "NODE_OPTIONS", "--require=./loader.js", false},
		{"path", "PATH", root, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, inline := range []bool{false, true} {
				args := map[string]any{"scope": "project", "command": "python3 -m pytest tests"}
				if inline {
					args["command"] = tc.key + "=" + quoteShellArg(tc.value) + " python3 -m pytest tests"
				} else {
					args["env"] = map[string]string{tc.key: tc.value}
				}
				raw, _ := json.Marshal(args)
				risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{root})
				if !ok || risk.LowRisk != tc.allowed || slices.Contains(risk.ApprovalReasons, "custom_environment") == tc.allowed {
					t.Fatalf("inline=%v risk=%+v", inline, risk)
				}
			}
		})
	}
}

func TestSandboxToolchainPATHApproval(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PowerShell commands require host approval")
	}
	home, project := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	versions := filepath.Join(home, ".pyenv", "versions")
	bin := filepath.Join(versions, "3", "bin")
	writableBin := filepath.Join(versions, "dev", "bin")
	cacheBin := filepath.Join(home, ".cache", "pip", "bin")
	for _, dir := range []string{bin, writableBin, cacheBin} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(versions, "other", "bin")
	if err := os.MkdirAll(filepath.Dir(link), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(project, link); err != nil {
		t.Fatal(err)
	}
	roots := []string{project, filepath.Dir(writableBin)}
	for _, tc := range []struct {
		name, path string
		allowed    bool
	}{
		{"system", "/usr/bin", true}, {"installed tools", bin + ":/usr/bin", true},
		{"empty", "", false}, {"empty component", bin + ":", false}, {"current directory", ".", false},
		{"relative directory", "tools/bin", false}, {"untrusted directory", project, false},
		{"writable toolchain", writableBin, false}, {"link outside tools", link, false},
		{"cache", cacheBin, false}, {"missing toolchain", filepath.Join(versions, "missing", "bin"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, form := range []string{"structured", "inline", "prefix"} {
				args := map[string]any{"scope": "project", "cwd": project, "command": "go test ./... && git status"}
				switch form {
				case "structured":
					args["env"] = map[string]string{"PATH": tc.path}
				case "inline":
					args["command"] = "PATH=" + quoteShellArg(tc.path) + " go test ./... && git status"
				case "prefix":
					args["command"] = `PATH="` + tc.path + `:$PATH" go test ./... && git status`
				}
				raw, _ := json.Marshal(args)
				risk, ok := ClassifyToolCallForProject(CommandRun, raw, roots)
				if !ok || risk.LowRisk != tc.allowed || slices.Contains(risk.ApprovalTriggers["custom_environment"], "PATH") == tc.allowed {
					t.Fatalf("%s: %+v", form, risk)
				}
			}
		})
	}
	for _, assignment := range []string{`"/usr/bin$PATH"`, `"/usr/bin:$OTHER"`, `"$PATH:/usr/bin"`, `"/usr/bin:${PATH:-.}"`, `'/usr/bin:$PATH'`, `"/usr/bin:$(pwd):$PATH"`} {
		raw, _ := json.Marshal(map[string]any{"scope": "project", "cwd": project, "command": "PATH=" + assignment + " go test"})
		risk, ok := ClassifyToolCallForProject(CommandRun, raw, roots)
		if !ok || risk.LowRisk || !slices.Contains(risk.ApprovalReasons, "custom_environment") {
			t.Fatalf("%s: %+v", assignment, risk)
		}
	}
	for _, tc := range []struct {
		command string
		allowed bool
	}{
		{`unset PATH && PATH="/usr/bin:$PATH" go test`, false},
		{`for PATH in .; do PATH="/usr/bin:$PATH" go test; done`, false},
		{`printf -v BUILD_LABEL '%s' test && PATH="/usr/bin:$PATH" go test`, false},
		{`printf -vBUILD_LABEL '%s' test && PATH="/usr/bin:$PATH" go test`, false},
		{`printf '%s\n' test && PATH="/usr/bin:$PATH" go test`, true},
		{`printf "%s\n" test && PATH="/usr/bin:$PATH" go test`, true},
		{`printf -- '-v%s' test && PATH="/usr/bin:$PATH" go test`, true},
	} {
		raw, _ := json.Marshal(map[string]any{"scope": "project", "cwd": project, "command": tc.command})
		risk, ok := ClassifyToolCallForProject(CommandRun, raw, roots)
		if !ok || risk.LowRisk != tc.allowed || slices.Contains(risk.ApprovalTriggers["custom_environment"], "PATH") == tc.allowed {
			t.Fatalf("%s: %+v", tc.command, risk)
		}
	}
	raw := json.RawMessage(`{"scope":"project","command":"PATH=/usr/bin go test && rm artifact","env":{"NODE_OPTIONS":"--require=private.js","LANG":"C"}}`)
	risk, ok := ClassifyToolCallForProject(CommandRun, raw, []string{project})
	triggers, _ := json.Marshal(risk.ApprovalTriggers)
	if !ok || risk.LowRisk || !slices.Equal(risk.ApprovalTriggers["destructive_command"], []string{"#2 rm"}) || !slices.Equal(risk.ApprovalTriggers["custom_environment"], []string{"NODE_OPTIONS"}) || strings.Contains(string(triggers), "private") {
		t.Fatalf("unexpected triggers: %s", triggers)
	}
}

func TestTempWorkspaceResourceOwnership(t *testing.T) {
	for _, raw := range []string{`{"scope":"temp"}`, `{"from":{"scope":"temp"}}`, `{"to":{"scope":"temp"}}`} {
		if !UsesSessionArtifacts(Call{Name: FileCopy, Args: json.RawMessage(raw)}) {
			t.Fatalf("temp access has no lifecycle lock: %s", raw)
		}
	}
	if UsesSessionArtifacts(Call{Name: FileRead, Args: json.RawMessage(`{"scope":"project"}`)}) {
		t.Fatal("unrelated project read gained a scratch lock")
	}
}

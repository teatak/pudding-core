package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
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

package tool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/teatak/pudding-core/internal/store"
)

func TestMutationTrackingForCall(t *testing.T) {
	root := t.TempDir()
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		call        Call
		wantOK      bool
		wantTargets []string
		wantOrigin  store.FileChangeOrigin
	}{
		{
			name:        "foreground command tracks explicit target",
			call:        Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"touch changed.txt"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "changed.txt")},
			wantOrigin:  store.FileChangeOriginCommandObserved,
		},
		{
			name:        "command target is relative to cwd",
			call:        Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","cwd":"sub","command":"printf hi > generated.txt"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "sub", "generated.txt")},
			wantOrigin:  store.FileChangeOriginCommandObserved,
		},
		{
			name:        "package manager formatter tracks explicit target",
			call:        Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","cwd":"sub","command":"pnpm exec prettier --write view.tsx"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "sub", "view.tsx")},
			wantOrigin:  store.FileChangeOriginCommandObserved,
		},
		{
			name:   "package manager script remains opaque",
			call:   Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"pnpm run format"}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
		{
			name:   "background command is not finalized with the call",
			call:   Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"sleep 10","background":true}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
		{
			name:        "write owns one project path",
			call:        Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"project","path":"dir/file.txt","content":"new"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "dir", "file.txt")},
			wantOrigin:  store.FileChangeOriginStructured,
		},
		{
			name:        "copy owns only destination",
			call:        Call{Name: FileCopy, Args: json.RawMessage(`{"scope":"project","from_path":"source.txt","to_path":"copy.txt"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantTargets: []string{filepath.Join(resolvedRoot, "copy.txt")},
			wantOrigin:  store.FileChangeOriginStructured,
		},
		{
			name:   "project root target does not expand command tracking",
			call:   Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"prettier --write ."}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
		{
			name:   "dynamic command target is excluded",
			call:   Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"touch \"$TARGET\""}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
		{
			name:   "glob command target is excluded",
			call:   Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"gofmt -w *.go"}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
		{
			name:   "non-project write is excluded",
			call:   Call{Name: FileWrite, Args: json.RawMessage(`{"scope":"temp","path":"file.txt","content":"new"}`), ProjectDirs: []string{root}},
			wantOK: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := MutationTrackingForCall(test.call)
			if ok != test.wantOK {
				t.Fatalf("ok = %v, want %v, tracking = %+v", ok, test.wantOK, got)
			}
			if !ok {
				return
			}
			if len(got.Targets) != len(test.wantTargets) {
				t.Fatalf("targets = %v, want %v", got.Targets, test.wantTargets)
			}
			for i := range test.wantTargets {
				if got.Targets[i] != test.wantTargets[i] {
					t.Fatalf("targets = %v, want %v", got.Targets, test.wantTargets)
				}
			}
			if got.Origin != test.wantOrigin {
				t.Fatalf("origin = %q, want %q", got.Origin, test.wantOrigin)
			}
		})
	}
}

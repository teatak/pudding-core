package tool

import (
	"encoding/json"
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
	tests := []struct {
		name        string
		call        Call
		wantOK      bool
		wantOrigin  store.FileChangeOrigin
		wantTargets []string
	}{
		{
			name:       "foreground command observes project",
			call:       Call{Name: CommandRun, Args: json.RawMessage(`{"scope":"project","command":"touch changed.txt"}`), ProjectDirs: []string{root}},
			wantOK:     true,
			wantOrigin: store.FileChangeOriginCommandObserved,
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
			wantOrigin:  store.FileChangeOriginStructured,
			wantTargets: []string{filepath.Join(resolvedRoot, "dir", "file.txt")},
		},
		{
			name:        "copy owns only destination",
			call:        Call{Name: FileCopy, Args: json.RawMessage(`{"scope":"project","from_path":"source.txt","to_path":"copy.txt"}`), ProjectDirs: []string{root}},
			wantOK:      true,
			wantOrigin:  store.FileChangeOriginStructured,
			wantTargets: []string{filepath.Join(resolvedRoot, "copy.txt")},
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
			if got.Origin != test.wantOrigin {
				t.Fatalf("origin = %q, want %q", got.Origin, test.wantOrigin)
			}
			if len(got.Targets) != len(test.wantTargets) {
				t.Fatalf("targets = %v, want %v", got.Targets, test.wantTargets)
			}
			for i := range test.wantTargets {
				if got.Targets[i] != test.wantTargets[i] {
					t.Fatalf("targets = %v, want %v", got.Targets, test.wantTargets)
				}
			}
		})
	}
}

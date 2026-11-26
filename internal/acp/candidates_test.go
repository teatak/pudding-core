package acp

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestWindowsCandidateExecutables(t *testing.T) {
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	appData := filepath.Join(home, "AppData", "Local")
	bundled := filepath.Join(home, ".gemini", "bin", "agy.exe")
	standalone := filepath.Join(appData, "agy", "bin", "agy.exe")
	for _, path := range []string{bundled, standalone} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, nil, 0700); err != nil {
			t.Fatal(err)
		}
		if path == bundled {
			if got := candidateExecutables("windows", home, appData, ""); !slices.Equal(got, []string{bundled}) {
				t.Fatalf("IDE only, empty PATH: got %v", got)
			}
		}
	}
	if got := candidateExecutables("windows", home, appData, standalone); !slices.Equal(got, []string{standalone, bundled}) {
		t.Fatalf("standalone also on PATH: got %v", got)
	}
}

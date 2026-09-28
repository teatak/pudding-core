package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDraftFilesPersistUntilExplicitCommit(t *testing.T) {
	home := t.TempDir()
	id := "canvas_draft_test"
	d, err := StartDraft(home, id, "")
	if err != nil || len(d.Files) != 0 {
		t.Fatalf("start draft: %+v %v", d, err)
	}
	initialHash := d.DraftHash
	manifest := `{"schemaVersion":1,"sdkVersion":"1","entry":"src/App.tsx","sources":{},"operations":{}}`
	d, err = WriteDraftFile(home, id, "workbench.json", &manifest, d.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := WriteDraftFile(home, id, "src/App.tsx", &manifest, initialHash); !errors.Is(err, ErrDraftConflict) {
		t.Fatalf("stale draft write accepted: %v", err)
	}
	if _, err := WriteDraftFile(home, id, "fixtures/mock.json", &manifest, d.DraftHash); err == nil {
		t.Fatal("fixture file accepted")
	}
	app := "export default function App(){return <p>Draft</p>}"
	d, err = WriteDraftFile(home, id, "src/App.tsx", &app, d.DraftHash)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, "workbenches", id, "draft", "src", "App.tsx")); err != nil {
		t.Fatalf("working copy is not a real source directory: %v", err)
	}
	for _, name := range []string{".DS_Store", "src/.DS_Store"} {
		if err := os.WriteFile(filepath.Join(home, "workbenches", id, "draft", name), []byte("Finder metadata"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := StartDraft(home, id, "")
	if err != nil || reopened.DraftHash != d.DraftHash || reopened.Files["src/App.tsx"] != app {
		t.Fatalf("draft did not survive reopening: %+v %v", reopened, err)
	}
	if _, _, err := (Package{Files: reopened.Files}).Validate(); err != nil {
		t.Fatal(err)
	}
	reopened, err = SetDraftBase(home, id, d.DraftHash)
	if err != nil || reopened.BaseRevisionHash != d.DraftHash || reopened.DraftHash != d.DraftHash {
		t.Fatalf("commit advanced draft incorrectly: %+v %v", reopened, err)
	}
}

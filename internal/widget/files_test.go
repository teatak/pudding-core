package widget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestImmutablePackageIntegrity(t *testing.T) {
	home := t.TempDir()
	p := fixturePackage(t)
	hash, err := WritePackage(home, "wb_test", p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = WritePackage(home, "wb_test", p); err != nil {
		t.Fatalf("repeated save failed: %v", err)
	}
	got, err := ReadPackage(home, "wb_test", hash)
	if err != nil || got.Files["src/App.tsx"] != p.Files["src/App.tsx"] {
		t.Fatalf("%+v %v", got, err)
	}
	root := filepath.Join(home, "studio", "wb_test", "revisions", hash)
	for _, name := range []string{".DS_Store", "src/.DS_Store"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("Finder metadata"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got, err = ReadPackage(home, "wb_test", hash)
	if err != nil || len(got.Files) != len(p.Files) {
		t.Fatalf("Finder metadata changed package integrity: %+v %v", got, err)
	}
	unexpected := filepath.Join(root, ".unexpected")
	if err := os.WriteFile(unexpected, []byte("unexpected"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadPackage(home, "wb_test", hash); err == nil {
		t.Fatal("unrecognized extra source file accepted")
	}
	if err := os.Remove(unexpected); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "studio", "wb_test", "revisions", hash, "src", "App.tsx")
	if err = os.WriteFile(file, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadPackage(home, "wb_test", hash); err == nil {
		t.Fatal("corrupt source accepted")
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(filepath.Join(home, "outside"), file); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadPackage(home, "wb_test", hash); err == nil {
		t.Fatal("symlink accepted")
	}
}

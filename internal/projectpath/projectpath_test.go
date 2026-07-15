package projectpath

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveAcceptsCanonicalSpellingOfAuthorizedSymlinkRoot(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(base, "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(realRoot, "main.go")
	if err := os.WriteFile(target, []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	root, resolved, relative, err := Resolve([]string{aliasRoot}, target, false, false)
	if err != nil {
		t.Fatal(err)
	}
	canonicalTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if root != aliasRoot || resolved != canonicalTarget || relative != "main.go" {
		t.Fatalf("unexpected resolution: root=%q resolved=%q relative=%q", root, resolved, relative)
	}

	missing := filepath.Join(realRoot, "new.go")
	root, resolved, relative, err = Resolve([]string{aliasRoot}, missing, false, true)
	if err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(realRoot)
	if err != nil {
		t.Fatal(err)
	}
	if root != aliasRoot || resolved != filepath.Join(canonicalRoot, "new.go") || relative != "new.go" {
		t.Fatalf("unexpected missing resolution: root=%q resolved=%q relative=%q", root, resolved, relative)
	}
}

func TestResolveCanonicalSpellingStillRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(realRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	aliasRoot := filepath.Join(base, "alias")
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(realRoot, "escape")); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, _, _, err := Resolve([]string{aliasRoot}, filepath.Join(realRoot, "escape", "secret.txt"), false, false); err != ErrPathNotAllowed {
		t.Fatalf("symlink escape error = %v, want %v", err, ErrPathNotAllowed)
	}
}

package projectpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveMultiRootNeverChoosesByExistence(t *testing.T) {
	first, second := canonicalTestDir(t), canonicalTestDir(t)
	if err := os.WriteFile(filepath.Join(second, "same.txt"), []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, populateFirst := range []bool{false, true} {
		if populateFirst {
			if err := os.WriteFile(filepath.Join(first, "same.txt"), []byte("first"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, allowMissing := range []bool{false, true} {
			for _, path := range []string{"same.txt", "new.txt", ".", ""} {
				_, _, _, err := Resolve([]string{first, second}, path, true, allowMissing)
				if !errors.Is(err, ErrAbsolutePathRequired) {
					t.Fatalf("path=%q allowMissing=%v firstExists=%v: %v", path, allowMissing, populateFirst, err)
				}
			}
		}
	}
	root, target, _, err := Resolve([]string{first, second}, filepath.Join(second, "same.txt"), false, false)
	if err != nil || root != second || target != filepath.Join(second, "same.txt") {
		t.Fatalf("explicit second root: root=%q target=%q err=%v", root, target, err)
	}
}

func TestResolveDistinguishesMissingFromUnauthorized(t *testing.T) {
	root, outside := canonicalTestDir(t), canonicalTestDir(t)
	missing := filepath.Join(root, "new", "file.txt")
	if _, _, _, err := Resolve([]string{root}, missing, false, false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing authorized target: %v", err)
	}
	if _, target, _, err := Resolve([]string{root}, "new/file.txt", false, true); err != nil || target != missing {
		t.Fatalf("relative create target=%q err=%v", target, err)
	}
	for _, allowMissing := range []bool{false, true} {
		if _, _, _, err := Resolve([]string{root}, filepath.Join(outside, "missing.txt"), false, allowMissing); !errors.Is(err, ErrPathNotAllowed) {
			t.Fatalf("outside missing target allowMissing=%v: %v", allowMissing, err)
		}
	}
}

func TestResolveRejectsSymlinkCreationEscape(t *testing.T) {
	root, outside := canonicalTestDir(t), canonicalTestDir(t)
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, _, _, err := Resolve([]string{root}, "escape/new.txt", false, true); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("missing file under escaping symlink: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, "not-created"), filepath.Join(root, "dangling")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"dangling", "dangling/file.txt"} {
		if _, _, _, err := Resolve([]string{root}, path, false, true); err == nil {
			t.Fatalf("dangling symlink accepted as a creatable path: %s", path)
		}
	}
}

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

func TestResolveAncestorAndRootSymlinkSpellings(t *testing.T) {
	base := canonicalTestDir(t)
	canonicalParent := filepath.Join(base, "private", "var")
	canonicalRoot := filepath.Join(canonicalParent, "real")
	if err := os.MkdirAll(canonicalRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	ancestorAlias := filepath.Join(base, "var")
	if err := os.Symlink(canonicalParent, ancestorAlias); err != nil {
		t.Fatal(err)
	}
	realSpelling := filepath.Join(ancestorAlias, "real")
	rootAlias := filepath.Join(ancestorAlias, "alias")
	if err := os.Symlink(realSpelling, rootAlias); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalRoot, "existing.txt"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	spellings := []string{rootAlias, realSpelling, canonicalRoot}
	for _, authorized := range spellings {
		for _, spelling := range spellings {
			for _, name := range []string{"existing.txt", "nested/new.txt"} {
				for _, allowMissing := range []bool{false, true} {
					root, target, relative, err := Resolve([]string{authorized}, filepath.Join(spelling, name), false, allowMissing)
					if name == "nested/new.txt" && !allowMissing {
						if !errors.Is(err, os.ErrNotExist) {
							t.Errorf("authorized=%q spelling=%q missing read: %v", authorized, spelling, err)
						}
						continue
					}
					if err != nil || root != authorized || target != filepath.Join(canonicalRoot, name) || relative != name {
						t.Errorf("authorized=%q spelling=%q missing=%v: root=%q target=%q relative=%q err=%v", authorized, spelling, allowMissing, root, target, relative, err)
					}
				}
			}
		}
	}
	outside := filepath.Join(canonicalParent, "outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "existing.txt"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(canonicalRoot, "escape")); err != nil {
		t.Fatal(err)
	}
	for _, spelling := range spellings {
		for _, name := range []string{"existing.txt", "new.txt"} {
			if _, _, _, err := Resolve([]string{rootAlias}, filepath.Join(spelling, "escape", name), false, true); !errors.Is(err, ErrPathNotAllowed) {
				t.Errorf("spelling=%q name=%q escape accepted: %v", spelling, name, err)
			}
		}
	}
}

func canonicalTestDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return root
}

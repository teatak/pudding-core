package projectpath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveEntryPreservesFinalSymlinks(t *testing.T) {
	root, outside := canonicalTestDir(t), canonicalTestDir(t)
	if err := os.WriteFile(filepath.Join(root, "source.txt"), []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, target string }{
		{"file-link", "source.txt"},
		{"directory-link", "."},
		{"dangling-link", "missing.txt"},
		{"outside-link", outside},
	} {
		link := filepath.Join(root, tc.name)
		if err := os.Symlink(tc.target, link); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		for _, spelling := range []string{tc.name, link} {
			gotRoot, target, rel, err := ResolveEntry([]string{root}, spelling, false, false)
			if err != nil || gotRoot != root || target != link || rel != tc.name {
				t.Fatalf("entry %q: root=%q target=%q rel=%q err=%v", spelling, gotRoot, target, rel, err)
			}
			if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("resolved entry is not the link: %v", err)
			}
		}
	}
	if _, target, _, err := Resolve([]string{root}, "file-link", false, false); err != nil || target != filepath.Join(root, "source.txt") {
		t.Fatalf("content reads must keep following authorized links: %q %v", target, err)
	}
	if _, _, _, err := ResolveEntry([]string{root}, "outside-link/child.txt", false, true); !errors.Is(err, ErrPathNotAllowed) {
		t.Fatalf("parent symlink escape accepted: %v", err)
	}
}

func TestResolveEntryAcceptsRootAndParentAliases(t *testing.T) {
	base := canonicalTestDir(t)
	root, alias := filepath.Join(base, "real"), filepath.Join(base, "alias")
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink("src", filepath.Join(root, "parent")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.txt", filepath.Join(root, "src", "link")); err != nil {
		t.Fatal(err)
	}
	for _, authorized := range []string{root, alias} {
		for _, spelling := range []string{root, alias} {
			_, target, rel, err := ResolveEntry([]string{authorized}, filepath.Join(spelling, "parent", "link"), false, false)
			if err != nil || target != filepath.Join(root, "src", "link") || rel != "src/link" {
				t.Fatalf("alias entry: target=%q rel=%q err=%v", target, rel, err)
			}
			_, target, rel, err = ResolveEntry([]string{authorized}, spelling, true, false)
			if err != nil || target != root || rel != "." {
				t.Fatalf("root alias: target=%q rel=%q err=%v", target, rel, err)
			}
			if _, _, _, err := ResolveEntry([]string{authorized}, spelling, false, true); !errors.Is(err, ErrFileRequired) {
				t.Fatalf("root alias became a mutation target: %v", err)
			}
		}
	}
}

func TestResolveEntryProtectsNestedRootsAndAliases(t *testing.T) {
	base := canonicalTestDir(t)
	outer := filepath.Join(base, "outer")
	inner := filepath.Join(outer, "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	assertRoot := func(t *testing.T, roots []string, path string) {
		t.Helper()
		if _, _, _, err := ResolveEntry(roots, path, false, true); !errors.Is(err, ErrFileRequired) {
			t.Fatalf("nested root %q became a mutation target with roots %q: %v", path, roots, err)
		}
		if _, target, rel, err := ResolveEntry(roots, path, true, false); err != nil || target != inner || rel != "." {
			t.Fatalf("nested root identity: target=%q rel=%q err=%v", target, rel, err)
		}
	}
	for _, roots := range [][]string{{inner, outer}, {outer, inner}} {
		assertRoot(t, roots, inner)
	}
	t.Run("aliases", func(t *testing.T) {
		alias := filepath.Join(outer, "inner-alias")
		parentAlias := filepath.Join(base, "parent-alias")
		if err := os.Symlink(inner, alias); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := os.Symlink(outer, parentAlias); err != nil {
			t.Fatal(err)
		}
		for _, roots := range [][]string{{inner, outer}, {outer, inner}, {alias, outer}, {outer, alias}} {
			assertRoot(t, roots, inner)
			assertRoot(t, roots, filepath.Join(parentAlias, "inner"))
		}
		for _, roots := range [][]string{{alias, outer}, {outer, alias}} {
			assertRoot(t, roots, alias)
			assertRoot(t, roots, filepath.Join(parentAlias, "inner-alias"))
		}
		// An ordinary link inside a root is still an entry. Its referent being
		// another authorized root must not prevent removing only this link.
		if _, target, _, err := ResolveEntry([]string{inner, outer}, alias, false, false); err != nil || target != alias {
			t.Fatalf("ordinary link was treated as its root referent: %q %v", target, err)
		}
	})
}

func TestResolvePreservesFileAndRootWhitespace(t *testing.T) {
	base := canonicalTestDir(t)
	root := filepath.Join(base, " project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{" note.txt", "note.txt "} {
		if runtime.GOOS == "windows" && strings.HasSuffix(name, " ") {
			continue
		}
		selected := filepath.Join(root, name)
		if err := os.WriteFile(selected, []byte("selected"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, resolve := range []func([]string, string, bool, bool) (string, string, string, error){Resolve, ResolveEntry} {
			for _, spelling := range []string{name, selected} {
				gotRoot, target, rel, err := resolve([]string{root}, spelling, false, false)
				if err != nil || gotRoot != root || target != selected || rel != name {
					t.Fatalf("path %q: root=%q target=%q rel=%q err=%v", spelling, gotRoot, target, rel, err)
				}
			}
		}
	}
	if runtime.GOOS != "windows" {
		root += " "
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		if _, target, _, err := Resolve([]string{root}, "new.txt", false, true); err != nil || target != filepath.Join(root, "new.txt") {
			t.Fatalf("root whitespace changed: %q %v", target, err)
		}
	}
}
